// Package callback delivers execution-bound control messages and verified file streams to or from runtime-controller using the selected transport and ticket.
// Пакет callback передаёт привязанные к исполнению управляющие сообщения и проверяемые файловые потоки runtime-controller через выбранный транспорт с ticket.
package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/filetransfer"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// Client owns execution-ticket authentication, a control callback client and a separately budgeted file client sharing the selected transport; construction does not authorize an execution.
// Client хранит execution ticket, клиент управляющих callback и отдельный файловый клиент с общим выбранным транспортом; создание не авторизует исполнение.
type Client struct {
	http        *http.Client
	files       *http.Client
	base        *url.URL
	token       string
	retryDelays []time.Duration
}

const (
	// callbackMaximumRetryDelays caps retry scheduling at five delays, allowing at most six attempts for one encoded callback body.
	// callbackMaximumRetryDelays ограничивает расписание пятью задержками: не более шести попыток для одного закодированного тела callback.
	callbackMaximumRetryDelays = 5
	// callbackAttemptTimeout bounds each attempt only when retry delays are configured; the caller's earlier deadline still wins.
	// callbackAttemptTimeout ограничивает отдельную попытку только при заданных задержках повторов; более ранний срок вызывающего сохраняется.
	callbackAttemptTimeout = 8 * time.Second
	// callbackDeliveryTimeout caps delivery and individual configured delays; only completion deliberately detaches from the caller's cancellation.
	// callbackDeliveryTimeout ограничивает доставку и каждую заданную задержку; только завершение намеренно отделяется от отмены вызывающего.
	callbackDeliveryTimeout = 60 * time.Second
)

// defaultCallbackRetryDelays supplies the bounded backoff when the client has no override; a non-nil empty override disables retries.
// defaultCallbackRetryDelays задаёт ограниченные задержки при отсутствии переопределения; пустой срез, отличный от nil, отключает повторы.
var defaultCallbackRetryDelays = [...]time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}

// New selects the input's callback transport, parses its URL and reads the execution ticket, closing idle connections on failure. Control requests get a 35-second client timeout; file transfers use their own policy.
// New выбирает транспорт из входа, разбирает URL и читает execution ticket, закрывая простаивающие соединения при ошибке. Управляющие запросы получают 35 секунд; передача файлов использует отдельную политику.
func New(input model.Input) (*Client, error) {
	transport, err := TransportForInput(input)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(input.CallbackURL)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, errors.New("parse runtime callback URL")
	}
	token, err := readCredential(input.ExecutionTicketFile)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 35 * time.Second}, files: filetransfer.NewClient(transport), base: base, token: token}, nil
}

// Close releases idle connections of both clients; it does not cancel active requests or erase the retained ticket string.
// Close освобождает простаивающие соединения обоих клиентов; не отменяет активные запросы и не стирает сохранённую строку ticket.
func (client *Client) Close() {
	if client.files != nil {
		client.files.CloseIdleConnections()
	}
	if transport, ok := client.http.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

// Token returns the retained execution ticket for trusted local composition; callers must not log or expose this bearer credential.
// Token возвращает сохранённый execution ticket для доверенной локальной сборки; вызывающий не должен журналировать или раскрывать bearer-реквизит.
func (client *Client) Token() string { return client.token }

// Progress posts one revision-bound progress code without retrying an ambiguous outcome, even when retry delays are configured on the client.
// Progress отправляет один привязанный к ревизии код хода без повтора при неопределённом исходе, даже если клиенту заданы задержки повторов.
func (client *Client) Progress(ctx context.Context, input model.Input, code string) error {
	return client.post(ctx, input, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/progress", runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Progress: code})
}

// ProviderProcess rejects observations not matching the input and retries the same typed initialization observation within the caller's cancellation and delivery budget.
// ProviderProcess отклоняет наблюдение чужого входа и повторяет то же типизированное наблюдение инициализации в пределах отмены вызывающего и бюджета доставки.
func (client *Client) ProviderProcess(ctx context.Context, input model.Input, value runtimecontract.ProviderProcessObservation) error {
	if !value.Matches(input) {
		return errors.New("validate provider process callback")
	}
	delivery, cancel := context.WithTimeout(ctx, callbackDeliveryTimeout)
	defer cancel()
	return client.postRetriable(delivery, input, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/progress",
		runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Progress: runtimecontract.ProviderProcessInitializedProgress, ProviderProcess: &value})
}

// PublishedMessage validates the message projection before transport and retries one encoded progress payload within a cancellation-aware delivery budget; it does not create server authority.
// PublishedMessage проверяет проекцию сообщения до транспорта и повторяет один закодированный progress-пакет в бюджете с отменой; серверных полномочий не создаёт.
func (client *Client) PublishedMessage(ctx context.Context, input model.Input, message runtimecontract.RuntimeAgentMessage) error {
	if err := message.Validate(); err != nil {
		return errors.New("validate published message callback")
	}
	delivery, cancel := context.WithTimeout(ctx, callbackDeliveryTimeout)
	defer cancel()
	return client.postRetriable(delivery, input, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/progress",
		runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Message: &message})
}

