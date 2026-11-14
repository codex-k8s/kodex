// Package telegram implements only bounded outbound Bot API calls; it does not accept inbound updates or arbitrary agent messages.
// Пакет telegram реализует только ограниченные исходящие Bot API вызовы; входящие updates и произвольные сообщения агента не принимаются.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

const (
	// apiOrigin is the only Telegram origin accepted by the transport; connection payload cannot replace it.
	// apiOrigin — единственный Telegram origin транспорта; payload подключения не заменяет его.
	apiOrigin = integrationpackage.TelegramAPIOrigin
	// maximumResponseBytes bounds provider envelopes before JSON decoding, including chunked responses.
	// maximumResponseBytes ограничивает provider envelope до JSON decode, включая chunked ответы.
	maximumResponseBytes = 256 << 10
	// maximumIdentifier keeps Bot API coordinates within Telegram's documented 52-bit integer range.
	// maximumIdentifier удерживает координаты Bot API в документированном 52-битном диапазоне Telegram.
	maximumIdentifier int64 = integrationpackage.TelegramMaximumIdentifier
	// maximumRetryAfter bounds an explicit provider no-effect flood wait; it never permits replay of an unknown send.
	// maximumRetryAfter ограничивает явное ожидание flood после provider no-effect; неизвестная отправка не повторяется.
	maximumRetryAfter = time.Hour
	// errorConfiguration hides malformed scope and credential details from logs and public callers.
	// errorConfiguration скрывает подробности неверных scope и credential от логов и публичных callers.
	errorConfiguration = "TELEGRAM_CONFIGURATION_INVALID"
	// errorProviderRejected classifies only a validated Bot API rejection without provider descriptions.
	// errorProviderRejected классифицирует лишь проверенный отказ Bot API без provider descriptions.
	errorProviderRejected = "TELEGRAM_PROVIDER_REJECTED"
	// errorRateLimited identifies a validated retry_after response rather than an arbitrary HTTP rate-limit page.
	// errorRateLimited обозначает проверенный retry_after, а не произвольную HTTP rate-limit страницу.
	errorRateLimited = "TELEGRAM_RATE_LIMITED"
	// errorUnknown preserves possible external effects after dispatch without including URL or raw errors.
	// errorUnknown сохраняет возможность внешнего эффекта после отправки без URL и исходных ошибок.
	errorUnknown = "TELEGRAM_OUTCOME_UNKNOWN"
	// errorPreflightUnavailable records read-only unavailability before any send or effect admission.
	// errorPreflightUnavailable фиксирует read-only недоступность до отправки либо effect admission.
	errorPreflightUnavailable = "TELEGRAM_PREFLIGHT_UNAVAILABLE"
	// errorAdmissionClosed means CP has already durably deferred or closed this lease without authorizing a send.
	// errorAdmissionClosed означает, что CP уже устойчиво отложил либо закрыл эту lease без разрешения отправки.
	errorAdmissionClosed = "TELEGRAM_ADMISSION_CLOSED"
)

// operation names the closed provider methods and never contains a request path or credential.
// operation именует закрытые provider methods и никогда не содержит request path либо credential.
type operation string

const (
	// operationIdentity reads the current authenticated bot identity without sending a message.
	// operationIdentity читает текущую identity бота без отправки сообщения.
	operationIdentity operation = "getMe"
	// operationChat reads the exact configured chat without following migration or username aliases.
	// operationChat читает точный настроенный chat без перехода по migration либо username aliases.
	operationChat operation = "getChat"
	// operationMember reads current bot membership for the configured group or channel.
	// operationMember читает актуальное членство бота в настроенной группе либо канале.
	operationMember operation = "getChatMember"
	// operationSend permits only one server-rendered notification within a begun owner delivery.
	// operationSend допускает лишь одно server-rendered уведомление в начатой owner доставке.
	operationSend operation = "sendMessage"
)

// SafeError exposes only a closed outcome and an optional bounded confirmed no-effect delay; raw provider bytes and token URLs are discarded.
// SafeError выдаёт лишь закрытый исход и необязательную ограниченную задержку подтверждённого no-effect; provider bytes и token URL отбрасываются.
type SafeError struct {
	Code       string
	NoEffect   bool
	RetryAfter time.Duration
}

// Error returns the closed code without wrapping a URL-bearing transport error or provider description.
// Error возвращает закрытый код, не оборачивая transport error с URL либо provider description.
func (err *SafeError) Error() string { return err.Code }

// ConfirmedNoEffect accepts only the explicit adapter marker; missing or unknown errors cannot authorize another send.
// ConfirmedNoEffect принимает лишь явный маркер адаптера; отсутствующие либо неизвестные ошибки не разрешают новую отправку.
func ConfirmedNoEffect(err error) bool {
	var safe *SafeError
	return errors.As(err, &safe) && safe.NoEffect
}

// OwnerAdmissionClosed distinguishes an owner-completed pre-send transition from an adapter failure requiring a completion report.
// OwnerAdmissionClosed отличает завершённый владельцем pre-send transition от отказа адаптера, требующего completion report.
func OwnerAdmissionClosed(err error) bool {
	var safe *SafeError
	return errors.As(err, &safe) && safe.Code == errorAdmissionClosed && safe.NoEffect
}

// Outcome returns a closed provider result code without allowing arbitrary errors to expand diagnostics or metric cardinality.
// Outcome возвращает закрытый provider result code, не позволяя произвольным ошибкам расширять диагностику либо metric cardinality.
func Outcome(err error) (bool, string) {
	if err == nil {
		return true, ""
	}
	var safe *SafeError
	if errors.As(err, &safe) {
		switch safe.Code {
		case errorConfiguration, errorProviderRejected, errorRateLimited, errorUnknown, errorPreflightUnavailable, errorAdmissionClosed:
			return false, safe.Code
		}
	}
	return false, errorUnknown
}

