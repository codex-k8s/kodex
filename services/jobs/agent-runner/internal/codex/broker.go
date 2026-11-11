package codex

import (
	"bufio"
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
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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
	// providerSafeFailureLog formats only closed stage/category fields;
	// callers must classify first instead of interpolating the original error or provider body.
	// providerSafeFailureLog форматирует только закрытые этапы и категории;
	// вызывающий код сначала классифицирует отказ, не подставляя исходную ошибку или тело провайдера.
	providerSafeFailureLog = "Provider request failed at safe stage: %s; class: %s; detail: NONE; rpc_code: 0; notification: NONE; account_read: NONE; notification_error: NONE; stream_error: NONE"
)

var (
	// ErrProviderAuthentication is the stable missing/invalid-auth sentinel without credential details.
	// ErrProviderAuthentication является устойчивым маркером отсутствующих или неверных реквизитов без их деталей.
	ErrProviderAuthentication = errors.New("OpenCode provider authentication is unavailable")
	// errProviderBrokerResponseInvalid marks a rejected response shape or peer without echoing untrusted input.
	// errProviderBrokerResponseInvalid отмечает отклонённую структуру ответа или peer без вывода недоверенного ввода.
	errProviderBrokerResponseInvalid = errors.New("isolated OpenCode provider response is invalid")
	// errProviderBrokerFailed represents provider/transport failure separately from a malformed response.
	// errProviderBrokerFailed обозначает отказ провайдера или транспорта отдельно от повреждённого ответа.
	errProviderBrokerFailed = errors.New("isolated OpenCode provider failed")
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
	Result        Result                                     `json:"result"`
	Failure       providerBrokerFailure                      `json:"failure,omitempty"`
	OK            bool                                       `json:"ok"`
	Diagnostic    *runtimecontract.ProviderFailureDiagnostic `json:"diagnostic,omitempty"`
	WorkspaceJoin *workspaceJoinProof                        `json:"workspace_join,omitempty"`
	NativeCapture *nativeCaptureProof                        `json:"native_capture,omitempty"`
}

// validCaptureDigest accepts only a lowercase canonical hexadecimal SHA-256 digest of exactly 32 bytes.
// validCaptureDigest принимает только канонический SHA-256 из ровно32 байтов в нижнем шестнадцатеричном регистре.
func validCaptureDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
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
	if input.Provider != "opencode" || input.ModelProvider == nil || security.VerifyProtectedRegular(input.ProviderAuthFile, false) != nil {
		return nil, ErrProviderAuthentication
	}
	file, err := os.OpenFile(input.ProviderAuthFile, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrProviderAuthentication
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > runtimecontract.MaximumProviderAuthBytes {
		return nil, ErrProviderAuthentication
	}
	auth, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumProviderAuthBytes+1))
	expected, digestErr := pinnedProviderDigest(input)
	if err != nil || digestErr != nil || int64(len(auth)) != info.Size() || validateManagedProviderAuthentication(auth, expected, input.ModelProvider.DefinitionKey()) != nil {
		clear(auth)
		return nil, ErrProviderAuthentication
	}
	return auth, nil
}

