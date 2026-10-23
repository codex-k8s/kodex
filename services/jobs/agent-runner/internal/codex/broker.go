package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/credentialrelay"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/filetransfer"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	"golang.org/x/sys/unix"
)

const (
	// ProviderSocketPath fixes the shared Unix socket through which runner contacts the isolated provider.
	// ProviderSocketPath задаёт общий Unix-сокет обращения runner к изолированному провайдеру.
	ProviderSocketPath = "/run/kodex/provider/provider.sock"
	// maximumBrokerBytes caps the scanned request frame; prompt bytes have a separate smaller limit.
	// maximumBrokerBytes ограничивает считываемый кадр запроса; байты prompt имеют отдельный меньший лимит.
	maximumBrokerBytes = 4 << 20
	// providerRefreshCommitTimeout bounds credential commit independently of a cancelled execution context.
	// providerRefreshCommitTimeout ограничивает сохранение реквизитов независимо от отменённого контекста исполнения.
	providerRefreshCommitTimeout = 40 * time.Second
	// providerSandboxProbeTimeout bounds the startup bubblewrap availability probe, not an agent turn.
	// providerSandboxProbeTimeout ограничивает стартовую проверку bubblewrap, а не ход агента.
	providerSandboxProbeTimeout = 5 * time.Second
	// providerResultDeliveryGrace budgets process termination, credential commit and final response reading
	// after cancellation; it does not extend the immutable execution deadline.
	// providerResultDeliveryGrace отводит время остановке процесса, сохранению реквизитов и чтению итога
	// после отмены; неизменяемый срок исполнения этим не продлевается.
	providerResultDeliveryGrace = processGrace + terminationGrace + providerRefreshCommitTimeout + 5*time.Second
	// providerWriterUID is the required kernel peer UID for provider responses, not a payload identity.
	// providerWriterUID задаёт обязательный UID peer из ядра для ответов провайдера, а не идентичность из payload.
	providerWriterUID = 10002
	// rolloutCaptureSchema pins the private proof envelope relating archive evidence to a single execution.
	// rolloutCaptureSchema закрепляет формат приватного доказательства связи архива с одним исполнением.
	rolloutCaptureSchema = "kodex.provider-rollout-capture.v1"
	// providerSafeFailureLog formats safe stage/category fields and a correlated numeric RPC code;
	// callers must classify first instead of interpolating the original error or provider body.
	// providerSafeFailureLog форматирует безопасные этапы, категории и числовой код связанного RPC;
	// вызывающий код сначала классифицирует отказ, не подставляя исходную ошибку или тело провайдера.
	providerSafeFailureLog = "Codex provider request failed at safe stage: %s; class: %s; detail: %s; rpc_code: %d; notification: %s; account_read: %s; notification_error: %s; stream_error: %s"
)

var (
	// ErrProviderAuthentication is the stable missing/invalid-auth sentinel without credential details.
	// ErrProviderAuthentication является устойчивым маркером отсутствующих или неверных реквизитов без их деталей.
	ErrProviderAuthentication = errors.New("Codex provider authentication is unavailable")
	// errProviderBrokerResponseInvalid marks a rejected response shape or peer without echoing untrusted input.
	// errProviderBrokerResponseInvalid отмечает отклонённую структуру ответа или peer без вывода недоверенного ввода.
	errProviderBrokerResponseInvalid = errors.New("isolated Codex provider response is invalid")
	// errProviderBrokerFailed represents provider/transport failure separately from a malformed response.
	// errProviderBrokerFailed обозначает отказ провайдера или транспорта отдельно от повреждённого ответа.
	errProviderBrokerFailed = errors.New("isolated Codex provider failed")
)

// brokerRequest transfers one versioned execution snapshot, prompt and local MCP capability over UDS.
// Its fields are not authority by themselves: peer, input, context and credential pins are checked on receipt.
// brokerRequest переносит версию, снимок исполнения, prompt и локальную MCP capability через UDS.
// Поля сами не дают полномочий: получатель проверяет peer, input, context и привязки реквизитов.
type brokerRequest struct {
	Version       int         `json:"version"`
	Input         model.Input `json:"input"`
	Prompt        []byte      `json:"prompt"`
	MCPSocket     string      `json:"mcp_socket"`
	MCPProxyToken string      `json:"mcp_proxy_token"`
}

// brokerResponse separates transport/execution failure class from the Result outcome and optional
// bound diagnostic/capture evidence. The strict terminal consumer enforces admissible combinations.
// brokerResponse отделяет класс отказа транспорта или исполнения от Outcome результата и необязательных
// привязанных доказательств диагностики и архива. Допустимые сочетания проверяет строгий читатель итога.
type brokerResponse struct {
	Result         Result                                     `json:"result"`
	Failure        providerBrokerFailure                      `json:"failure,omitempty"`
	OK             bool                                       `json:"ok"`
	RolloutCapture *rolloutCaptureProof                       `json:"rollout_capture,omitempty"`
	Diagnostic     *runtimecontract.ProviderFailureDiagnostic `json:"diagnostic,omitempty"`
}

// rolloutCaptureProof records exact execution/attempt/session/turn and captured archive metadata.
// The private sealed flag is not decoded from JSON; exported Result fields alone cannot establish proof.
// rolloutCaptureProof хранит точные привязки исполнения, попытки, сессии, хода и метаданные захваченного архива.
// Приватный sealed не читается из JSON; одни публичные поля Result не устанавливают доказательство.
type rolloutCaptureProof struct {
	Schema                 string `json:"schema"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	InputDigest            string `json:"input_digest"`
	Attempt                int32  `json:"attempt"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	SessionID              string `json:"codex_session_id"`
	RelativePath           string `json:"archive_relative_path"`
	SHA256                 string `json:"archive_sha256"`
	SizeBytes              int64  `json:"archive_size_bytes"`
	sealed                 bool
}