// Complete validates terminal provenance, normalizes only the exact interrupted
// wait diagnostic, then freezes one delivery body for bounded retries independent
// of caller cancellation. Pod state cannot change the accepted completion intent.
// Complete проверяет provenance завершения, нормализует только exact diagnostic
// прерванного ожидания и фиксирует одно тело для ограниченных повторов независимо
// от отмены caller. Состояние Pod не меняет принятый intent завершения.
func (client *Client) Complete(ctx context.Context, input model.Input, payload runtimecontract.RunnerCompletionRequest) error {
	if err := payload.Validate(); err != nil {
		return errors.New("validate runtime completion: " + err.Error())
	}
	if payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest || payload.Attempt != input.Attempt {
		return errors.New("validate runtime completion: provenance does not match RuntimeRevision")
	}
	payload = interruptedCompletion(input, payload)
	delivery, cancel := context.WithTimeout(context.WithoutCancel(ctx), callbackDeliveryTimeout)
	defer cancel()
	return client.postRetriable(delivery, input, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/complete", payload)
}

// RecordNativeToolCall validates the bounded native-tool projection before retrying its execution-scoped callback; raw command text is not a permitted substitute for safe parameters.
// RecordNativeToolCall проверяет ограниченную проекцию нативного инструмента перед повторами callback конкретного исполнения; сырой текст команды не заменяет безопасные параметры.
func (client *Client) RecordNativeToolCall(ctx context.Context, input model.Input, call runtimecontract.NativeToolCall) error {
	payload := runtimecontract.RunnerNativeToolCallRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, NativeToolCall: call}
	if err := payload.Validate(); err != nil {
		return errors.New("validate native tool callback: " + err.Error())
	}
	delivery, cancel := context.WithTimeout(ctx, callbackDeliveryTimeout)
	defer cancel()
	return client.postRetriable(delivery, input, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/native-tool-call", payload)
}

// CommitProviderCredentialRefresh validates and posts a credential rotation once; an uncertain outcome is returned rather than replayed, and ownership checks remain server-side.
// CommitProviderCredentialRefresh проверяет и однократно отправляет ротацию реквизитов; неопределённый исход возвращается без повтора, проверка владения остаётся серверной.
func (client *Client) CommitProviderCredentialRefresh(ctx context.Context, input model.Input, payload runtimecontract.RunnerProviderCredentialRefreshRequest) error {
	if err := payload.Validate(); err != nil {
		return errors.New("validate provider credential refresh callback: " + err.Error())
	}
	return client.post(ctx, input, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/provider-credential-refresh", payload)
}

// WriteArtifact downloads through the file-budgeted client and verifies status, media type, declared length, digest header and actual bounded bytes. Data may reach destination before final hash validation; callers must discard failed output.
// WriteArtifact получает файл отдельным клиентом и сверяет статус, тип, заявленный размер, заголовок дайджеста и ограниченные фактические байты. Данные могут попасть в destination до итоговой проверки хеша; неуспешный результат нужно отбросить.
func (client *Client) WriteArtifact(ctx context.Context, input model.Input, artifact runtimecontract.RunnerInputArtifact, destination io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, filetransfer.TotalTimeout)
	defer cancel()
	endpoint := *client.base
	endpoint.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/artifacts/" + url.PathEscape(artifact.Ref)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("create runtime artifact request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	bindExecutionHeaders(request, input, "artifact")
	request.Header.Set("Accept", artifact.MediaType)
	response, err := client.files.Do(request)
	if err != nil {
		return errors.New("runtime artifact callback is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != artifact.MediaType ||
		response.Header.Get("X-Kodex-Artifact-Digest") != artifact.Digest || response.ContentLength != artifact.SizeBytes {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
		return errors.New("runtime artifact callback rejected request")
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(response.Body, runtimecontract.MaximumInputArtifactBytes+1))
	if err != nil || written > runtimecontract.MaximumInputArtifactBytes || written != artifact.SizeBytes {
		return errors.New("runtime artifact response is invalid")
	}
	actualDigest := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actualDigest), []byte(artifact.Digest)) != 1 {
		return errors.New("runtime artifact digest is invalid")
	}
	return nil
}

// NextWarm requests the next turn using the warm revision and ticket, treats 204 as unavailable, strictly decodes bounded input and requires a compatible system-assistant turn. It does not itself claim server-owned authority.
// NextWarm запрашивает следующий ход с warm-ревизией и ticket, считает 204 отсутствием, строго декодирует ограниченный вход и требует совместимый ход системного помощника. Серверные полномочия сам не назначает.
func (client *Client) NextWarm(ctx context.Context, input model.Input) (model.Input, bool, error) {
	endpoint := *client.base
	endpoint.Path = "/v1/warm/next"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return model.Input{}, false, errors.New("create warm runtime request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("X-Kodex-Runtime-Revision", input.RuntimeRevisionRef)
	request.Header.Set("X-Kodex-Runtime-Revision-Digest", input.RuntimeRevisionDigest)
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return model.Input{}, false, errors.New("warm runtime callback is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return model.Input{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return model.Input{}, false, errors.New("warm runtime callback rejected request")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, runtimecontract.MaximumRunnerInputBytes+1))
	if err != nil || len(raw) > runtimecontract.MaximumRunnerInputBytes {
		return model.Input{}, false, errors.New("warm runtime response is invalid")
	}
	turn, err := runtimecontract.DecodeRunnerInput(raw)
	if err != nil {
		return model.Input{}, false, errors.New("decode warm runtime turn")
	}
	if turn.Mode != runtimecontract.RunnerModeTurn || !turn.IsSystemAssistant() {
		return model.Input{}, false, errors.New("warm runtime turn kind is invalid")
	}
	warmCompatibility, warmErr := runtimecontract.WarmCompatibilityDigest(input)
	if warmErr != nil {
		return model.Input{}, false, errors.New("warm runtime compatibility is invalid")
	}
	turnCompatibility, turnErr := runtimecontract.WarmCompatibilityDigest(turn)
	if turnErr != nil {
		return model.Input{}, false, errors.New("warm runtime turn compatibility is invalid")
	}
	if turnCompatibility != warmCompatibility {
		return model.Input{}, false, errors.New("warm runtime turn compatibility mismatch")
	}
	return turn, true, nil
}

// post delegates to the shared encoder with no retry delays, preserving one-attempt delivery for non-replayable callback operations.
// post вызывает общий кодировщик без задержек повторов, сохраняя одну попытку для неповторяемых операций callback.
func (client *Client) post(ctx context.Context, input model.Input, path string, payload any) error {
	return client.postWithRetry(ctx, input, path, payload, nil)
}

// postRetriable uses configured delays or the default schedule without changing the input or payload; the outer caller owns the total delivery context.
// postRetriable использует заданные или стандартные задержки без изменения входа и пакета; общий контекст доставки принадлежит внешнему вызывающему.
func (client *Client) postRetriable(ctx context.Context, input model.Input, path string, payload any) error {
	delays := client.retryDelays
	if delays == nil {
		delays = defaultCallbackRetryDelays[:]
	}
	return client.postWithRetry(ctx, input, path, payload, delays)
}

// postWithRetry validates body and schedule bounds, encodes once, clears the encoded bytes on return and reuses them across bounded attempts. Only transport errors and the closed transient status set are retried; cancellation interrupts backoff.
// postWithRetry проверяет пределы тела и расписания, кодирует один раз, очищает байты при выходе и повторно использует их в ограниченных попытках. Повторяются лишь транспортные ошибки и закрытый набор временных статусов; отмена прерывает задержку.
func (client *Client) postWithRetry(ctx context.Context, input model.Input, path string, payload any, retryDelays []time.Duration) error {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > runtimecontract.MaximumCompletionBytes+1<<20 || len(retryDelays) > callbackMaximumRetryDelays {
		return errors.New("encode runtime callback request")
	}
	for _, delay := range retryDelays {
		if delay <= 0 || delay > callbackDeliveryTimeout {
			return errors.New("encode runtime callback request")
		}
	}
	defer clear(raw)
	endpoint := *client.base
	endpoint.Path = path
	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		attemptContext := ctx
		cancel := func() {}
		if len(retryDelays) > 0 {
			attemptContext, cancel = context.WithTimeout(ctx, callbackAttemptTimeout)
		}
		retry, postErr := client.postOnce(attemptContext, input, endpoint.String(), raw)
		cancel()
		if postErr == nil {
			return nil
		}
		lastErr = postErr
		if !retry || attempt == len(retryDelays) {
			return lastErr
		}
		delay := retryDelays[attempt]
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.New("runtime callback is unavailable")
		case <-timer.C:
		}
	}
	return lastErr
}

// postOnce sends one ticket-authenticated, execution-bound JSON request, drains only a bounded response prefix and returns safe status-only errors plus retry eligibility, never the upstream body.
// postOnce отправляет один JSON-запрос с ticket и привязкой исполнения, вычитывает лишь ограниченный префикс ответа и возвращает безопасную ошибку статуса с признаком повтора, но не тело upstream.
func (client *Client) postOnce(ctx context.Context, input model.Input, endpoint string, raw []byte) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return false, errors.New("create runtime callback request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	bindExecutionHeaders(request, input, strings.TrimPrefix(filepath.Base(request.URL.Path), "/"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return true, errors.New("runtime callback is unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return false, nil
	}
	retry := retryableCallbackStatus(response.StatusCode)
	return retry, errors.New("runtime callback rejected request with status " + strconv.Itoa(response.StatusCode))
}