// validateManagedProviderAuthentication requires the exact configured definition and credential digest, never a ChatGPT bootstrap or CLI auth fallback.
// validateManagedProviderAuthentication требует exact configured definition и credential digest без ChatGPT bootstrap или CLI auth fallback.
func validateManagedProviderAuthentication(auth []byte, digest, definition string) error {
	key, err := runtimecontract.DecodeGatewayProviderAuthentication(auth, definition)
	clear(key)
	actual := sha256.Sum256(auth)
	if err != nil || hex.EncodeToString(actual[:]) != digest {
		return ErrProviderAuthentication
	}
	return nil
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
			return Result{}, errors.New("connect isolated OpenCode provider broker")
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
		return Result{}, errors.New("send isolated OpenCode provider request")
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

// providerSafeFailureClass returns the closed broker category without inspecting private upstream text.
// providerSafeFailureClass возвращает закрытую категорию broker без разбора приватного upstream текста.
func providerSafeFailureClass(err error) string {
	return string(classifyProviderBrokerFailure(err))
}

// logProviderSafeFailure records only the owner-bound stage and closed category, never the raw cause.
// logProviderSafeFailure записывает лишь привязанный этап и закрытую категорию, без исходной причины.
func logProviderSafeFailure(stage providerExecutionStage, err error) {
	diagnostic, _ := providerFailureDetails(stage, err)
	log.Printf(providerSafeFailureLog, stage, diagnostic.Class)
}

// providerFailureDetails creates an unbound coarse observation; native terminal codes remain a separate result.
// bindProviderDiagnostic assigns execution pins before delivery and does not accept old engine notifications.
// providerFailureDetails создаёт непривязанное укрупнённое наблюдение; native terminal codes остаются отдельным результатом.
// bindProviderDiagnostic назначает привязки перед доставкой и не принимает уведомления прежнего движка.
func providerFailureDetails(stage providerExecutionStage, err error) (runtimecontract.ProviderFailureDiagnostic, int64) {
	return runtimecontract.ProviderFailureDiagnostic{Kind: "REQUEST_FAILURE", Stage: string(stage),
		Class: providerSafeFailureClass(err), Detail: "NONE", Notification: "NONE", AccountRead: "NONE", NotificationError: "NONE"}, 0
}

// bindProviderDiagnostic assigns schema and exact execution pins, narrows notification values and
// returns nil if the resulting diagnostic is invalid. It does not authenticate caller-supplied input.
// bindProviderDiagnostic назначает схему и точные привязки исполнения, сужает notification
// и возвращает nil при неверной диагностике. Подлинность переданного input она сама не устанавливает.
func bindProviderDiagnostic(input model.Input, value runtimecontract.ProviderFailureDiagnostic) *runtimecontract.ProviderFailureDiagnostic {
	value.Schema, value.RuntimeRevisionDigest, value.InputDigest = runtimecontract.ProviderFailureDiagnosticSchema, input.RuntimeRevisionDigest, input.InputDigest
	value.ExecutionBindingDigest, value.SessionRef, value.TurnRef, value.Attempt = input.ExecutionBindingDigest, input.SessionRef, input.TurnRef, input.Attempt
	switch value.Notification {
	case "NONE", "UNKNOWN":
	default:
		value.Notification = "UNKNOWN"
	}
	if value.Validate() != nil {
		return nil
	}
	return &value
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
		return errors.New("OpenCode provider broker UID is invalid")
	}
	// Provider-local native logs/cache must remain0600 even when upstream chooses permissive defaults.
	// Provider-local native logs/cache остаются0600 даже при permissive defaults upstream.
	unix.Umask(0o077)
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
		return errors.New("listen isolated OpenCode provider broker")
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
			return errors.New("accept isolated OpenCode provider request")
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
		return errors.New("OpenCode provider sandbox probe is unavailable")
	}
	probeContext, cancel := context.WithTimeout(ctx, providerSandboxProbeTimeout)
	defer cancel()
	uid := strconv.Itoa(os.Geteuid())
	command := exec.CommandContext(probeContext, "/usr/bin/bwrap", "--unshare-user", "--uid", uid, "--gid", uid,
		"--ro-bind", "/", "/", "/usr/bin/true")
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	command.Stdin, command.Stdout, command.Stderr = nil, io.Discard, io.Discard
	if err := execute(command); err != nil {
		return errors.New("OpenCode provider sandbox is unavailable")
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
	bridge, err := startProviderMCPBridge(ctx, request.MCPSocket, request.MCPProxyToken, request.Input, frames.activity)
	if err != nil {
		return writeProviderBrokerFailureAtStage(frames, providerStageMCPBridge, err)
	}
	defer bridge.Close()
	result, err := executeOpenCodeTurn(ctx, request.Input, request.Prompt, bridge.URL(), request.MCPProxyToken, auth, frames.activity)
	joinContext, joinCancel := context.WithTimeout(context.WithoutCancel(ctx), 6*time.Second)
	joinError := bridge.server.Shutdown(joinContext)
	joinCancel()
	if joinError != nil {
		err = errProviderBrokerFailed
	}
	if err != nil {
		logProviderSafeFailure(providerStageOf(err), err)
		return writeProviderBrokerResultFailure(frames, result, err)
	}
	result = normalizeCompletedProviderWorkspace(ctx, request.Input, result, normalizeProviderWorkspaceAccess)
	result = publishCompletedProviderOutbox(ctx, request.Input, result)
	if result.Outcome != "SUCCEEDED" {
		log.Printf("OpenCode provider turn completed with safe failure code: %s", result.FailureCode)
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
	failed := Result{Usage: result.Usage, UsageCompleteness: result.UsageCompleteness, ToolCalls: result.ToolCalls, providerDiagnostic: result.providerDiagnostic, workspaceJoin: result.workspaceJoin}
	if result.nativeCapture != nil && result.nativeCapture.sealed && result.NativeSession != nil && *result.NativeSession == result.nativeCapture.Checkpoint {
		value := *result.NativeSession
		failed.NativeSession = &value
		failed.nativeCapture = result.nativeCapture
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
func startProviderMCPBridge(ctx context.Context, socketPath, localToken string, input model.Input, observations ...func(runtimecontract.RuntimeActivity) error) (*providerMCPBridge, error) {
	if len(observations) > 1 || os.Geteuid() != 10002 || socketPath != "/run/kodex/provider/mcp-authority.sock" || len(localToken) != 64 {
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
	reverse := &httputil.ReverseProxy{Transport: transport, ErrorLog: log.New(io.Discard, "", 0), ModifyResponse: restoreNativeMCPResponse,
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
		if request.URL.Path == "/model-measurements" && len(observations) == 1 {
			serveModelMeasurement(writer, request, localToken, input.RuntimeRevisionDigest, observations[0])
			return
		}
		if bindNativeMCPRequest(request, input, localToken) != nil {
			http.Error(writer, "invalid managed native tool identity", http.StatusBadRequest)
			return
		}
		if request.URL.Path == "/workspace/mcp" && runtimecontract.RuntimeWorkspaceToolsAvailable(input) &&
			request.Method == http.MethodPost && request.URL.RawQuery == "" && subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+localToken)) == 1 {
			reverse.ServeHTTP(writer, request)
			return
		}
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