// bound checks the proof's schema, canonical digests, exact execution tuple, archive identity/size and
// canonical workspace/home relationship. It neither reads archive bytes nor checks the private seal.
// bound проверяет схему, канонические digest, точную привязку исполнения, имя и размер архива,
// а также каноническую связь workspace/home. Байты архива и приватный seal здесь не проверяются.
func (proof *rolloutCaptureProof) bound(input model.Input) bool {
	return proof != nil && proof.Schema == rolloutCaptureSchema && input.Mode == runtimecontract.RunnerModeTurn &&
		validCaptureDigest(input.ExecutionBindingDigest) && validCaptureDigest(input.RuntimeRevisionDigest) && validCaptureDigest(input.InputDigest) &&
		proof.ExecutionBindingDigest == input.ExecutionBindingDigest && proof.RuntimeRevisionDigest == input.RuntimeRevisionDigest && proof.InputDigest == input.InputDigest &&
		input.Attempt > 0 && proof.Attempt == input.Attempt && input.SessionRef != "" && input.TurnRef != "" && proof.SessionRef == input.SessionRef && proof.TurnRef == input.TurnRef &&
		(input.CodexSessionID == "" || proof.SessionID == input.CodexSessionID) && runtimecontract.ValidateCodexArchiveIdentity(proof.SessionID, proof.RelativePath) == nil &&
		validCaptureDigest(proof.SHA256) && proof.SizeBytes > 0 && proof.SizeBytes <= maximumArchiveBytes &&
		filepath.IsAbs(input.WorkspaceRoot) && filepath.Clean(input.WorkspaceRoot) == input.WorkspaceRoot && input.CodexHome == filepath.Join(input.WorkspaceRoot, ".kodex/state/codex-home")
}

// validCaptureDigest accepts only a lowercase canonical hexadecimal SHA-256 digest of exactly 32 bytes.
// validCaptureDigest принимает только канонический SHA-256 из ровно32 байтов в нижнем шестнадцатеричном регистре.
func validCaptureDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

// withRolloutCapture attaches a sealed proof and its public metadata to a value copy of Result.
// It does not independently verify input binding, file bytes or the final absolute archive path.
// withRolloutCapture прикрепляет закрытое доказательство и его публичные метаданные к копии Result.
// Она самостоятельно не проверяет привязку input, байты файла или окончательный абсолютный путь архива.
func withRolloutCapture(result Result, proof *rolloutCaptureProof) Result {
	if proof == nil || !proof.sealed {
		return result
	}
	result.rolloutCapture = proof
	result.SessionID, result.ArchiveRelativePath, result.ArchiveSHA256, result.ArchiveSizeBytes = proof.SessionID, proof.RelativePath, proof.SHA256, proof.SizeBytes
	return result
}

// matchesCapture compares only public session/relative-path/digest/size metadata to the supplied proof.
// It is not a file read, input-binding check or seal validation.
// matchesCapture сравнивает только публичные session, относительный путь, digest и размер с доказательством.
// Она не читает файл и не проверяет input binding или seal.
func (result Result) matchesCapture(proof *rolloutCaptureProof) bool {
	return proof != nil && result.SessionID == proof.SessionID && result.ArchiveRelativePath == proof.RelativePath && result.ArchiveSHA256 == proof.SHA256 && result.ArchiveSizeBytes == proof.SizeBytes
}

// HasVerifiedRollout requires a private sealed, input-bound proof, matching public metadata and the
// exact workspace-relative absolute path. It cannot be reconstructed from arbitrary Result JSON
// and does not re-read or re-hash a file that may subsequently change.
// HasVerifiedRollout требует приватное закрытое доказательство, привязку к input, совпадение метаданных
// и точный абсолютный путь внутри workspace. Его нельзя восстановить из произвольного JSON Result;
// последующие изменения файла здесь не проверяются повторным чтением или хешированием.
func (result Result) HasVerifiedRollout(input model.Input) bool {
	return result.rolloutCapture != nil && result.rolloutCapture.sealed && result.rolloutCapture.bound(input) && result.matchesCapture(result.rolloutCapture) && result.ArchivePath == filepath.Join(input.WorkspaceRoot, filepath.FromSlash(result.ArchiveRelativePath))
}

// brokerPeerUID obtains Linux SO_PEERCRED only from a real UnixConn under RawConn.Control.
// Transport, descriptor and credential failures collapse to a safe sentinel; the caller checks the expected UID.
// brokerPeerUID получает Linux SO_PEERCRED только у настоящего UnixConn внутри RawConn.Control.
// Ошибки транспорта, дескриптора и реквизитов сводятся к безопасному маркеру; ожидаемый UID сверяет вызывающий код.
func brokerPeerUID(connection net.Conn) (uint32, error) {
	peer, ok := connection.(*net.UnixConn)
	if !ok {
		return 0, errProviderBrokerResponseInvalid
	}
	raw, err := peer.SyscallConn()
	if err != nil {
		return 0, errProviderBrokerResponseInvalid
	}
	var credential *unix.Ucred
	var credentialErr error
	if raw.Control(func(fd uintptr) {
		credential, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}) != nil || credentialErr != nil || credential == nil {
		return 0, errProviderBrokerResponseInvalid
	}
	return credential.Uid, nil
}

// providerBrokerFailure names the closed coarse wire failures, independently of business Result outcomes.
// providerBrokerFailure задаёт закрытые укрупнённые wire-классы отказов отдельно от Outcome результата.
type providerBrokerFailure string

const (
	// providerBrokerFailureAuthentication carries only the authentication failure class, never credentials.
	// providerBrokerFailureAuthentication переносит только класс отказа авторизации, но не реквизиты.
	providerBrokerFailureAuthentication providerBrokerFailure = "AUTHENTICATION"
	// providerBrokerFailureAuthority identifies an unsupported authority request without granting that authority.
	// providerBrokerFailureAuthority обозначает неподдерживаемый запрос полномочий, не выдавая их.
	providerBrokerFailureAuthority providerBrokerFailure = "AUTHORITY"
	// providerBrokerFailureMCP reports an unavailable required MCP path rather than optional degradation.
	// providerBrokerFailureMCP сообщает о недоступном обязательном пути MCP, а не необязательном ухудшении.
	providerBrokerFailureMCP providerBrokerFailure = "MCP"
	// providerBrokerFailureConfiguration represents a rejected immutable runtime selection/profile.
	// providerBrokerFailureConfiguration обозначает отклонённый неизменяемый выбор или профиль исполнения.
	providerBrokerFailureConfiguration providerBrokerFailure = "CONFIGURATION"
	// providerBrokerFailureProvider is the safe fallback class for other provider or execution failures.
	// providerBrokerFailureProvider является безопасным общим классом остальных отказов провайдера или исполнения.
	providerBrokerFailureProvider providerBrokerFailure = "PROVIDER"
)

// providerAuthenticationSnapshot decodes only the mode and credential-bearing fields needed for local
// shape checks; it is not a strict unknown-field/duplicate-key validator or an external authentication probe.
// providerAuthenticationSnapshot читает лишь режим и поля реквизитов для локальной проверки структуры;
// это не строгий валидатор лишних полей или повторных ключей и не внешняя проверка авторизации.
type providerAuthenticationSnapshot struct {
	AuthMode     string          `json:"auth_mode"`
	OpenAIAPIKey *string         `json:"OPENAI_API_KEY"`
	Tokens       json.RawMessage `json:"tokens"`
}