// apiClient owns one short-lived credential and a non-instrumented exact-origin transport; it never returns an HTTP request or response to callers.
// apiClient владеет одними краткоживущими реквизитами и неинструментированным transport точного origin; HTTP request/response callers не выдаются.
type apiClient struct {
	token   string
	next    http.RoundTripper
	timeout time.Duration
}

// newAPIClient validates a canonical token path segment and a finite timeout without provider I/O; bot identity still requires getMe.
// newAPIClient проверяет канонический сегмент token path и конечный timeout без provider I/O; identity бота по-прежнему требует getMe.
func newAPIClient(token string, next http.RoundTripper, timeout time.Duration) (*apiClient, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 2 || len(token) > 256 || len(parts[1]) < 20 || next == nil || timeout < time.Second || timeout > 20*time.Second {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id < 1 || id > maximumIdentifier || strconv.FormatInt(id, 10) != parts[0] {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	for _, character := range parts[1] {
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
		}
	}
	return &apiClient{token: token, next: next, timeout: timeout}, nil
}

// apiEnvelope keeps provider descriptions private and preserves raw result bytes until operation-specific coordinate validation.
// apiEnvelope сохраняет provider descriptions приватными и удерживает result bytes до проверки координат конкретной операции.
type apiEnvelope struct {
	OK          *bool           `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description json.RawMessage `json:"description"`
	Parameters  struct {
		RetryAfter      int64 `json:"retry_after"`
		MigrateToChatID int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

// call performs exactly one POST without redirects, HTTP instrumentation, transport retries or caller context values; cancellation and the finite deadline remain linked.
// call выполняет ровно один POST без redirects, HTTP instrumentation, transport retries и values caller context; отмена и конечный deadline остаются связанными.
// Only a validated rejecting Bot API envelope proves no effect; malformed success and transport failure after dispatch remain unknown.
// Только проверенный отказ Bot API доказывает отсутствие эффекта; неверный success и transport failure после dispatch сохраняют неизвестный исход.
func (client *apiClient) call(ctx context.Context, method operation, input any) (json.RawMessage, error) {
	switch method {
	case operationIdentity, operationChat, operationMember, operationSend:
	default:
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	if ctx.Err() != nil {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	payload, err := json.Marshal(input)
	if err != nil || len(payload) > 16<<10 {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	// Новый context не наследует httptrace hooks, которые могли бы увидеть token path.
	requestContext, cancel := context.WithTimeout(context.Background(), client.timeout)
	stop := context.AfterFunc(ctx, cancel)
	defer cancel()
	defer stop()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, apiOrigin+"/bot"+client.token+"/"+string(method), bytes.NewReader(payload))
	if err != nil {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Kodex-Telegram/1")
	response, err := client.next.RoundTrip(request)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, &SafeError{Code: errorUnknown}
	}
	if response == nil || response.Body == nil {
		return nil, &SafeError{Code: errorUnknown}
	}
	defer response.Body.Close()
	if response.ContentLength > maximumResponseBytes {
		return nil, &SafeError{Code: errorUnknown}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	var envelope apiEnvelope
	if err != nil || len(body) > maximumResponseBytes || !uniqueJSON(body) {
		return nil, &SafeError{Code: errorUnknown}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.OK == nil {
		return nil, &SafeError{Code: errorUnknown}
	}
	if response.StatusCode == http.StatusOK && *envelope.OK && envelope.ErrorCode == 0 && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		return envelope.Result, nil
	}
	if !*envelope.OK && envelope.ErrorCode == response.StatusCode && (len(envelope.Result) == 0 || string(envelope.Result) == "null") {
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return nil, &SafeError{Code: errorProviderRejected, NoEffect: true}
		case http.StatusTooManyRequests:
			delay := time.Duration(envelope.Parameters.RetryAfter) * time.Second
			if envelope.Parameters.RetryAfter > 0 && envelope.Parameters.RetryAfter <= int64(maximumRetryAfter/time.Second) && delay <= maximumRetryAfter {
				return nil, &SafeError{Code: errorRateLimited, NoEffect: true, RetryAfter: delay}
			}
		}
	}
	return nil, &SafeError{Code: errorUnknown}
}

// uniqueJSON rejects duplicate or case-aliased keys, excessive nesting and trailing values before typed decoding without returning provider content.
// uniqueJSON отклоняет duplicate/case-alias keys, чрезмерную вложенность и дополнительные значения до typed decode без выдачи provider content.
func uniqueJSON(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if !uniqueJSONValue(decoder, 0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

// uniqueJSONValue consumes one bounded JSON value and tracks distinct keys separately for every object; no partially recovered model is used.
// uniqueJSONValue читает одно ограниченное JSON value и отслеживает разные ключи отдельно для каждого object; частично восстановленная модель не используется.
func uniqueJSONValue(decoder *json.Decoder, depth int) bool {
	if depth > 24 {
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return true
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			folded := strings.ToLower(key)
			if err != nil || !ok || keys[folded] || len(keys) >= 4096 {
				return false
			}
			if len(key) == 0 || key[0] < 'a' || key[0] > 'z' {
				return false
			}
			for _, character := range key {
				if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_') {
					return false
				}
			}
			keys[folded] = true
			if !uniqueJSONValue(decoder, depth+1) {
				return false
			}
		}
		last, err := decoder.Token()
		return err == nil && last == json.Delim('}')
	case '[':
		count := 0
		for decoder.More() {
			count++
			if count > 4096 || !uniqueJSONValue(decoder, depth+1) {
				return false
			}
		}
		last, err := decoder.Token()
		return err == nil && last == json.Delim(']')
	default:
		return false
	}
}