// bindExecutionHeaders writes the exact organization/project/run/session/turn/attempt and revision/input/binding digests plus callback method. These assertions accompany the ticket; header values alone grant no authority.
// bindExecutionHeaders задаёт точные организацию/проект/запуск/сессию/ход/попытку, дайджесты ревизии/входа/привязок и метод callback. Эти утверждения дополняют ticket; сами заголовки полномочий не дают.
func bindExecutionHeaders(request *http.Request, input model.Input, method string) {
	request.Header.Set("X-Kodex-Organization-Ref", input.OrganizationRef)
	request.Header.Set("X-Kodex-Project-Ref", input.ProjectRef)
	request.Header.Set("X-Kodex-Run-Ref", input.RunRef)
	request.Header.Set("X-Kodex-Node-Ref", input.NodeRef)
	request.Header.Set("X-Kodex-Session-Ref", input.SessionRef)
	request.Header.Set("X-Kodex-Turn-Ref", input.TurnRef)
	request.Header.Set("X-Kodex-Attempt", strconv.FormatInt(int64(input.Attempt), 10))
	request.Header.Set("X-Kodex-Runtime-Revision-Digest", input.RuntimeRevisionDigest)
	request.Header.Set("X-Kodex-Input-Digest", input.InputDigest)
	request.Header.Set("X-Kodex-Execution-Binding-Digest", input.ExecutionBindingDigest)
	request.Header.Set("X-Kodex-MCP-Binding-Digest", input.MCPBindingDigest)
	request.Header.Set("X-Kodex-Callback-Method", method)
}