// providerExecutor runs one prepared turn and may return measured partial Result together with an error.
// providerExecutor исполняет один подготовленный ход и может вернуть частичные измерения Result вместе с ошибкой.
type providerExecutor func(context.Context, model.Input, []byte, string) (Result, error)

// providerCredentialRefreshCommitter submits refreshed auth with exact previous revision/digest pins;
// the relay/domain owner, not this function type, authorizes and commits the durable update.
// providerCredentialRefreshCommitter отправляет обновлённый auth с точными прежними revision/digest;
// разрешает и фиксирует устойчивое изменение relay или владелец домена, а не этот тип функции.
type providerCredentialRefreshCommitter func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error

// ExecuteViaBroker requires a live activity consumer and delegates provider-only data through UDS.
// The UID and mount separation is completed by the deployment and receiver checks, not this call alone.
// ExecuteViaBroker требует потребителя текущих событий и передаёт данные провайдера через UDS.
// Разделение UID и mount обеспечивают развёртывание и проверки получателя, а не один этот вызов.
func ExecuteViaBroker(ctx context.Context, input model.Input, prompt []byte, mcpSocket, mcpProxyToken string, onActivity func(runtimecontract.RuntimeActivity) error) (Result, error) {
	if onActivity == nil {
		return Result{}, errProviderBrokerResponseInvalid
	}
	return executeViaBroker(ctx, input, prompt, mcpSocket, mcpProxyToken, onActivity)
}

// readProviderAuthentication checks the protected regular source and pinned digest before returning
// raw auth to its authorized caller. ReadFile occurs before payload-size validation; this is not an
// allocation-bounded/no-race file opener and the caller owns clearing the returned bytes.
// readProviderAuthentication проверяет защищённый обычный файл и закреплённый digest перед выдачей
// сырого auth уполномоченному коду. ReadFile предшествует проверке размера; это не ограничивающее
// выделение памяти или исключающее гонки открытие, а очисткой возвращённых байтов владеет вызывающий код.
func readProviderAuthentication(input model.Input) ([]byte, error) {
	if err := security.VerifyProtectedRegular(input.ProviderAuthFile, false); err != nil {
		return nil, ErrProviderAuthentication
	}
	auth, err := os.ReadFile(input.ProviderAuthFile)
	expectedDigest, digestErr := pinnedProviderDigest(input)
	if err != nil || digestErr != nil || validateProviderAuthenticationPayload(auth, expectedDigest) != nil {
		return nil, ErrProviderAuthentication
	}
	return auth, nil
}

// validateProviderAuthenticationPayload combines local auth-shape checks with exact SHA-256 pin equality,
// without contacting a provider or proving credential freshness at the remote service.
// validateProviderAuthenticationPayload объединяет локальную проверку auth с точным совпадением SHA-256,
// не обращаясь к провайдеру и не доказывая актуальность реквизитов на внешнем сервисе.
func validateProviderAuthenticationPayload(auth []byte, expectedSHA256 string) error {
	if validateProviderAuthentication(auth) != nil {
		return ErrProviderAuthentication
	}
	digest := sha256.Sum256(auth)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return ErrProviderAuthentication
	}
	return nil
}

// validateProviderAuthentication exposes only the mode parser's validity result, not raw credential fields.
// validateProviderAuthentication возвращает лишь результат проверки режима, а не исходные поля реквизитов.
func validateProviderAuthentication(auth []byte) error {
	_, err := providerAuthenticationMode(auth)
	return err
}

// providerAuthenticationMode bounds and parses an object, then accepts supported credential shapes.
// It intentionally uses ordinary JSON decoding; valid local structure is not successful remote login.
// providerAuthenticationMode ограничивает и разбирает объект, затем принимает поддерживаемые формы реквизитов.
// Здесь намеренно обычный JSON decode; корректная локальная структура не означает успешного внешнего входа.
func providerAuthenticationMode(auth []byte) (string, error) {
	trimmed := bytes.TrimSpace(auth)
	if len(auth) == 0 || len(auth) > 1<<20 || len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return "", ErrProviderAuthentication
	}
	var snapshot providerAuthenticationSnapshot
	if json.Unmarshal(trimmed, &snapshot) != nil || !supportedProviderAuthentication(snapshot.AuthMode, snapshot.OpenAIAPIKey, snapshot.Tokens) {
		return "", ErrProviderAuthentication
	}
	return snapshot.AuthMode, nil
}

// supportedProviderAuthentication requires nonempty token objects for chatgpt modes or a nonempty API
// key for apikey mode. It does not validate token claims, expiry or individual OAuth token fields.
// supportedProviderAuthentication требует непустой объект tokens для chatgpt либо непустой API key
// для apikey. Claims, срок действия и отдельные OAuth-поля токенов здесь не проверяются.
func supportedProviderAuthentication(mode string, apiKey *string, tokens json.RawMessage) bool {
	switch mode {
	case "chatgpt", "chatgptAuthTokens":
		var value map[string]json.RawMessage
		return len(tokens) > 0 && json.Unmarshal(tokens, &value) == nil && len(value) > 0
	case "apikey":
		return apiKey != nil && *apiKey != ""
	default:
		return false
	}
}

// executeViaBroker retries failed dials within a timer window while each dial uses the caller context,
// requires the kernel provider UID, installs cancellation/read-grace handling, sends one request and
// reads the exact input-bound response. It never replays an already-sent request after an uncertain result.
// executeViaBroker повторяет неудачные подключения в пределах окна таймера, передавая context каждой попытке,
// требует kernel UID провайдера, связывает отмену и срок чтения, отправляет один запрос и читает
// привязанный к input ответ. Уже отправленный запрос при неопределённом результате не повторяется.
func executeViaBroker(ctx context.Context, input model.Input, prompt []byte, mcpSocket, mcpProxyToken string, onActivity func(runtimecontract.RuntimeActivity) error) (Result, error) {
	dialer := net.Dialer{}
	var connection net.Conn
	var err error
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for connection == nil {
		connection, err = dialer.DialContext(ctx, "unix", ProviderSocketPath)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, context.Canceled
		case <-deadline.C:
			return Result{}, errors.New("connect isolated Codex provider broker")
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer connection.Close()
	peerUID, peerErr := brokerPeerUID(connection)
	if peerErr != nil || peerUID != providerWriterUID {
		return Result{}, errProviderBrokerResponseInvalid
	}
	stopContext := bindBrokerConnectionContext(ctx, connection)
	defer stopContext()
	encoder := json.NewEncoder(connection)
	if err := encoder.Encode(brokerRequest{Version: providerBrokerVersion, Input: input, Prompt: prompt,
		MCPSocket: mcpSocket, MCPProxyToken: mcpProxyToken}); err != nil {
		return Result{}, errors.New("send isolated Codex provider request")
	}
	return readBoundProviderBrokerResponse(connection, &input, providerWriterUID, onActivity)
}

// bindBrokerConnectionContext applies caller write/read deadlines with separate final-result grace
// and registers an interrupt on cancellation. Its returned cleanup stops that registration or waits
// for an already-started callback; it does not itself close the connection and must be called once.
// bindBrokerConnectionContext задаёт сроки записи и чтения с отдельным резервом итога и регистрирует
// прерывание при отмене. Возвращённая очистка снимает регистрацию либо ждёт начавшийся callback;
// само соединение она не закрывает и должна вызываться однократно.
func bindBrokerConnectionContext(ctx context.Context, connection net.Conn) func() {
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetWriteDeadline(deadline)
		_ = connection.SetReadDeadline(deadline.Add(providerResultDeliveryGrace))
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		interruptBrokerRequest(connection)
	})
	return func() {
		if !stop() {
			<-done
		}
	}
}

// interruptBrokerRequest expires writes and half-closes a Unix connection's write side while retaining
// a bounded final-result read period. Deadline/half-close errors are deliberately best effort.
// interruptBrokerRequest прекращает запись и закрывает пишущую сторону Unix-соединения, сохраняя
// ограниченный период чтения итога. Ошибки deadline и half-close намеренно обрабатываются как best effort.
func interruptBrokerRequest(connection net.Conn) {
	_ = connection.SetWriteDeadline(time.Now())
	if unixConnection, ok := connection.(*net.UnixConn); ok {
		_ = unixConnection.CloseWrite()
	}
	_ = connection.SetReadDeadline(time.Now().Add(providerResultDeliveryGrace))
}

// validateBrokerTerminal checks usage, closed completeness, absence of batched calls and final-text size.
// A valid failed envelope keeps only failed-result projection and a typed error; an OK envelope still
// requires a supported outcome. Peer, sequence and proof validation belong to the surrounding reader.
// validateBrokerTerminal проверяет usage, полноту, отсутствие пакета вызовов и размер итогового текста.
// Корректный отказ сохраняет только проекцию измерений с типизированной ошибкой; конверт OK требует
// допустимого Outcome. Peer, порядок и доказательства проверяет окружающий читатель.
func validateBrokerTerminal(response brokerResponse) (Result, error) {
	if response.Result.Usage.Validate() != nil || response.Result.UsageCompleteness > UsageComplete || len(response.Result.ToolCalls) != 0 || len(response.Result.FinalMessage) > maximumFinalBytes {
		return Result{}, errProviderBrokerResponseInvalid
	}
	if !response.OK {
		switch response.Failure {
		case providerBrokerFailureAuthentication, providerBrokerFailureAuthority, providerBrokerFailureMCP,
			providerBrokerFailureConfiguration, providerBrokerFailureProvider:
		default:
			return Result{}, errProviderBrokerResponseInvalid
		}
		return failedProviderResult(response.Result), providerBrokerError(response.Failure)
	}
	if response.Failure != "" || response.Result.Outcome != "SUCCEEDED" && response.Result.Outcome != "FAILED" {
		return Result{}, errProviderBrokerResponseInvalid
	}
	return response.Result, nil
}

// providerBrokerError maps a closed wire class back to its stable sentinel; unknown classes fail closed.
// providerBrokerError восстанавливает устойчивый маркер по закрытому wire-классу; неизвестный класс отклоняется.
func providerBrokerError(failure providerBrokerFailure) error {
	switch failure {
	case providerBrokerFailureAuthentication:
		return ErrProviderAuthentication
	case providerBrokerFailureAuthority:
		return ErrAuthorityRequestUnsupported
	case providerBrokerFailureMCP:
		return ErrRequiredMCPUnavailable
	case providerBrokerFailureConfiguration:
		return ErrRuntimeProfile
	case providerBrokerFailureProvider:
		return errProviderBrokerFailed
	default:
		return errProviderBrokerResponseInvalid
	}
}

// classifyProviderBrokerFailure uses errors.Is in priority order and defaults to PROVIDER without
// serializing arbitrary error text; it does not decide whether a result is successful.
// classifyProviderBrokerFailure проверяет errors.Is в заданном порядке и иначе выбирает PROVIDER без
// сериализации произвольного текста ошибки; успешность результата эта функция не определяет.
func classifyProviderBrokerFailure(err error) providerBrokerFailure {
	switch {
	case errors.Is(err, ErrProviderAuthentication):
		return providerBrokerFailureAuthentication
	case errors.Is(err, ErrAuthorityRequestUnsupported):
		return providerBrokerFailureAuthority
	case errors.Is(err, ErrRequiredMCPUnavailable):
		return providerBrokerFailureMCP
	case errors.Is(err, ErrRuntimeProfile):
		return providerBrokerFailureConfiguration
	default:
		return providerBrokerFailureProvider
	}
}

// providerSafeFailureClass preserves the distinct account-response schema class and otherwise returns
// only the coarse safe broker class, excluding external bodies, account identity and credential values.
// providerSafeFailureClass сохраняет отдельный класс схемы ответа аккаунта, иначе возвращая лишь
// безопасный укрупнённый класс broker без внешних тел, идентичности аккаунта и значений реквизитов.
func providerSafeFailureClass(err error) string {
	if errors.Is(err, errAccountReadResponseInvalid) {
		return "ACCOUNT_RESPONSE_SCHEMA"
	}
	return string(classifyProviderBrokerFailure(err))
}

// logProviderSafeFailure emits classified stage/detail/notification/stream fields, never err.Error().
// The numeric code is retained only by the correlated RPC-error projection in providerFailureDetails.
// logProviderSafeFailure выводит классифицированные stage/detail/notification/stream, но не err.Error().
// Числовой код сохраняет только проекция связанного RPC-отказа внутри providerFailureDetails.
func logProviderSafeFailure(stage providerExecutionStage, err error) {
	diagnostic, code := providerFailureDetails(stage, err)
	log.Printf(providerSafeFailureLog, stage, diagnostic.Class, diagnostic.Detail, code, diagnostic.Notification, diagnostic.AccountRead, diagnostic.NotificationError, safeAppServerStreamFailureReason(err))
}