// retryableCallbackStatus recognizes only 408, 429, 500, 502, 503 and 504; authorization failures, conflicts and other statuses remain non-retryable.
// retryableCallbackStatus допускает только 408, 429, 500, 502, 503 и 504; отказы авторизации, конфликты и прочие статусы не повторяются.
func retryableCallbackStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// exactTransport creates the ordinary callback TLS transport with its original thirty-second header budget and fixed safe diagnostics.
// exactTransport создаёт обычный TLS-транспорт callback с прежним тридцатисекундным бюджетом заголовков и фиксированной безопасной диагностикой.
func exactTransport(binding model.TLSBinding) (*http.Transport, error) {
	return newExactTransport(binding, 30*time.Second, "runtime callback")
}

// readCredential reads exactly sixty-four bytes, rejects whitespace-shortened or multiline tickets and clears the byte buffer on success. It neither bounds allocation before ReadFile nor clears early-failure buffers or the returned string.
// readCredential читает ровно шестьдесят четыре байта, отклоняет укороченный обрезкой пробелов или многострочный ticket и очищает буфер при успехе. Не ограничивает выделение до ReadFile и не стирает буферы раннего отказа или возвращённую строку.
func readCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) != 64 {
		return "", errors.New("read execution ticket")
	}
	value := strings.TrimSpace(string(raw))
	if len(value) != 64 || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("execution ticket is invalid")
	}
	for index := range raw {
		raw[index] = 0
	}
	return value, nil
}

// FinancialObservation validates optional telemetry and sends it once under a five-second budget. Transport errors and retry-eligible HTTP statuses are ignored, while non-retryable rejections remain visible; even 408/429 are optional here.
// FinancialObservation проверяет необязательную телеметрию и отправляет один раз с бюджетом пять секунд. Транспортные ошибки и допускающие повтор HTTP-статусы игнорируются, прочие отказы возвращаются; даже 408/429 здесь необязательны.
func (client *Client) FinancialObservation(ctx context.Context, input model.Input, observation runtimecontract.ProviderFinancialObservation) error {
	if observation.Validate() != nil {
		return errors.New("invalid financial observation")
	}
	raw, err := json.Marshal(runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Financial: &observation})
	if err != nil {
		return err
	}
	delivery, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := *client.base
	endpoint.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/progress"
	optional, err := client.postOnce(delivery, input, endpoint.String(), raw)
	if optional {
		return nil
	}
	return err
}