// providerFailureDetails projects typed resume/call failures into allowlisted detail, notification and
// account/token categories, retaining an RPC code only for RPC_ERROR. Returned diagnostics are unbound;
// bindProviderDiagnostic must add execution pins and validate before wire delivery.
// providerFailureDetails переводит типизированные resume/call отказы в разрешённые detail, notification
// и account/token категории, сохраняя RPC-код только для RPC_ERROR. Диагностика ещё не привязана:
// bindProviderDiagnostic должна добавить привязки исполнения и проверить её перед wire-доставкой.
func providerFailureDetails(stage providerExecutionStage, err error) (runtimecontract.ProviderFailureDiagnostic, int64) {
	detail, code := "NONE", int64(0)
	notification := "NONE"
	notificationError := "NONE"
	accountRead := "NONE"
	var resumeFailure *resumeSourceFailure
	if stage == providerStageThreadRead && providerSafeFailureClass(err) == "PROVIDER" && errors.As(err, &resumeFailure) {
		switch resumeFailure.detail {
		case "RESUME_SOURCE_SCHEMA", "RESUME_SOURCE_ID", "RESUME_SOURCE_LOCATOR", "RESUME_SOURCE_OPEN", "RESUME_SOURCE_METADATA", "RESUME_SOURCE_IDENTITY":
			detail = resumeFailure.detail
		}
	}
	var failure *appServerCallFailure
	if errors.As(err, &failure) {
		switch failure.detail {
		case "REQUEST_WRITE", "CONTEXT_CANCELLED", "STREAM_CLOSED", "STREAM_INVALID",
			"NOTIFICATION_INVALID", "REQUEST_REJECTED", "RESPONSE_CORRELATION", "MESSAGE_KIND", "RPC_ERROR":
			detail = failure.detail
			if detail == "RPC_ERROR" {
				code = failure.code
				if stage == providerStageAccountRead && code == -32603 {
					accountRead = safeAccountReadFailure(failure.accountRead)
				}
			}
			if detail == "NOTIFICATION_INVALID" {
				notification = safeNotificationMethod(failure.notification)
				notificationError = "UNKNOWN"
				switch failure.notificationError {
				case "METHOD", "ENVELOPE", "TUPLE", "ITEM", "TIMESTAMP", "MESSAGE", "TOKEN_USAGE", "TERMINAL", "LIFECYCLE", "MCP", "PROVIDER_ERROR":
					notificationError = failure.notificationError
				}
				if notification == "thread/tokenUsage/updated" {
					if reason := safeTokenUsageFailureReason(tokenUsageFailureReason(failure.notificationError)); reason != "UNKNOWN" {
						notificationError = reason
					}
				}
			}
		}
	}
	return runtimecontract.ProviderFailureDiagnostic{Kind: "REQUEST_FAILURE", Stage: string(stage),
		Class: providerSafeFailureClass(err), Detail: detail, Notification: notification,
		AccountRead: accountRead, NotificationError: notificationError}, code
}

// bindProviderDiagnostic assigns schema and exact execution pins, narrows notification values and
// returns nil if the resulting diagnostic is invalid. It does not authenticate caller-supplied input.
// bindProviderDiagnostic назначает схему и точные привязки исполнения, сужает notification
// и возвращает nil при неверной диагностике. Подлинность переданного input она сама не устанавливает.
func bindProviderDiagnostic(input model.Input, value runtimecontract.ProviderFailureDiagnostic) *runtimecontract.ProviderFailureDiagnostic {
	value.Schema, value.RuntimeRevisionDigest, value.InputDigest = runtimecontract.ProviderFailureDiagnosticSchema, input.RuntimeRevisionDigest, input.InputDigest
	value.ExecutionBindingDigest, value.SessionRef, value.TurnRef, value.Attempt = input.ExecutionBindingDigest, input.SessionRef, input.TurnRef, input.Attempt
	switch value.Notification {
	case "NONE", "UNKNOWN", "thread/tokenUsage/updated", "rawResponse/completed":
	default:
		value.Notification = "UNKNOWN"
	}
	if value.Validate() != nil {
		return nil
	}
	return &value
}

// safeNotificationMethod returns registered notification names plus a closed diagnostic-only set;
// recognizing a name here neither executes it nor expands the protocol's allowed notification methods.
// safeNotificationMethod возвращает зарегистрированные имена уведомлений и закрытое диагностическое дополнение;
// узнавание имени не исполняет уведомление и не расширяет методы, разрешённые протоколом.
func safeNotificationMethod(method string) string {
	if _, allowed := serverNotificationMethods[method]; allowed {
		return method
	}
	switch method {
	case "thread/reverted", "thread/attachment/updated", "thread/queue/changed", "project/changed",
		"thread/project/updated", "thread/environment/connected", "thread/environment/disconnected",
		"autoApprovalReview/strictReviewRequired", "mcpServer/event/stream/notification", "account/gatewayOAuth/changed",
		"modelProvider/authRecoveryStarted", "modelProvider/authRecoveryCompleted", "thread/realtime/item/started",
		"thread/realtime/item/transcript/delta", "thread/realtime/item/completed":
		return method
	default:
		return "UNKNOWN"
	}
}

// ServeProviderBroker requires UID10002, disables dumpability, probes bubblewrap and serves the protected
// UDS one connection at a time. Context cancellation closes the listener; request failures are safely
// logged before accepting the next peer. Absence of runner authority mounts is a deployment invariant,
// not inspected here, and this routine does not claim to join its listener-cancellation goroutine.
// ServeProviderBroker требует UID10002, отключает dumpability, проверяет bubblewrap и обслуживает
// защищённый UDS по одному соединению. Отмена закрывает listener; отказ запроса безопасно логируется
// перед следующим peer. Отсутствие authority mounts runner обеспечивает развёртывание, а не эта функция;
// она не заявляет ожидания своей goroutine отмены listener.
func ServeProviderBroker(ctx context.Context) error {
	proofObserver := providerInputProofLogger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if os.Geteuid() != 10002 {
		return errors.New("Codex provider broker UID is invalid")
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return errors.New("disable provider broker process inspection")
	}
	if err := verifyProviderSandbox(ctx, func(command *exec.Cmd) error { return command.Run() }); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ProviderSocketPath), 0o770); err != nil {
		return errors.New("create provider broker socket directory")
	}
	_ = os.Remove(ProviderSocketPath)
	listener, err := net.Listen("unix", ProviderSocketPath)
	if err != nil {
		return errors.New("listen isolated Codex provider broker")
	}
	defer listener.Close()
	if err := os.Chown(ProviderSocketPath, -1, 29000); err != nil || os.Chmod(ProviderSocketPath, 0o660) != nil {
		return errors.New("protect provider broker socket")
	}
	go func() { <-ctx.Done(); _ = listener.Close() }()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("accept isolated Codex provider request")
		}
		if err := serveBrokerRequest(ctx, connection, proofObserver); err != nil {
			logProviderSafeFailure(providerStageOf(err), err)
			_ = connection.Close()
			continue
		}
		_ = connection.Close()
	}
}

// verifyProviderSandbox runs the fixed read-only bubblewrap probe with a minimal environment and
// bounded context through the supplied executor. An injected successful executor is not live sandbox proof.
// verifyProviderSandbox запускает фиксированную read-only проверку bubblewrap с минимальным окружением
// и ограниченным context через переданный executor. Подставленный успешный executor не доказывает живой sandbox.
func verifyProviderSandbox(ctx context.Context, execute func(*exec.Cmd) error) error {
	if execute == nil {
		return errors.New("Codex provider sandbox probe is unavailable")
	}
	probeContext, cancel := context.WithTimeout(ctx, providerSandboxProbeTimeout)
	defer cancel()
	uid := strconv.Itoa(os.Geteuid())
	command := exec.CommandContext(probeContext, "/usr/bin/bwrap", "--unshare-user", "--uid", uid, "--gid", uid,
		"--ro-bind", "/", "/", "/usr/bin/true")
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	command.Stdin, command.Stdout, command.Stderr = nil, io.Discard, io.Discard
	if err := execute(command); err != nil {
		return errors.New("Codex provider sandbox is unavailable")
	}
	return nil
}

// serveBrokerRequest accepts only a UID10001 Unix peer, validates the bounded request and runtime/context
// snapshot, binds peer cancellation/deadline, verifies auth and MCP capability, then prepares the bridge
// and executes one turn with credential relay. Failures retain measured usage and safe provenance;
// normalization follows completed execution. This path does not accept authority from request IDs alone.
// serveBrokerRequest принимает только Unix peer с UID10001, проверяет ограниченный запрос и runtime/context,
// связывает отмену и срок, проверяет auth и MCP capability, затем готовит bridge и исполняет один ход
// с credential relay. Отказы сохраняют измерения и безопасное происхождение; нормализация следует
// после исполнения. Одни идентификаторы запроса не дают этому пути полномочий.
func serveBrokerRequest(ctx context.Context, connection net.Conn, proofObserver providerInputProofObserver) error {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return atProviderStage(providerStageBrokerRequest, errors.New("provider broker transport is invalid"))
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return atProviderStage(providerStageBrokerRequest, errors.New("inspect provider broker peer"))
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || credential == nil || credential.Uid != 10001 {
		return atProviderStage(providerStageBrokerRequest, errors.New("provider broker peer is unauthorized"))
	}
	if connection.SetReadDeadline(time.Now().Add(30*time.Second)) != nil {
		return atProviderStage(providerStageBrokerRequest, errProviderBrokerFailed)
	}
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 64<<10), maximumBrokerBytes)
	var request brokerRequest
	if !scanner.Scan() || rejectDuplicateJSONKeys(scanner.Bytes()) != nil || strictDecode(scanner.Bytes(), &request) != nil || request.Version != providerBrokerVersion || request.Input.Validate() != nil ||
		len(request.Prompt) == 0 || len(request.Prompt) > 1<<20 {
		return atProviderStage(providerStageBrokerRequest, errors.New("provider broker request is invalid"))
	}
	frames := &brokerFrameWriter{writer: connection, input: &request.Input}
	if err := ValidateRuntimeProfile(request.Input); err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageSelection, err)
	}
	snapshot, err := request.Input.RequiredContextSnapshot(time.Now())
	if err != nil || verifyProviderContext(request.Input, snapshot) != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageContext, ErrRuntimeProfile)
	}
	ctx, cancelContext := snapshot.BoundExecutionContext(ctx)
	defer cancelContext()
	if connection.SetReadDeadline(time.Time{}) != nil {
		return errProviderBrokerFailed
	}
	ctx, joinPeer := bindBrokerPeerContext(ctx, connection, scanner)
	defer joinPeer()
	ctx, cancelDeadline := request.Input.BoundExecutionDeadline(ctx, 0)
	defer cancelDeadline()
	auth, err := readProviderAuthentication(request.Input)
	if err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageAuthRead, err)
	}
	defer clear(auth)
	expectedDigest, err := pinnedProviderDigest(request.Input)
	if err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageAccountPin, err)
	}
	digest := sha256.Sum256(auth)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return writeProviderBrokerFailureAtStage(frames, providerStageAccountPin, errors.New("provider broker account pin mismatch"))
	}
	if request.MCPSocket != "/run/kodex/provider/mcp-authority.sock" || len(request.MCPProxyToken) != 64 {
		return writeProviderBrokerFailureAtStage(frames, providerStageMCPBinding, errors.New("provider broker MCP binding is invalid"))
	}
	if _, err := hex.DecodeString(request.MCPProxyToken); err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageMCPBinding, errors.New("provider broker MCP capability is invalid"))
	}
	bridge, err := startProviderMCPBridge(ctx, request.MCPSocket, request.MCPProxyToken, request.Input)
	if err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageMCPBridge, err)
	}
	defer bridge.Close()
	if err := PrepareHomeWithAuth(request.Input, bridge.URL(), auth); err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageHomePrepare, err)
	}
	execute := func(ctx context.Context, input model.Input, prompt []byte, token string) (Result, error) {
		if err := prepareWorkingDirectory(ctx, input); err != nil {
			return Result{}, atProviderStage(providerStageHomePrepare, err)
		}
		return executeLocalWithInputProof(ctx, input, prompt, token, frames.activity, proofObserver)
	}
	result, err := executeProviderTurn(ctx, request.Input, request.Prompt, request.MCPProxyToken, execute, credentialrelay.Commit)
	if err != nil {
		logProviderSafeFailure(providerStageOf(err), err)
		return writeProviderBrokerResultFailure(frames, result, err)
	}
	result = normalizeCompletedProviderWorkspace(ctx, request.Input, result, normalizeProviderWorkspaceAccess)
	if result.Outcome != "SUCCEEDED" {
		log.Printf("Codex provider turn completed with safe failure code: %s", result.FailureCode)
	}
	diagnostic := result.ProviderDiagnostic(request.Input)
	if result.Outcome != "SUCCEEDED" && diagnostic == nil {
		diagnostic = bindProviderDiagnostic(request.Input, runtimecontract.ProviderFailureDiagnostic{
			Kind: "TERMINAL_FAILURE", Stage: "TERMINAL_RESULT", Class: "PROVIDER", Detail: "NONE",
			Notification: "NONE", NotificationError: "NONE", AccountRead: "NONE", TerminalCode: result.FailureCode})
	}
	return frames.finish(brokerResponse{Result: result, OK: true, Diagnostic: diagnostic})
}

// writeProviderBrokerFailure sends an empty failed-result envelope using the safe classification of err.
// writeProviderBrokerFailure отправляет пустой конверт отказа, используя безопасную классификацию err.
func writeProviderBrokerFailure(connection io.Writer, err error) error {
	return writeProviderBrokerResultFailure(connection, Result{}, err)
}

// writeProviderBrokerFailureAtStage logs only safe stage metadata, then writes a stage-wrapped failure.
// writeProviderBrokerFailureAtStage логирует только безопасные данные этапа и пишет отказ с привязкой к этапу.
func writeProviderBrokerFailureAtStage(connection io.Writer, stage providerExecutionStage, err error) error {
	logProviderSafeFailure(stage, err)
	return writeProviderBrokerFailure(connection, atProviderStage(stage, err))
}

// failedProviderResult drops unconfirmed outcome/final text while retaining measured usage, completeness,
// activity facts and private diagnostics. Matching sealed capture metadata is retained without a new
// file read; slices/pointers are not deep-copied and no credential effect is confirmed by this projection.
// failedProviderResult убирает неподтверждённые Outcome и итоговый текст, сохраняя измерения, полноту,
// факты событий и приватную диагностику. Совпадающий закрытый захват сохраняется без нового чтения файла;
// slice/pointer не копируются глубоко, а подтверждения credential effect эта проекция не выдаёт.
func failedProviderResult(result Result) Result {
	failed := Result{Usage: result.Usage, UsageCompleteness: result.UsageCompleteness, ToolCalls: result.ToolCalls, providerDiagnostic: result.providerDiagnostic}
	if result.rolloutCapture != nil && result.rolloutCapture.sealed && result.matchesCapture(result.rolloutCapture) {
		failed = withRolloutCapture(failed, result.rolloutCapture)
		failed.ArchivePath = result.ArchivePath
	}
	return failed
}

// writeProviderBrokerResultFailure keeps the failed-result measurement projection and adds a validated
// input-bound diagnostic only when the supplied writer carries that input. Terminal framing removes
// already-published tool calls; no raw underlying error is serialized.
// writeProviderBrokerResultFailure сохраняет проекцию измерений отказа и добавляет проверенную привязанную
// диагностику только при наличии input у writer. Кадр итога исключает уже переданные вызовы;
// исходная вложенная ошибка не сериализуется.
func writeProviderBrokerResultFailure(connection io.Writer, result Result, err error) error {
	var diagnostic *runtimecontract.ProviderFailureDiagnostic
	if frames, ok := connection.(*brokerFrameWriter); ok && frames.input != nil {
		value, _ := providerFailureDetails(providerStageOf(err), err)
		diagnostic = bindProviderDiagnostic(*frames.input, value)
	}
	return writeBrokerTerminal(connection, brokerResponse{
		Result:     failedProviderResult(result),
		Failure:    classifyProviderBrokerFailure(err),
		OK:         false,
		Diagnostic: diagnostic,
	})
}

// executeProviderTurn validates the pinned deadline, then executes once with a deferred attempt to remove
// the temporary auth file. Refreshed credentials use an independent commit timeout even after execution failure;
// refresh/commit errors keep the measured Result. Successful non-assistant artifact-capable turns
// publish their outbox, converting publication failure into a failed outcome without inventing success.
// executeProviderTurn проверяет закреплённый срок, затем исполняет один раз с отложенной попыткой удалить
// временный auth. Обновлённые реквизиты сохраняются с независимым timeout даже после отказа исполнения;
// ошибки refresh/commit сохраняют измеренный Result. Успешный ход не-помощника с artifact capability
// публикует outbox, а отказ публикации даёт ошибочный Outcome вместо выдуманного успеха.
func executeProviderTurn(ctx context.Context, input model.Input, prompt []byte, mcpProxyToken string,
	execute providerExecutor, commit providerCredentialRefreshCommitter,
) (Result, error) {
	if input.ExecutionDeadline.Validate() != nil {
		return Result{}, ErrRuntimeProfile
	}
	ctx, cancelDeadline := input.BoundExecutionDeadline(ctx, 0)
	defer cancelDeadline()
	authenticationPath := filepath.Join(input.CodexHome, "auth.json")
	defer os.Remove(authenticationPath)
	result, executionErr := execute(ctx, input, prompt, mcpProxyToken)
	authentication, changed, err := readProviderCredentialRefresh(input, authenticationPath)
	if err != nil {
		return result, err
	}
	defer clear(authentication)
	if changed {
		payload := runtimecontract.RunnerProviderCredentialRefreshRequest{
			RuntimeRevisionDigest:         input.RuntimeRevisionDigest,
			PreviousCredentialRevisionRef: input.ProviderCredentialRef,
			PreviousContentSHA256:         input.ProviderCredentialSHA256,
			Authentication:                authentication,
		}
		commitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerRefreshCommitTimeout)
		defer cancel()
		if err := commit(commitContext, input, payload); err != nil {
			return result, errors.New("commit refreshed provider authentication")
		}
	}
	if executionErr == nil && result.Outcome == "SUCCEEDED" && !input.IsAssistant() && slices.Contains(input.Capabilities, runtimecontract.ArtifactCapability) {
		if err := publishProviderOutbox(ctx, input); err != nil {
			logProviderFilesystemDenial(providerOutboxStage, err)
			result.Outcome, result.FailureCode, result.FinalMessage = "FAILED", "RUNTIME_ARTIFACT_INVALID", "i18n:RUNTIME_ARTIFACT_INVALID"
			result = result.WithWorkspaceDiagnostic(input, providerOutboxStage, filesystemDenialReason(providerOutboxStage, err).String())
		}
	}
	return result, executionErr
}

// readProviderCredentialRefresh opens the workspace-confined protected auth file, bounds metadata and
// actual read size, validates its mode and compares the content pin. Changed API-key auth is rejected;
// invalid buffers are cleared, while callers must clear a successfully returned OAuth/auth buffer.
// readProviderCredentialRefresh открывает защищённый auth внутри workspace, ограничивает метаданные
// и фактическое чтение, проверяет режим и сравнивает digest. Изменившийся API-key auth отклоняется;
// ошибочные буферы очищаются, а успешно возвращённый auth обязан очистить вызывающий код.
func readProviderCredentialRefresh(input model.Input, path string) ([]byte, bool, error) {
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return nil, false, errors.New("read refreshed provider authentication")
	}
	defer file.Close()
	if info.Size() <= 0 || info.Size() > runtimecontract.MaximumProviderAuthBytes {
		return nil, false, errors.New("refreshed provider authentication metadata is invalid")
	}
	authentication, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumProviderAuthBytes+1))
	if err != nil || int64(len(authentication)) != info.Size() || len(authentication) > runtimecontract.MaximumProviderAuthBytes {
		clear(authentication)
		return nil, false, errors.New("refreshed provider authentication content is invalid")
	}
	mode, modeErr := providerAuthenticationMode(authentication)
	if modeErr != nil {
		clear(authentication)
		return nil, false, errors.New("refreshed provider authentication is invalid")
	}
	digest := sha256.Sum256(authentication)
	changed := hex.EncodeToString(digest[:]) != input.ProviderCredentialSHA256
	if changed && mode == "apikey" {
		clear(authentication)
		return nil, false, errors.New("provider API-key authentication changed unexpectedly")
	}
	return authentication, changed, nil
}

// providerMCPBridge owns the loopback HTTP server, UDS-only upstream transport and serve completion channel.
// Its URL is local provider access, not a new public endpoint or authority grant.
// providerMCPBridge владеет loopback HTTP server, транспортом upstream только через UDS и каналом завершения.
// Его URL даёт локальную точку доступа провайдера, а не публичный endpoint или новые полномочия.
type providerMCPBridge struct {
	server    *http.Server
	transport *http.Transport
	done      chan error
	url       string
}

// startProviderMCPBridge requires the provider UID and fixed authority socket, then exposes bearer-checked
// MCP methods and catalog-bound file GETs on loopback. The proxy dials only that UDS, strips incoming
// cookie/forwarding headers and bounds file-response context/write time. Authorization stays with the
// upstream authority; token-length checks here are not independent token authenticity verification.
// startProviderMCPBridge требует UID провайдера и фиксированный authority socket, открывая на loopback
// MCP-методы с bearer-проверкой и привязанные к каталогу file GET. Proxy ходит только в этот UDS,
// удаляет входящие cookie/forwarding заголовки и ограничивает context/запись файлового ответа.
// Авторизация остаётся у upstream; одна проверка длины токена здесь не доказывает его подлинность.
func startProviderMCPBridge(ctx context.Context, socketPath, localToken string, input model.Input) (*providerMCPBridge, error) {
	if os.Geteuid() != 10002 || socketPath != "/run/kodex/provider/mcp-authority.sock" || len(localToken) != 64 {
		return nil, errors.New("provider MCP bridge binding is invalid")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("listen provider MCP bridge")
	}
	transport := &http.Transport{DisableCompression: true, MaxResponseHeaderBytes: 16 << 10, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	target, _ := url.Parse("http://kodex-mcp-authority/mcp")
	reverse := &httputil.ReverseProxy{Transport: transport, ErrorLog: log.New(io.Discard, "", 0),
		Director: func(request *http.Request) {
			request.URL.Scheme, request.URL.Host = target.Scheme, target.Host
			request.Host = target.Host
			request.Header.Del("Cookie")
			request.Header.Del("Forwarded")
			request.Header.Del("X-Forwarded-For")
			request.Header.Del("X-Forwarded-Host")
			request.Header.Del("X-Forwarded-Proto")
		}, ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "required MCP authority is unavailable", http.StatusBadGateway)
		}, FlushInterval: -1}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path != "/mcp" && subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) == 1 {
			if _, err := filetransfer.CatalogRequest(input, request); err != nil {
				http.NotFound(writer, request)
				return
			}
			bounded, cancel := context.WithTimeout(request.Context(), filetransfer.TotalTimeout)
			defer cancel()
			control := http.NewResponseController(writer)
			deadline, _ := bounded.Deadline()
			if err := control.SetWriteDeadline(deadline); err != nil {
				http.Error(writer, "runtime file response deadline is unavailable", http.StatusServiceUnavailable)
				return
			}
			defer control.SetWriteDeadline(time.Time{})
			reverse.ServeHTTP(writer, request.WithContext(bounded))
			return
		}
		if request.URL.Path != "/mcp" || request.URL.RawQuery != "" ||
			(request.Method != http.MethodPost && request.Method != http.MethodGet && request.Method != http.MethodDelete) ||
			subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) != 1 {
			http.Error(writer, "invalid provider MCP request", http.StatusNotFound)
			return
		}
		reverse.ServeHTTP(writer, request)
	})
	done := make(chan error, 1)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	bridge := &providerMCPBridge{server: server, transport: transport, done: done,
		url: "http://" + listener.Addr().String() + "/mcp"}
	go func() { done <- server.Serve(listener) }()
	return bridge, nil
}

// URL returns the bridge's loopback MCP address without performing readiness or network checks.
// URL возвращает loopback адрес MCP bridge без проверки готовности или сетевого запроса.
func (bridge *providerMCPBridge) URL() string { return bridge.url }

// Close abruptly closes the HTTP server, releases idle upstream connections and waits at most one second
// for Serve to return. It is not graceful request draining and timeout does not prove the goroutine joined.
// Close немедленно закрывает HTTP server, освобождает idle upstream connections и ждёт Serve максимум секунду.
// Это не graceful drain запросов, а timeout не доказывает завершение goroutine.
func (bridge *providerMCPBridge) Close() {
	_ = bridge.server.Close()
	bridge.transport.CloseIdleConnections()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-bridge.done:
	case <-timer.C:
	}
}

// decodeEOF accepts only an immediate EOF after decoding; a second value or malformed suffix is rejected.
// decodeEOF принимает только немедленный EOF; второе значение или повреждённый хвост отклоняются.
func decodeEOF(decoder *json.Decoder) bool {
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

// boundedReader tracks a caller-supplied byte budget across reads without allocating a full response.
// Exhaustion produces an error rather than pretending the input ended cleanly.
// boundedReader отслеживает переданный байтовый бюджет чтений без выделения полного ответа.
// Исчерпание даёт ошибку, а не притворяется корректным концом ввода.
type boundedReader struct {
	reader    io.Reader
	remaining int64
}

// Read truncates the requested buffer to the remaining budget and decrements it by actual bytes read.
// A subsequent read at zero budget fails even if the underlying reader would otherwise return EOF.
// Read ограничивает буфер оставшимся бюджетом и уменьшает его на реально прочитанные байты.
// Следующее чтение при нулевом бюджете завершается ошибкой, даже если исходный reader вернул бы EOF.
func (reader *boundedReader) Read(value []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, errors.New("provider broker request exceeded its bound")
	}
	if int64(len(value)) > reader.remaining {
		value = value[:reader.remaining]
	}
	count, err := reader.reader.Read(value)
	reader.remaining -= int64(count)
	return count, err
}
