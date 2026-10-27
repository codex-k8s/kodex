package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// processGrace bounds graceful stdio completion and the separate interrupt attempt, not the whole
	// turn or the sum of later termination/drain budgets.
	// processGrace ограничивает graceful завершение stdio и отдельную попытку interrupt, а не весь
	// ход или сумму последующих бюджетов termination/drain.
	processGrace = 10 * time.Second
	// terminationGrace bounds each individual wait/reader-drain stage during termination and escalation.
	// terminationGrace ограничивает каждый отдельный этап wait или reader-drain при остановке и эскалации.
	terminationGrace = 2 * time.Second
	// maximumArchiveBytes caps accepted rollout bytes, with one extra read byte used to detect growth.
	// maximumArchiveBytes ограничивает принимаемый архив; дополнительный байт чтения позволяет заметить рост.
	maximumArchiveBytes = 64 << 20
	// maximumDiagnosticSize bounds discarded stderr and still makes overflow a reported failure.
	// maximumDiagnosticSize ограничивает отбрасываемый stderr, сохраняя превышение как сообщаемый отказ.
	maximumDiagnosticSize = 1 << 20
	// maximumRequestBytes limits marshaled app-server requests before their newline, not Marshal allocation.
	// maximumRequestBytes ограничивает уже кодированный запрос до добавления newline, а не выделение памяти Marshal.
	maximumRequestBytes = 2 << 20
	// maximumAppServerFrameBytes permits large terminal aggregates without relaxing per-field semantic
	// quotas. Scanner newline overhead is separate from this encoded JSON limit.
	// maximumAppServerFrameBytes допускает крупный итоговый агрегат без ослабления смысловых квот полей.
	// Служебные байты newline в Scanner учитываются отдельно от лимита JSON.
	maximumAppServerFrameBytes = 8 << 20
	// appServerMessageQueueCapacity keeps the bounded queue's nominal frame-byte budget at 64 MiB;
	// JSON objects and other runtime allocations are not included in that arithmetic bound.
	// appServerMessageQueueCapacity сохраняет номинальный кадровый бюджет очереди64 MiB;
	// объекты JSON и другие выделения runtime в этот арифметический предел не входят.
	appServerMessageQueueCapacity = (64 << 20) / maximumAppServerFrameBytes
	// mcpReadinessTimeout caps required-tool readiness polling under the caller's existing context.
	// mcpReadinessTimeout ограничивает ожидание готовности обязательных инструментов в контексте вызывающего.
	mcpReadinessTimeout = 15 * time.Second
	// mcpReadinessPoll spaces not-yet-ready status reads without a busy loop or background poller.
	// mcpReadinessPoll разделяет чтения ещё не готового статуса без активного цикла или фонового poller.
	mcpReadinessPoll = 100 * time.Millisecond
)

// rolloutPathPattern recognizes only the retained workspace-relative dated rollout layout; canonical
// path and traversal-marker checks are performed by validRolloutRelativePath as well.
// rolloutPathPattern узнаёт только закреплённую относительную структуру dated rollout;
// каноничность пути и маркеры обхода дополнительно проверяет validRolloutRelativePath.
var rolloutPathPattern = regexp.MustCompile(`^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$`)

var (
	// ErrAuthorityRequestUnsupported preserves a typed authority denial without granting a model's request.
	// ErrAuthorityRequestUnsupported сохраняет типизированный отказ в полномочиях, не выдавая их по запросу модели.
	ErrAuthorityRequestUnsupported = errors.New("Codex app-server authority request is unsupported")
	// ErrRequiredMCPUnavailable marks failure of a mandatory tool path rather than optional degradation.
	// ErrRequiredMCPUnavailable обозначает отказ обязательного пути инструментов, а не необязательное ухудшение.
	ErrRequiredMCPUnavailable = errors.New("Codex required MCP runtime is unavailable")
	// errAccountReadResponseInvalid marks rejected account response structure without exposing account fields.
	// errAccountReadResponseInvalid обозначает отклонённую структуру ответа аккаунта без раскрытия его полей.
	errAccountReadResponseInvalid = errors.New("Codex app-server account/read response is invalid")
	// errAppServerExited retains unsuccessful process exit independently of the original protocol cause.
	// errAppServerExited сохраняет неуспешный выход процесса независимо от исходной протокольной причины.
	errAppServerExited = errors.New("Codex app-server exited unsuccessfully")
	// errAppServerStreamDeadline records an exhausted reader-drain budget without treating timeout as joined.
	// errAppServerStreamDeadline фиксирует исчерпание reader-drain бюджета, не приравнивая timeout к завершению.
	errAppServerStreamDeadline = errors.New("Codex app-server stream shutdown deadline exceeded")
)

// providerExecutionStage identifies internal diagnostic boundaries; the string type itself does not
// validate arbitrary values or authorize execution at a stage.
// providerExecutionStage обозначает внутренние диагностические границы; сам строковый тип не проверяет
// произвольные значения и не разрешает выполнение этапа.
type providerExecutionStage string

const (
	// providerStageSelection marks rejection of runtime selection before credentials or processes are used.
	// providerStageSelection отмечает отказ выбора runtime до обращения к реквизитам или процессам.
	providerStageSelection providerExecutionStage = "SELECTION"
	// providerStageContext identifies missing or invalid pinned context and its execution binding.
	// providerStageContext обозначает отсутствие или неверную привязку закреплённого контекста исполнения.
	providerStageContext providerExecutionStage = "CONTEXT"
	// providerStageBrokerRequest classifies the broker's peer/request boundary, not a caller-issued authority.
	// providerStageBrokerRequest классифицирует границу peer/request broker, а не выдачу полномочий вызывающим.
	providerStageBrokerRequest providerExecutionStage = "BROKER_REQUEST"
	// providerStageAuthRead identifies protected auth-file acquisition rather than a remote login result.
	// providerStageAuthRead обозначает получение защищённого auth-файла, а не результат внешнего входа.
	providerStageAuthRead providerExecutionStage = "AUTH_READ"
	// providerStageMCPBinding marks the fixed local authority socket/capability binding checks.
	// providerStageMCPBinding отмечает проверки привязки фиксированного локального authority socket и capability.
	providerStageMCPBinding providerExecutionStage = "MCP_BINDING"
	// providerStageMCPBridge identifies failure to construct the provider's bounded local MCP transport.
	// providerStageMCPBridge обозначает отказ создания ограниченного локального транспорта MCP провайдера.
	providerStageMCPBridge providerExecutionStage = "MCP_BRIDGE"
	// providerStageHomePrepare identifies preparation of pinned configuration and the working home/directory.
	// providerStageHomePrepare обозначает подготовку закреплённой конфигурации и рабочего home или каталога.
	providerStageHomePrepare providerExecutionStage = "HOME_PREPARE"
	// providerStageAccountPin marks local credential-content pin checks, not provider financial eligibility.
	// providerStageAccountPin отмечает локальную проверку digest реквизитов, а не финансовый допуск провайдера.
	providerStageAccountPin providerExecutionStage = "ACCOUNT_PIN"
	// providerStageArchiveRestore names the retained-session preflight boundary; its current verifier is
	// an explicit no-op rather than validation of a caller-supplied archive locator.
	// providerStageArchiveRestore именует preflight сохранённой сессии; текущая проверка явно ничего
	// не делает вместо проверки переданного вызывающим archive locator.
	providerStageArchiveRestore providerExecutionStage = "ARCHIVE_RESTORE"
	// providerStageProcessStart identifies app-server creation/start failures before protocol initialization.
	// providerStageProcessStart обозначает отказ создания или старта app-server до инициализации протокола.
	providerStageProcessStart providerExecutionStage = "PROCESS_START"
	// providerStageInitialize groups initialize/initialized and initial bound process-observation failures.
	// providerStageInitialize объединяет отказы initialize/initialized и первого привязанного наблюдения процесса.
	providerStageInitialize providerExecutionStage = "INITIALIZE"
	// providerStageSkills identifies exact pinned skill reconciliation/discovery before a turn begins.
	// providerStageSkills обозначает точное согласование и обнаружение закреплённых skills до начала хода.
	providerStageSkills providerExecutionStage = "SKILLS"
	// providerStageAccountRead separates account RPC/schema outcomes from earlier local credential checks.
	// providerStageAccountRead отделяет результаты account RPC и его схемы от прежних локальных проверок реквизитов.
	providerStageAccountRead providerExecutionStage = "ACCOUNT_READ"
	// providerStageThreadCall identifies the execution thread RPC boundary, distinct from binding its result.
	// providerStageThreadCall обозначает RPC потока исполнения отдельно от привязки его результата.
	providerStageThreadCall providerExecutionStage = "THREAD_CALL"
	// providerStageThreadBind identifies validation of the returned thread against the current execution.
	// providerStageThreadBind обозначает сверку возвращённого потока с текущим исполнением.
	providerStageThreadBind providerExecutionStage = "THREAD_BIND"
	// providerStageMCPReadiness records required MCP readiness failure before workload execution can proceed.
	// providerStageMCPReadiness фиксирует отказ обязательной готовности MCP до продолжения работы.
	providerStageMCPReadiness providerExecutionStage = "MCP_READINESS"
	// providerStageUsageBaseline marks capture of prior thread usage so a resumed turn charges only its delta.
	// providerStageUsageBaseline отмечает фиксацию прежнего расхода потока для учёта лишь дельты возобновлённого хода.
	providerStageUsageBaseline providerExecutionStage = "USAGE_BASELINE"
	// providerStageTurnParameters identifies construction/verification of the immutable current turn parameters.
	// providerStageTurnParameters обозначает формирование и проверку неизменяемых параметров текущего хода.
	providerStageTurnParameters providerExecutionStage = "TURN_PARAMETERS"
	// providerStageTurnStart identifies the turn/start RPC boundary, not successful turn completion.
	// providerStageTurnStart обозначает границу RPC turn/start, а не успешное завершение хода.
	providerStageTurnStart providerExecutionStage = "TURN_START"
	// providerStageTerminalWait identifies notification/request processing while awaiting the authoritative terminal.
	// providerStageTerminalWait обозначает обработку уведомлений и запросов при ожидании авторитетного итога.
	providerStageTerminalWait providerExecutionStage = "TERMINAL_WAIT"
	// providerStageThreadRead separates final thread/source-location binding from the terminal notification itself.
	// providerStageThreadRead отделяет итоговую привязку thread/source location от самого итогового уведомления.
	providerStageThreadRead providerExecutionStage = "THREAD_READ"
	// providerStageProcessStop identifies process/reader shutdown after the protocol terminal was observed.
	// providerStageProcessStop обозначает остановку процесса и readers после получения протокольного итога.
	providerStageProcessStop providerExecutionStage = "PROCESS_STOP"
	// providerStageTerminalResult identifies validation of the completed state into the public Result projection.
	// providerStageTerminalResult обозначает проверку завершённого состояния при построении публичной проекции Result.
	providerStageTerminalResult providerExecutionStage = "TERMINAL_RESULT"
	// providerStageArchiveCapture marks archive publication/proof capture only after the stopped-writer checks.
	// providerStageArchiveCapture отмечает публикацию и захват доказательства архива после проверок остановки writer.
	providerStageArchiveCapture providerExecutionStage = "ARCHIVE_CAPTURE"
	// providerStageUnknown is the diagnostic fallback when no typed stage wrapper is present.
	// providerStageUnknown является запасным диагностическим значением при отсутствии типизированной обёртки этапа.
	providerStageUnknown providerExecutionStage = "UNKNOWN"
)

// providerStageError wraps an internal stage and original cause while exposing a fixed outer message;
// Unwrap intentionally preserves the cause, so the entire chain is not safe to serialize.
// providerStageError связывает внутренний этап с исходной причиной, выдавая фиксированный внешний текст;
// Unwrap намеренно сохраняет причину, поэтому всю цепочку нельзя считать безопасной для сериализации.
type providerStageError struct {
	stage providerExecutionStage
	err   error
}

// Error returns only the fixed execution-stage message, never the stage's wrapped private cause.
// Error возвращает только фиксированное сообщение этапа исполнения без вложенной приватной причины.
func (failure *providerStageError) Error() string { return "Codex provider execution stage failed" }

// Unwrap exposes the original cause for errors.Is/As; the receiver must be nonnil and callers must
// use the safe diagnostic projection rather than printing this preserved chain.
// Unwrap открывает исходную причину для errors.Is/As; получатель обязан быть ненулевым, а вызывающий
// код должен использовать безопасную проекцию диагностики вместо вывода сохранённой цепочки.
func (failure *providerStageError) Unwrap() error { return failure.err }

// atProviderStage leaves nil success untouched and otherwise wraps the supplied stage without
// normalizing it or logging the original error.
// atProviderStage сохраняет nil успех и иначе оборачивает переданный этап без его нормализации
// или вывода исходной ошибки в лог.
func atProviderStage(stage providerExecutionStage, err error) error {
	if err == nil {
		return nil
	}
	return &providerStageError{stage: stage, err: err}
}

// providerStageOf returns the first wrapped typed stage or UNKNOWN when absent; it does not validate
// the stored string and requires any matched typed wrapper to be nonnil.
// providerStageOf возвращает первый вложенный типизированный этап либо UNKNOWN при отсутствии;
// сохранённая строка не проверяется, а найденная typed wrapper обязана быть ненулевой.
func providerStageOf(err error) providerExecutionStage {
	var failure *providerStageError
	if errors.As(err, &failure) {
		return failure.stage
	}
	return providerStageUnknown
}

// streamEvent carries either a parsed wire message or reader failure inside the private reader channel;
// it is not a public activity or a terminal result by itself.
// streamEvent переносит разобранное wire сообщение либо ошибку reader по приватному каналу;
// само оно не является публичным событием или итоговым результатом.
type streamEvent struct {
	message wireMessage
	err     error
}

// appServerCallFailure retains call/notification metadata and the original cause for classification.
// Not every stored string is already normalized, and Error delegates to the cause; safe outward logs
// must use the closed mapper/stage wrapper rather than treating this struct as a redaction boundary.
// appServerCallFailure сохраняет call/notification метаданные и исходную причину для классификации.
// Не все строки уже нормализованы, а Error делегирует причине; безопасный внешний лог использует
// закрытую проекцию или stage wrapper, а не считает эту структуру границей сокрытия.
type appServerCallFailure struct {
	financialUnavailable bool
	detail               string
	code                 int64
	notification         string
	notificationError    string
	accountRead          string
	err                  error
}

// Error delegates to the nonnil cause and may contain its private text; this internal method is
// not the outward diagnostic redaction layer.
// Error делегирует ненулевой причине и может содержать её приватный текст; этот внутренний метод
// не является внешним слоем сокрытия диагностики.
func (failure *appServerCallFailure) Error() string { return failure.err.Error() }

// Unwrap retains call failure identity through the original cause; a nonnil receiver is required.
// Unwrap сохраняет идентичность отказа через исходную причину; получатель обязан быть ненулевым.
func (failure *appServerCallFailure) Unwrap() error { return failure.err }

// callFailure attaches an internal detail to the supplied cause without validating or redacting them;
// callers supply a nonnil cause and later safe classification handles outward diagnostics.
// callFailure прикрепляет внутренний detail к причине без их проверки или сокрытия;
// вызывающий код передаёт ненулевую причину и позже классифицирует внешнюю диагностику.
func callFailure(detail string, err error) error {
	return &appServerCallFailure{detail: detail, err: err}
}

// notificationFailure maps exact parser messages and typed usage/MCP failures to closed categories
// while retaining the original nonnil cause and method internally. Unknown text never becomes a
// category; the log mapper still normalizes the retained method before emitting it.
// notificationFailure переводит точные сообщения parser и typed usage/MCP ошибки в закрытые категории,
// сохраняя исходную ненулевую причину и метод внутри. Неизвестный текст не становится категорией;
// сохранённый метод перед выводом дополнительно нормализует проекция логов.
func notificationFailure(method string, err error) error {
	category := "UNKNOWN"
	switch err.Error() {
	case "Codex app-server notification method is not allowed":
		category = "METHOD"
	case "Codex app-server notification is invalid":
		category = "ENVELOPE"
	case "Codex app-server notification tuple is invalid", "Codex app-server token usage tuple is invalid":
		category = "TUPLE"
	case "Codex app-server item timestamp is invalid":
		category = "TIMESTAMP"
	case "Codex app-server thread item is invalid", "Codex app-server thread item type is invalid",
		"Codex app-server tagged thread item is invalid", "Codex app-server thread item id is invalid", "Codex app-server MCP UI metadata is invalid":
		category = "ITEM"
	case "Codex app-server agent message text is invalid", "Codex app-server agent message phase is invalid",
		"Codex app-server agent message is invalid", "Codex app-server agent message changed after completion",
		"Codex app-server published message limit exceeded", "Codex app-server published message exceeds its bound":
		category = "MESSAGE"
	case "Codex app-server token usage is invalid", "Codex app-server token usage breakdown is invalid", "Codex app-server token usage delta is invalid":
		category = "TOKEN_USAGE"
	case "Codex app-server terminal notification is invalid", "Codex app-server turn lifecycle is incomplete",
		"Codex app-server successful turn carries an error", "Codex app-server completed with an unfinished native tool",
		"Codex app-server completed without a final message", "Codex app-server terminal status is invalid":
		category = "TERMINAL"
	case "Codex app-server thread started notification is invalid", "Codex app-server turn started notification is invalid":
		category = "LIFECYCLE"
	case "Codex app-server MCP startup notification is invalid", "Codex app-server MCP startup thread is invalid",
		"Codex app-server MCP startup diagnostic is invalid", "Codex app-server MCP startup failure reason is invalid":
		category = "MCP"
	case "Codex app-server error notification is invalid", "Codex app-server error retry flag is invalid":
		category = "PROVIDER_ERROR"
	}
	if errors.Is(err, ErrRequiredMCPUnavailable) {
		category = "MCP"
	}
	var usageFailure *tokenUsageFailure
	if (method == "thread/tokenUsage/updated" || method == "rawResponse/completed") && errors.As(err, &usageFailure) {
		category = safeTokenUsageFailureReason(usageFailure.reason)
	}
	return &appServerCallFailure{detail: "NOTIFICATION_INVALID", notification: method, notificationError: category, err: err}
}

// appServer owns one child process, explicit stdio, ordered protocol requests and reader/wait state.
// Reader-stop signalling and stream closure have separate once guards; protocol calls and mutable state are serialized by callers,
// not made concurrently safe by the struct. Parent-owned streams outlive Wait until reader drain/join.
// appServer владеет одним процессом, явными stdio, упорядоченными запросами и состоянием reader/wait.
// Сигнал остановки reader и закрытие streams имеют отдельные once guard; запросы и mutable state упорядочивает вызывающий код,
// а не сама структура. Parent-owned streams живут после Wait до drain/join readers.
type appServer struct {
	command        *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.Closer
	stderr         io.Closer
	messages       <-chan streamEvent
	wait           <-chan error
	diagnostics    <-chan error
	readerStop     chan struct{}
	readerDone     <-chan struct{}
	stopOnce       sync.Once
	streamsOnce    sync.Once
	waited         bool
	waitErr        error
	readerRead     bool
	diagnosticRead bool
	diagnosticErr  error
	nextID         int64
}

// executeLocal runs the same pinned provider turn pipeline without an optional input-proof observer;
// it does not add a separate executor or bypass any context/credential/protocol checks.
// executeLocal запускает тот же закреплённый pipeline без необязательного наблюдателя input proof;
// отдельного executor или обхода context, credential и protocol проверок нет.
func executeLocal(ctx context.Context, input model.Input, prompt []byte, mcpProxyToken string, onActivity func(runtimecontract.RuntimeActivity) error) (result Result, resultErr error) {
	return executeLocalWithInputProof(ctx, input, prompt, mcpProxyToken, onActivity, nil)
}

// executeLocalWithInputProof validates selection, context, auth pin and managed readiness before
// starting the child, then binds initialization, skills, account, thread and required tools before
// one turn. It waits for terminal/thread read/final financial observation and process stop before
// capturing a sealed rollout. On failure the deferred path preserves validated usage and attempts
// capture only when writer/readers are stopped; cleanup failures never manufacture a successful turn.
// executeLocalWithInputProof проверяет выбор, context, auth pin и managed readiness до старта ребёнка,
// затем связывает initialize, skills, account, thread и обязательные инструменты перед одним ходом.
// Она ждёт terminal, thread read, итоговое финансовое наблюдение и stop до захвата закрытого архива.
// При отказе отложенный путь сохраняет проверенный usage и пытается захватить архив лишь после
// остановки writer/readers; ошибки очистки не превращаются в успешный ход.
func executeLocalWithInputProof(ctx context.Context, input model.Input, prompt []byte, mcpProxyToken string, onActivity func(runtimecontract.RuntimeActivity) error, proofObserver providerInputProofObserver) (result Result, resultErr error) {
	if err := validateRuntimeSelection(input); err != nil {
		return Result{}, atProviderStage(providerStageSelection, err)
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil {
		return Result{}, atProviderStage(providerStageContext, err)
	}
	if err := verifyProviderContext(input, snapshot); err != nil {
		return Result{}, atProviderStage(providerStageContext, err)
	}
	ctx, cancelContext := snapshot.BoundExecutionContext(ctx)
	defer cancelContext()
	if onActivity == nil {
		return Result{}, atProviderStage(providerStageSelection, errors.New("Codex runtime activity consumer is unavailable"))
	}
	financialIdentity, pinErr := readPinnedFinancialIdentity(input)
	if pinErr != nil {
		return Result{}, atProviderStage(providerStageAccountPin, pinErr)
	}
	if err := verifyRestoreArchive(input); err != nil {
		return Result{}, atProviderStage(providerStageArchiveRestore, err)
	}
	if err := runtimecontract.ValidateManagedMCPReadiness(input, time.Now()); err != nil {
		return Result{}, atProviderStage(providerStageMCPReadiness, err)
	}
	server, err := startAppServer(input, mcpProxyToken)
	if err != nil {
		return Result{}, atProviderStage(providerStageProcessStart, err)
	}
	state := newProtocolState(input.CodexSessionID)
	state.onActivity = onActivity
	state.financialExpectedAccount = financialIdentity
	state.financialEpoch = 1
	state.financialBoundEpoch = 1
	captureAttempted := false
	defer func() {
		if state.resumeSource != nil {
			defer state.resumeSource.file.Close()
		}
		if resultErr != nil {
			// Учитываем только ранее проверенные измерения, даже если terminal,
			// thread/read, остановка процесса или захват архива завершились ошибкой.
			result = state.measuredResult()
			if !captureAttempted {
				result = captureFailedRollout(input, server, state, result)
			}
		}
	}()
	initialize := map[string]any{
		"clientInfo": map[string]string{"name": "kodex-agent-runner", "title": "Kodex agent-runner", "version": "1"},
		// Нужен закреплённый turn/start.additionalContext. Это не расширяет
		// закрытый реестр методов, native tools либо серверные полномочия.
		"capabilities": map[string]any{"experimentalApi": true, "optOutNotificationMethods": suppressedNotificationMethods},
	}
	raw, err := server.call(ctx, state, "initialize", initialize)
	if err != nil {
		return Result{}, atProviderStage(providerStageInitialize, server.abort(ctx, state, err))
	}
	if err := state.initialize(raw, input.CodexHome); err != nil {
		return Result{}, atProviderStage(providerStageInitialize, server.abort(ctx, state, err))
	}
	if err := server.notifyInitialized(); err != nil {
		return Result{}, atProviderStage(providerStageInitialize, server.abort(ctx, state, err))
	}
	if err := state.publishProviderProcess(input); err != nil {
		return Result{}, atProviderStage(providerStageInitialize, server.abort(ctx, state, err))
	}
	if err := server.configureContextSkills(ctx, state, input, snapshot); err != nil {
		return Result{}, atProviderStage(providerStageSkills, server.abort(ctx, state, err))
	}
	raw, err = server.call(ctx, state, "account/read", map[string]bool{"refreshToken": false})
	if err := classifyAccountReadResponse(raw, err); err != nil {
		return Result{}, atProviderStage(providerStageAccountRead, server.abort(ctx, state, err))
	}
	var financialAccount struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	_ = json.Unmarshal(raw, &financialAccount)
	state.financialEnabled = financialIdentity != "" && financialAccount.Account != nil && financialAccount.Account.Type == "chatgpt"
	state.financialBoundEpoch = state.financialEpoch
	if err := server.observeFinancial(ctx, state); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.bindExecutionThread(ctx, state, input); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.waitRequiredMCP(ctx, state, RequiredMCPToolNames(input)); err != nil {
		return Result{}, atProviderStage(providerStageMCPReadiness, server.abort(ctx, state, err))
	}
	if err := state.captureUsageBaseline(); err != nil {
		return Result{}, atProviderStage(providerStageUsageBaseline, server.abort(ctx, state, err))
	}
	if err := server.startTurnWithInputProof(ctx, state, input, prompt, proofObserver); err != nil {
		return Result{}, atProviderStage(providerStageOf(err), server.abort(ctx, state, err))
	}
	if err := server.waitTerminal(ctx, state); err != nil {
		return Result{}, atProviderStage(providerStageTerminalWait, server.abort(ctx, state, err))
	}
	raw, err = server.call(ctx, state, "thread/read", map[string]any{"threadId": state.threadID, "includeTurns": false})
	if err != nil {
		return Result{}, atProviderStage(providerStageThreadRead, server.abort(ctx, state, err))
	}
	if err := state.bindThreadRead(raw); err != nil {
		return Result{}, atProviderStage(providerStageThreadRead, server.abort(ctx, state, err))
	}
	if err := server.observeFinancialFinal(ctx, state); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.stop(state); err != nil {
		return Result{}, atProviderStage(providerStageProcessStop, err)
	}
	result, err = state.terminalResult()
	if err != nil {
		return Result{}, atProviderStage(providerStageTerminalResult, err)
	}
	if !server.captureReady() {
		return Result{}, atProviderStage(providerStageArchiveCapture, errors.New("Codex rollout writer is not stopped"))
	}
	captureAttempted = true
	if state.resumeSource != nil {
		captured, err := state.resumeSource.capture(input, server)
		if err != nil {
			return Result{}, atProviderStage(providerStageArchiveCapture, err)
		}
		result = withRolloutCapture(result, captured.rolloutCapture)
		result.ArchivePath = captured.ArchivePath
		return result, nil
	}
	_, _, _, _, err = captureRollout(input, state.threadPath)
	if err != nil {
		return Result{}, atProviderStage(providerStageArchiveCapture, err)
	}
	captured, err := CaptureStoppedRollout(input, state.threadID, state.threadPath)
	if err != nil {
		return Result{}, atProviderStage(providerStageArchiveCapture, err)
	}
	result = withRolloutCapture(result, captured.rolloutCapture)
	result.ArchivePath = captured.ArchivePath
	return result, nil
}

// captureFailedRollout best-effort attaches actual archive proof to measured failure data only after
// captureReady. It uses the held resume source or publishes group/mode on a fresh rollout before
// capture; failure returns the original Result without inventing source pins or changing its usage.
// captureFailedRollout по возможности добавляет настоящее доказательство архива к измерениям отказа
// лишь после captureReady. Она использует удерживаемый resume source либо публикует group/mode нового
// архива перед захватом; отказ возвращает исходный Result без выдуманных source pins и изменения usage.
func captureFailedRollout(input model.Input, server *appServer, state *protocolState, result Result) Result {
	if !server.captureReady() {
		return result
	}
	if state.resumeSource != nil {
		captured, err := state.resumeSource.capture(input, server)
		if err != nil {
			return result
		}
		result = withRolloutCapture(result, captured.rolloutCapture)
		result.ArchivePath = captured.ArchivePath
		return result
	}
	if _, _, _, _, err := captureRollout(input, state.threadPath); err != nil {
		return result
	}
	captured, err := CaptureStoppedRollout(input, state.threadID, state.threadPath)
	if err != nil {
		return result
	}
	result = withRolloutCapture(result, captured.rolloutCapture)
	result.ArchivePath = captured.ArchivePath
	return result
}

// CaptureStoppedRollout reads/hashes the actual protected source and validates its exact input-bound
// proof and local writer metadata before sealing it. Trusted callers must have completed bounded
// writer/reader join first; this function does not stop a process or accept a caller-provided digest,
// and it does not alter owner/group/mode.
// CaptureStoppedRollout читает и хеширует настоящий защищённый источник, проверяя точную привязку proof
// к input и метаданные локального writer до установки seal. Доверенный вызывающий код обязан заранее
// завершить bounded join writer/readers; функция сама не останавливает процесс, не принимает готовый
// digest и не меняет owner/group/mode.
func CaptureStoppedRollout(input model.Input, sessionID, path string) (Result, error) {
	relative, err := filepath.Rel(input.WorkspaceRoot, path)
	if err != nil {
		return Result{}, errProviderBrokerResponseInvalid
	}
	proof := &rolloutCaptureProof{Schema: rolloutCaptureSchema, ExecutionBindingDigest: input.ExecutionBindingDigest,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, Attempt: input.Attempt,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, SessionID: sessionID, RelativePath: filepath.ToSlash(relative)}
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return Result{}, errProviderBrokerResponseInvalid
	}
	proof.SizeBytes = info.Size()
	proof.SHA256, err = digestArchive(file, info)
	closeErr := file.Close()
	if err != nil || closeErr != nil || !proof.bound(input) {
		return Result{}, errProviderBrokerResponseInvalid
	}
	result := Result{SessionID: proof.SessionID, ArchivePath: path, ArchiveRelativePath: proof.RelativePath, ArchiveSHA256: proof.SHA256, ArchiveSizeBytes: proof.SizeBytes}
	if verifyRolloutCapture(input, result, proof, uint32(os.Geteuid())) != nil {
		return Result{}, errProviderBrokerResponseInvalid
	}
	proof.sealed = true
	return withRolloutCapture(result, proof), nil
}

// verifyRolloutCapture compares exact input/result/proof pins, opens the protected source, checks
// owner against the authenticated writer UID and group29000 for the isolated provider, then hashes
// and reopens to compare inode/metadata stability. The UID comes from trusted peer state, not payload;
// these observations do not replace the caller's stopped-writer requirement.
// verifyRolloutCapture сверяет input/result/proof, открывает защищённый источник, проверяет owner
// по доверенному UID writer и group29000 для изолированного провайдера, затем хеширует и повторно
// открывает для сверки inode и метаданных. UID берётся из доверенного peer, не payload;
// эти наблюдения не заменяют предварительную остановку writer вызывающим кодом.
func verifyRolloutCapture(input model.Input, result Result, proof *rolloutCaptureProof, writerUID uint32) error {
	if !proof.bound(input) || !result.matchesCapture(proof) || result.ArchivePath != filepath.Join(input.WorkspaceRoot, filepath.FromSlash(proof.RelativePath)) {
		return errProviderBrokerResponseInvalid
	}
	file, info, err := openProtectedFile(input.WorkspaceRoot, result.ArchivePath)
	if err != nil {
		return errProviderBrokerResponseInvalid
	}
	defer file.Close()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != writerUID || (writerUID == providerWriterUID && stat.Gid != 29000) || info.Mode().Perm() != 0o640 || info.Size() != proof.SizeBytes {
		return errProviderBrokerResponseInvalid
	}
	digest, err := digestArchive(file, info)
	if err != nil || digest != proof.SHA256 {
		return errProviderBrokerResponseInvalid
	}
	current, currentInfo, err := openProtectedFile(input.WorkspaceRoot, result.ArchivePath)
	if err != nil {
		return errProviderBrokerResponseInvalid
	}
	defer current.Close()
	currentStat, currentOK := currentInfo.Sys().(*syscall.Stat_t)
	if !currentOK || !os.SameFile(info, currentInfo) || currentInfo.Size() != info.Size() || currentInfo.Mode() != info.Mode() ||
		currentStat.Uid != stat.Uid || currentStat.Gid != stat.Gid || currentStat.Nlink != stat.Nlink || currentStat.Mtim != stat.Mtim || currentStat.Ctim != stat.Ctim {
		return errProviderBrokerResponseInvalid
	}
	return nil
}

// captureReady requires observed process Wait, both reader completions and ESRCH for the child's
// process group. A nonzero exit does not prohibit capture, but missing completion or a remaining group does.
// captureReady требует наблюдения Wait, завершения обоих readers и ESRCH для process group ребёнка.
// Ненулевой exit не запрещает захват, но отсутствие завершения или оставшаяся группа запрещают.
func (server *appServer) captureReady() bool {
	return server.waited && server.readerRead && server.diagnosticRead && server.command != nil && server.command.Process != nil &&
		errors.Is(syscall.Kill(-server.command.Process.Pid, 0), syscall.ESRCH)
}

// turnStartParams constructs current snapshot input and complete instruction chunks with exact model,
// cwd and approval policy on every attempt, including resume. Validated effective effort is sent only
// for reasoning-capable models; configured personality is explicit, without a collaboration preset.
// turnStartParams формирует ввод текущего snapshot и полные instruction chunks с точными model,
// cwd и approval policy на каждой попытке, включая resume. Проверенный effort передаётся только
// reasoning-модели, personality — явно из конфигурации, без collaboration preset.
func turnStartParams(input model.Input, threadID string, prompt []byte) (map[string]any, error) {
	overlay, err := runtimecontract.ParseConfigOverlay(input.ConfigOverlay)
	if err != nil || runtimecontract.ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort, input.ReasoningMode) != nil {
		return nil, ErrRuntimeProfile
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil {
		return nil, err
	}
	items, err := contextInputItems(input, snapshot, prompt, time.Now())
	if err != nil {
		return nil, err
	}
	params := map[string]any{"threadId": threadID, "cwd": workingDirectory(input), "model": input.Model,
		"approvalPolicy": input.CodexApprovalPolicy, "input": items}
	current, err := currentInstructionContext(input.Instructions)
	if err != nil {
		return nil, err
	}
	params["additionalContext"] = current
	// Resume не должен сохранять reasoning/personality предыдущей attempt.
	if input.ReasoningMode == runtimecontract.ReasoningSupported {
		params["effort"] = input.EffectiveReasoningEffort
	}
	if overlay.Personality != "" {
		params["personality"] = overlay.Personality
	}
	return params, nil
}

// classifyAccountReadResponse preserves RPC failure, otherwise validates the closed outer/account
// and optional routing shapes. Missing/null account becomes authentication denial only when required;
// valid local schema is not remote authentication or a financial admission decision.
// classifyAccountReadResponse сохраняет RPC отказ либо проверяет закрытую внешнюю, account и optional
// routing схемы. Отсутствующий/null account даёт auth отказ лишь при обязательном входе;
// корректная локальная схема не является внешней авторизацией или финансовым допуском.
func classifyAccountReadResponse(raw json.RawMessage, callErr error) error {
	if callErr != nil {
		return callErr
	}
	fields, err := decodeObject(raw, schema([]string{"requiresOpenaiAuth"}, "account", "requiresOpenaiAuth", "workspaceRouting"))
	if err != nil {
		return errAccountReadResponseInvalid
	}
	var requiresOpenAIAuth bool
	if strictDecode(fields["requiresOpenaiAuth"], &requiresOpenAIAuth) != nil {
		return errAccountReadResponseInvalid
	}
	if routing, present := fields["workspaceRouting"]; present && !validAccountReadWorkspaceRouting(routing) {
		return errAccountReadResponseInvalid
	}
	account, present := fields["account"]
	if !present || bytes.Equal(bytes.TrimSpace(account), []byte("null")) {
		if requiresOpenAIAuth {
			return ErrProviderAuthentication
		}
		return nil
	}
	if validateAccountReadAccount(account) != nil {
		return errAccountReadResponseInvalid
	}
	return nil
}

// validAccountReadWorkspaceRouting validates the pinned experimental response shape, bounds strings
// and permits only registered routing values or null. It does not use backendOrigin as a URL to fetch,
// persist private IDs or assign account authority/network policy.
// validAccountReadWorkspaceRouting проверяет закреплённую experimental схему, длины строк и только
// зарегистрированные routing значения либо null. Она не обращается по backendOrigin, не сохраняет
// приватные ID и не назначает account authority или сетевую policy.
func validAccountReadWorkspaceRouting(raw json.RawMessage) bool {
	if len(raw) > 16<<10 {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	fields, err := decodeObject(raw, schema(
		[]string{"chatgptAccountId", "backendOrigin", "accountRoutingOverride"},
		"chatgptAccountId", "backendOrigin", "accountRoutingOverride"))
	if err != nil {
		return false
	}
	if _, err := decodeBoundedString(fields["chatgptAccountId"], 512); err != nil {
		return false
	}
	if _, err := decodeBoundedString(fields["backendOrigin"], 4096); err != nil {
		return false
	}
	override, err := decodeBoundedString(fields["accountRoutingOverride"], 64)
	if err != nil {
		return false
	}
	switch override {
	case "NO_CONSTRAINT", "us", "us_cr":
		return true
	default:
		return false
	}
}

// validateAccountReadAccount discriminates strict apiKey, chatgpt and amazonBedrock account shapes,
// including bounded nullable email and the pinned plan allowlist; unsupported kinds/fields fail closed.
// validateAccountReadAccount различает строгие схемы apiKey, chatgpt и amazonBedrock, включая
// ограниченный nullable email и закреплённый список планов; неизвестные виды и поля отклоняются.
func validateAccountReadAccount(raw json.RawMessage) error {
	fields, err := decodeObject(raw, schema([]string{"type"}, "type", "email", "planType", "usesCodexManagedCredentials"))
	if err != nil {
		return err
	}
	accountType, err := decodeBoundedString(fields["type"], 64)
	if err != nil {
		return err
	}
	switch accountType {
	case "apiKey":
		if len(fields) != 1 {
			return errors.New("Codex app-server API key account is invalid")
		}
	case "chatgpt":
		email, hasEmail := fields["email"]
		planType, hasPlanType := fields["planType"]
		if len(fields) != 3 || !hasEmail || !hasPlanType || validateAccountEmail(email) != nil || !validAccountPlanType(planType) {
			return errors.New("Codex app-server ChatGPT account is invalid")
		}
	case "amazonBedrock":
		managed, hasManaged := fields["usesCodexManagedCredentials"]
		if len(fields) > 2 || hasManaged && strictDecode(managed, new(bool)) != nil {
			return errors.New("Codex app-server Amazon Bedrock account is invalid")
		}
	default:
		return errors.New("Codex app-server account type is invalid")
	}
	return nil
}

// validateAccountEmail accepts null or a bounded JSON string as opaque account metadata, not as an
// email-format, mailbox-control or ownership verification.
// validateAccountEmail принимает null либо ограниченную JSON-строку как непрозрачные метаданные,
// а не проверяет формат email, доступ к ящику или владение им.
func validateAccountEmail(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var email string
	if strictDecode(raw, &email) != nil || len(email) > 4096 {
		return errors.New("Codex app-server account email is invalid")
	}
	return nil
}

// validAccountPlanType accepts only the pinned plan strings, including the literal unknown value;
// it does not infer permissions, spend allowance or support for an arbitrary future plan.
// validAccountPlanType принимает лишь закреплённые планы, включая буквальное unknown;
// она не выводит права, лимит расходов или поддержку произвольного будущего плана.
func validAccountPlanType(raw json.RawMessage) bool {
	planType, err := decodeBoundedString(raw, 64)
	if err != nil {
		return false
	}
	switch planType {
	case "free", "go", "plus", "pro", "prolite", "promax", "team", "self_serve_business_prolite",
		"self_serve_business_usage_based", "business", "ent26", "enterprise_cbp_automation",
		"enterprise_cbp_usage_based", "enterprise", "edu", "edu_plus", "edu_pro", "unknown":
		return true
	default:
		return false
	}
}

// waitRequiredMCP polls thread-bound tools/auth status under caller cancellation and a local readiness
// budget until the exact required tools bind or a failure occurs. It does not create grants, start
// background polling or substitute a separate unprotected health probe for the working RPC path.
// waitRequiredMCP читает tools/auth status своего thread с отменой и локальным бюджетом готовности
// до привязки точных обязательных инструментов либо отказа. Она не создаёт grants, не запускает
// фоновый polling и не заменяет рабочий RPC отдельной незащищённой health-проверкой.
func (server *appServer) waitRequiredMCP(ctx context.Context, state *protocolState, requiredTools []string) error {
	readinessContext, cancel := context.WithTimeout(ctx, mcpReadinessTimeout)
	defer cancel()
	params := map[string]any{
		"threadId": state.threadID,
		"detail":   "toolsAndAuthOnly",
		"limit":    uint32(128),
	}
	for {
		raw, err := server.call(readinessContext, state, "mcpServerStatus/list", params)
		if err != nil {
			if readinessContext.Err() != nil {
				return ErrRequiredMCPUnavailable
			}
			return err
		}
		ready, err := state.bindRequiredMCPStatus(raw, requiredTools)
		if err != nil || ready {
			return err
		}
		timer := time.NewTimer(mcpReadinessPoll)
		select {
		case <-readinessContext.Done():
			timer.Stop()
			return ErrRequiredMCPUnavailable
		case <-timer.C:
		}
	}
}

// RequiredMCPToolNames delegates the expected tool-name set to the shared runtime contract without
// granting any capability merely because the name is present in that set.
// RequiredMCPToolNames получает ожидаемый набор имён из общего runtime contract, не выдавая
// capability только из-за присутствия имени в наборе.
func RequiredMCPToolNames(input model.Input) []string {
	return runtimecontract.RuntimeMCPToolNames(input)
}

// startAppServer builds the fixed CLI command with exact projected environment/working directory,
// a new process group and parent-death TERM, then delegates stdio/start ownership. Execution context
// cancellation is handled by the surrounding pipeline, not by CommandContext in this constructor.
// startAppServer формирует фиксированную CLI команду с точным окружением и cwd, новой process group
// и parent-death TERM, затем передаёт владение stdio и запуском. Отменой context управляет окружающий
// pipeline, а не CommandContext в этом конструкторе.
func startAppServer(input model.Input, mcpProxyToken string) (*appServer, error) {
	command := exec.Command("/usr/local/bin/codex", "app-server", "--strict-config", "--listen", "stdio://")
	command.Dir = workingDirectory(input)
	command.Env = appServerEnvironment(input, mcpProxyToken)
	if command.Env == nil {
		return nil, errors.New("runtime environment projection is unavailable")
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	return startAppServerCommand(command, nil)
}

// startAppServerCommand allocates caller-owned stdio through the shared constructor, starts once,
// and keeps the existing reader-gate, diagnostic budget and process wait ownership. Child ends
// close after Start while parent ends remain live until the caller's drain/stop/join path.
// startAppServerCommand выделяет собственные stdio через общий конструктор, запускает один раз
// и сохраняет прежние reader-gate, бюджет диагностики и владение Wait. Child ends закрываются
// после Start, а parent ends остаются до drain/stop/join, принадлежащих вызывающему коду.
func startAppServerCommand(command *exec.Cmd, readerGate <-chan struct{}) (*appServer, error) {
	pipes, err := serviceruntime.NewCommandPipes(command, "Codex app-server")
	if err != nil {
		return nil, err
	}
	stdinChild, stdin := pipes.StdinChild, pipes.Stdin
	stdout, stdoutChild := pipes.Stdout, pipes.StdoutChild
	stderr, stderrChild := pipes.Stderr, pipes.StderrChild
	defer stdinChild.Close()
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutChild.Close()
		_ = stderr.Close()
		_ = stderrChild.Close()
		return nil, errors.New("start Codex app-server process")
	}
	// Родитель не держит child ends: EOF приходит после закрытия descriptors
	// process group, а Cmd.Wait не владеет reader ends и не закрывает их под readers.
	_ = stdoutChild.Close()
	_ = stderrChild.Close()
	messages := make(chan streamEvent, appServerMessageQueueCapacity)
	readerStop, readerDone := make(chan struct{}), make(chan struct{})
	go func() {
		if readerGate != nil {
			<-readerGate
		}
		readAppServerMessagesUntil(stdout, messages, readerStop, readerDone)
	}()
	diagnostics := make(chan error, 1)
	go func() {
		if readerGate != nil {
			<-readerGate
		}
		written, copyErr := io.Copy(io.Discard, io.LimitReader(stderr, maximumDiagnosticSize+1))
		if copyErr != nil || written > maximumDiagnosticSize {
			diagnostics <- errors.New("Codex app-server diagnostic stream exceeded its bound")
		} else {
			diagnostics <- nil
		}
	}()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	return &appServer{
		command: command, stdin: stdin, stdout: stdout, stderr: stderr,
		messages: messages, wait: wait, diagnostics: diagnostics,
		readerStop: readerStop, readerDone: readerDone,
	}, nil
}

// appServerEnvironment constructs a fresh allowlisted environment from fixed home/path/MCP values and
// the image GOROOT default (overridden only by published configuration, never ambient inheritance),
// approved plain values, named secret projections and exact server transport. Missing secrets or
// forbidden names reject it; unrelated parent variables are not inherited and transport cannot be overridden.
// appServerEnvironment строит новое окружение из фиксированных home/path/MCP, default GOROOT образа
// (меняемого только опубликованной конфигурацией, не ambient inheritance), разрешённых обычных
// значений, именованных secret projections и точного транспорта сервера. Отсутствующий секрет или
// запрещённое имя дают отказ; чужие parent переменные не наследуются и транспорт не подменяется.
func appServerEnvironment(input model.Input, mcpProxyToken string) []string {
	transport, err := runtimeTransportEnvironment()
	if err != nil {
		return nil
	}
	environment := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + input.CodexHome,
		"CODEX_HOME=" + input.CodexHome, "KODEX_MCP_PROXY_TOKEN=" + mcpProxyToken,
		"GOROOT=/usr/local/go"}
	for _, item := range input.EnvironmentValues {
		if !runtimecontract.ValidRuntimeEnvironmentName(item.Name) {
			return nil
		}
		environment = append(environment, item.Name+"="+item.Value)
	}
	for _, item := range input.SecretProjections {
		if !runtimecontract.ValidRuntimeEnvironmentName(item.Name) {
			return nil
		}
		value, present := os.LookupEnv(item.Name)
		if !present {
			return nil
		}
		environment = append(environment, item.Name+"="+value)
	}
	for _, name := range runtimeTransportEnvironmentNames {
		environment = append(environment, name+"="+transport[name])
	}
	return environment
}

// readAppServerMessages uses the same bounded JSONL reader without a cancellation or join channel;
// the caller must drain events or provide its own reader lifecycle to avoid blocking delivery.
// readAppServerMessages использует тот же ограниченный JSONL reader без каналов отмены и join;
// вызывающий код обязан читать events или обеспечить свой lifecycle reader, чтобы доставка не зависала.
func readAppServerMessages(reader io.Reader, events chan<- streamEvent) {
	readAppServerMessagesUntil(reader, events, nil, nil)
}

// readAppServerMessagesUntil bounds frame bytes and message count, parses each frame before channel
// delivery and maps failures to private closed subtypes. Stop unblocks a full event queue but does not
// itself interrupt a blocked underlying Read; the lifecycle owner closes the stream when required.
// It closes events and optional done on return, retaining LF/CRLF/EOF boundary semantics.
// readAppServerMessagesUntil ограничивает байты кадра и число сообщений, разбирает каждый кадр до
// доставки и переводит ошибки во внутренние закрытые подтипы. Stop освобождает заполненную очередь,
// но сам не прерывает заблокированный исходный Read; при необходимости поток закрывает владелец lifecycle.
// При выходе закрываются events и optional done с сохранением границ LF/CRLF/EOF.
func readAppServerMessagesUntil(reader io.Reader, events chan<- streamEvent, stop <-chan struct{}, done chan<- struct{}) {
	if done != nil {
		defer close(done)
	}
	defer close(events)
	send := func(event streamEvent) bool {
		if stop == nil {
			events <- event
			return true
		}
		select {
		case events <- event:
			return true
		case <-stop:
			return false
		}
	}
	scanner := bufio.NewScanner(reader)
	// Два дополнительных байта нужны только для LF/CRLF; длина JSON ниже
	// проверяется отдельно, включая последний кадр без перевода строки.
	scanner.Buffer(make([]byte, 64<<10), maximumAppServerFrameBytes+2)
	count := 0
	for scanner.Scan() {
		count++
		if count > maximumJSONLMessages {
			send(streamEvent{err: &appServerStreamFailure{reason: streamFailureMessageLimit}})
			return
		}
		if len(scanner.Bytes()) > maximumAppServerFrameBytes {
			send(streamEvent{err: &appServerStreamFailure{reason: streamFailureFrameTooLarge}})
			return
		}
		if len(scanner.Bytes()) == 0 {
			send(streamEvent{err: &appServerStreamFailure{reason: streamFailureEmptyFrame}})
			return
		}
		message, err := parseWireMessage(scanner.Bytes())
		if err != nil {
			send(streamEvent{err: &appServerStreamFailure{reason: streamFailureWireInvalid}})
			return
		}
		if !send(streamEvent{message: message}) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		reason := streamFailureReadError
		if errors.Is(err, bufio.ErrTooLong) {
			reason = streamFailureFrameTooLarge
		}
		send(streamEvent{err: &appServerStreamFailure{reason: reason}})
	}
}

// call serially writes one incremented request ID, processes intervening notifications/server requests
// and accepts only the exact correlated response/error. Caller cancellation, malformed streams and
// unexpected kinds retain internal detail; no retry or concurrent multiplexing is implemented here.
// call последовательно пишет новый ID, обрабатывает промежуточные уведомления и server requests,
// принимая лишь точно связанный response/error. Отмена, повреждённый поток и неверный kind сохраняют
// внутреннюю категорию; повтор запроса и конкурентное мультиплексирование здесь не реализованы.
func (server *appServer) call(ctx context.Context, state *protocolState, method string, params any) (json.RawMessage, error) {
	server.nextID++
	id := server.nextID
	if err := server.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, callFailure("REQUEST_WRITE", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil, callFailure("CONTEXT_CANCELLED", context.Canceled)
		case event, open := <-server.messages:
			if !open {
				return nil, callFailure("STREAM_CLOSED", errors.New("Codex app-server closed its response stream"))
			}
			if event.err != nil {
				return nil, callFailure("STREAM_INVALID", event.err)
			}
			switch event.message.kind {
			case messageNotification:
				if err := state.notification(event.message.method, event.message.payload); err != nil {
					return nil, notificationFailure(event.message.method, err)
				}
			case messageRequest:
				if err := server.handleRequest(state, event.message); err != nil {
					return nil, callFailure("REQUEST_REJECTED", err)
				}
			case messageResponse, messageError:
				responseID, err := numericRequestID(event.message.id)
				if err != nil || responseID != id {
					return nil, callFailure("RESPONSE_CORRELATION", errors.New("Codex app-server response correlation failed"))
				}
				if event.message.kind == messageError {
					return nil, protocolError(method, event.message.payload)
				}
				return event.message.payload, nil
			default:
				return nil, callFailure("MESSAGE_KIND", errors.New("Codex app-server message kind is invalid"))
			}
		}
	}
}

// protocolError validates the RPC error shape, keeps numeric code and closed account/financial reasons,
// and reports only method/code rather than the upstream message or private data object.
// protocolError проверяет схему RPC ошибки, сохраняет числовой код и закрытые account/financial причины,
// сообщая только method/code вместо внешнего message или приватного data.
func protocolError(method string, raw json.RawMessage) error {
	code, err := decodeRPCError(raw)
	if err != nil {
		return errors.New("Codex app-server returned an invalid protocol error")
	}
	return &appServerCallFailure{detail: "RPC_ERROR", code: code, accountRead: closedAccountReadFailure(method, code, raw),
		financialUnavailable: financialSnapshotsUnavailable(method, raw),
		err:                  fmt.Errorf("Codex app-server returned a protocol error for %s (code %d)", method, code)}
}

// waitTerminal consumes notifications and allowed server requests until the authoritative terminal,
// servicing financial wake reads without missing a terminal consumed inside that correlated RPC.
// Stream, parse, cancellation and uncorrelated-response failures keep their typed details; it does
// not treat a final-phase message alone as turn completion.
// waitTerminal обрабатывает уведомления и разрешённые server requests до авторитетного итога,
// обслуживая financial wake без потери terminal, полученного внутри такого RPC. Stream, parse,
// отмена и несвязанный response сохраняют категории; одно сообщение финальной фазы не завершает ход.
func (server *appServer) waitTerminal(ctx context.Context, state *protocolState) error {
	for state.terminals == 0 {
		if state.financialWake {
			if err := server.observeFinancial(ctx, state); err != nil {
				return err
			}
			// Коррелированный RPC также обрабатывает notifications и может
			// потребить terminal. Повторно проверяем условие до нового ожидания.
			if state.terminals != 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return callFailure("CONTEXT_CANCELLED", context.Canceled)
		case event, open := <-server.messages:
			if !open {
				return callFailure("STREAM_CLOSED", errors.New("Codex app-server closed before a terminal notification"))
			}
			if event.err != nil {
				return callFailure("STREAM_INVALID", event.err)
			}
			switch event.message.kind {
			case messageNotification:
				if err := state.notification(event.message.method, event.message.payload); err != nil {
					return notificationFailure(event.message.method, err)
				}
			case messageRequest:
				if err := server.handleRequest(state, event.message); err != nil {
					return callFailure("REQUEST_REJECTED", err)
				}
			default:
				return callFailure("RESPONSE_CORRELATION", errors.New("Codex app-server emitted an uncorrelated response"))
			}
		}
	}
	return nil
}

// notifyInitialized emits the post-initialize notification through the same bounded writer.
// notifyInitialized передаёт уведомление после initialize через тот же ограниченный writer.
func (server *appServer) notifyInitialized() error {
	return server.write(map[string]any{"method": "initialized"})
}

// rejectRequest rejects unregistered methods without granting authority, and sends a best-effort
// fixed error for known but unauthorized requests. The returned sentinel preserves denial even
// if writing that rejection failed; arbitrary unknown method names are not echoed in its message.
// rejectRequest отклоняет незарегистрированные методы без выдачи полномочий и по возможности пишет
// фиксированный отказ известному, но неразрешённому запросу. Маркер отказа сохраняется даже при
// ошибке записи; произвольное неизвестное имя метода не повторяется в сообщении.
func (server *appServer) rejectRequest(message wireMessage) error {
	if _, allowed := serverRequestMethods[message.method]; !allowed {
		return fmt.Errorf("%w: request method is not allowed", ErrAuthorityRequestUnsupported)
	}
	_ = server.write(map[string]any{"id": message.id, "error": map[string]any{
		"code": int64(-32000), "message": "Server requests are not authorized in agent-runner"}})
	return fmt.Errorf("%w: %s", ErrAuthorityRequestUnsupported, message.method)
}

// handleRequest permits only the narrowly checked empty Kodex MCP tool-approval form; every other
// request uses rejection. This protocol acknowledgement does not create an owner decision or bypass
// the upstream platform tool's own authorization/HumanGate.
// handleRequest допускает лишь узко проверенную пустую форму подтверждения инструмента Kodex MCP;
// остальное отклоняется. Это подтверждение протокола не создаёт owner decision и не обходит
// авторизацию или HumanGate самого платформенного инструмента.
func (server *appServer) handleRequest(state *protocolState, message wireMessage) error {
	if message.method != "mcpServer/elicitation/request" {
		return server.rejectRequest(message)
	}
	response, err := trustedMCPToolApproval(state, message.payload)
	if err != nil {
		return server.rejectRequest(message)
	}
	return server.write(map[string]any{"id": message.id, "result": response})
}

// trustedMCPToolApproval checks thread/optional turn, exact server/form mode, bounded message, empty
// object properties and the MCP approval-kind marker before returning empty acceptance content.
// It uses ordinary JSON decoding, not a complete unknown-field schema validator, and is neither a
// user-input form handler nor a source of tool permissions or human approval records.
// trustedMCPToolApproval сверяет thread/optional turn, точные server/form, ограниченный message,
// пустые object properties и маркер MCP approval-kind перед пустым accept. Здесь обычный JSON decode,
// не полная проверка лишних полей; это не обработчик пользовательского ввода, источник прав
// инструмента или записей человеческого подтверждения.
func trustedMCPToolApproval(state *protocolState, raw json.RawMessage) (map[string]any, error) {
	var request struct {
		ThreadID        string  `json:"threadId"`
		TurnID          *string `json:"turnId"`
		ServerName      string  `json:"serverName"`
		Mode            string  `json:"mode"`
		Message         string  `json:"message"`
		RequestedSchema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"requestedSchema"`
		Metadata map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("decode MCP tool approval request")
	}
	if request.ThreadID != state.threadID || request.ServerName != "kodex" || request.Mode != "form" ||
		request.Message == "" || len(request.Message) > 4096 || request.RequestedSchema.Type != "object" ||
		request.RequestedSchema.Properties == nil || len(request.RequestedSchema.Properties) != 0 {
		return nil, errors.New("MCP tool approval request is outside runtime authority")
	}
	if request.TurnID != nil && *request.TurnID != state.turnID {
		return nil, errors.New("MCP tool approval turn does not match runtime authority")
	}
	var approvalKind string
	if err := json.Unmarshal(request.Metadata["codex_approval_kind"], &approvalKind); err != nil || approvalKind != "mcp_tool_call" {
		return nil, errors.New("MCP elicitation is not a tool approval")
	}
	return map[string]any{"action": "accept", "content": map[string]any{}}, nil
}

// write marshals once, checks encoded request length and attempts one newline-terminated Write.
// It neither bounds Marshal allocation nor retries or verifies a short nil-error byte count;
// callers depend on the owned pipe writer and must not infer transactional delivery from success.
// write один раз кодирует запрос, проверяет размер и делает один Write с newline. Она не ограничивает
// память Marshal, не повторяет запись и не проверяет короткий byte count при nil error;
// вызывающий код полагается на owned pipe writer и не получает транзакционную гарантию доставки.
func (server *appServer) write(message any) error {
	raw, err := json.Marshal(message)
	if err != nil || len(raw) == 0 || len(raw) > maximumRequestBytes {
		return errors.New("encode Codex app-server request")
	}
	raw = append(raw, '\n')
	if _, err := server.stdin.Write(raw); err != nil {
		return errors.New("write Codex app-server request")
	}
	return nil
}

// stop closes stdin and processes late notifications/requests while awaiting process exit and stderr.
// It drains both readers before closing parent streams, retaining exit and diagnostic failures.
// Grace timeout, protocol or close failure escalates through terminate with additional bounded stages,
// so processGrace is not a single deadline covering every cleanup step.
// stop закрывает stdin и обрабатывает поздние notifications/requests при ожидании exit и stderr,
// затем ждёт readers до закрытия parent streams, сохраняя exit и diagnostic отказы. Timeout,
// protocol или close ошибка вызывают terminate с дополнительными бюджетами; processGrace не является
// единым сроком всех этапов очистки.
func (server *appServer) stop(state *protocolState) error {
	if err := server.stdin.Close(); err != nil {
		return server.terminate(errors.New("close Codex app-server request stream"))
	}
	timer := time.NewTimer(processGrace)
	defer timer.Stop()
	messages := server.messages
	diagnostics := server.diagnostics
	for {
		if server.waited && messages == nil && server.diagnosticRead {
			if !server.drainReaders(terminationGrace) {
				return server.terminate(errAppServerStreamDeadline)
			}
			server.closeStreams()
			if server.waitErr != nil {
				return errors.Join(errAppServerExited, server.diagnosticErr)
			}
			return server.diagnosticErr
		}
		select {
		case event, open := <-messages:
			if !open {
				messages = nil
				continue
			}
			if event.err != nil {
				return server.terminate(event.err)
			}
			if event.message.kind == messageNotification {
				if err := state.notification(event.message.method, event.message.payload); err != nil {
					return server.terminate(err)
				}
			} else if event.message.kind == messageRequest {
				if err := server.handleRequest(state, event.message); err != nil {
					return server.terminate(err)
				}
			} else {
				return server.terminate(errors.New("Codex app-server emitted a late response"))
			}
		case waitErr := <-server.wait:
			server.waited, server.waitErr = true, waitErr
		case diagnosticErr := <-diagnostics:
			server.diagnosticRead, server.diagnosticErr = true, diagnosticErr
			diagnostics = nil
		case <-timer.C:
			return server.terminate(errors.New("Codex app-server shutdown deadline exceeded"))
		}
	}
}

// abort attempts turn/interrupt and terminal consumption only for a known unfinished thread/turn,
// using an independent bounded context after caller cancellation. It always delegates final cleanup
// to terminate while preserving the original cause instead of replacing it with interrupt failure.
// abort пытается выполнить turn/interrupt и получить terminal лишь для известного незавершённого
// thread/turn с независимым bounded context после отмены. Итоговая очистка всегда передаётся
// terminate с сохранением исходной причины вместо её замены ошибкой interrupt.
func (server *appServer) abort(ctx context.Context, state *protocolState, cause error) error {
	if state.threadID != "" && state.turnID != "" && state.terminals == 0 {
		interruptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), processGrace)
		defer cancel()
		if _, err := server.call(interruptContext, state, "turn/interrupt", map[string]string{
			"threadId": state.threadID, "turnId": state.turnID}); err != nil {
			return server.terminate(cause)
		}
		if state.terminals == 0 {
			if err := server.waitTerminal(interruptContext, state); err != nil {
				return server.terminate(cause)
			}
		}
	}
	return server.terminate(cause)
}

// terminate stops queued reader delivery once, closes stdin, signals an unjoined leader's group and
// escalates when process wait or reader drain exhausts their individual budgets. It may kill a
// pipe-holding descendant after leader exit, closes streams once and joins cause/exit/diagnostic
// failures. A bounded return is not proof every process is gone; captureReady checks that separately.
// terminate однократно останавливает доставку reader, закрывает stdin, сигнализирует группе ещё
// не завершённого leader и усиливает остановку при исчерпании отдельных wait/drain бюджетов.
// После exit leader она может убить потомка, удерживающего pipe, закрывает streams один раз и
// объединяет cause/exit/diagnostic ошибки. Bounded return не доказывает отсутствие всех процессов:
// для захвата это отдельно проверяет captureReady.
func (server *appServer) terminate(cause error) error {
	server.stopOnce.Do(func() { close(server.readerStop) })
	_ = server.stdin.Close()
	if !server.waited && server.command.Process != nil {
		_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGTERM)
	}
	if !server.waitProcess(terminationGrace) {
		cause = errors.Join(cause, errors.New("Codex app-server termination deadline exceeded"))
		if server.command.Process != nil {
			_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
		}
		if !server.waitProcess(terminationGrace) {
			cause = errors.Join(cause, errors.New("Codex app-server process join deadline exceeded"))
		}
	}
	// Потомок может удерживать descriptors после выхода лидера. Сначала даём
	// readers отдельный drain budget, затем закрываем всю process group и pipes.
	if !server.drainReaders(terminationGrace) {
		cause = errors.Join(cause, errAppServerStreamDeadline)
		if server.command.Process != nil {
			_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
		}
		server.closeStreams()
		if !server.drainReaders(terminationGrace) {
			cause = errors.Join(cause, errors.New("Codex app-server reader join deadline exceeded"))
		}
	}
	server.closeStreams()
	if server.waitErr != nil {
		cause = errors.Join(cause, errAppServerExited)
	}
	return errors.Join(cause, server.diagnosticErr)
}

// waitProcess observes the single owned Wait result at most once, caching exit error; timeout returns
// false without marking the process waited or treating a failed exit as missing completion.
// waitProcess максимум один раз получает принадлежащий Wait результат и сохраняет exit error;
// timeout даёт false без отметки waited, а неуспешный exit всё равно считается наблюдённым завершением.
func (server *appServer) waitProcess(timeout time.Duration) bool {
	if server.waited {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case server.waitErr = <-server.wait:
		server.waited = true
		return true
	case <-timer.C:
		return false
	}
}

// readDiagnostic receives and caches the diagnostic result within its own timer if not already read.
// A timeout leaves diagnosticRead false and does not close the stream or claim the reader joined.
// readDiagnostic получает и сохраняет diagnostic result в отдельном таймере, если он ещё не прочитан.
// Timeout оставляет diagnosticRead=false, не закрывая поток и не объявляя reader завершённым.
func (server *appServer) readDiagnostic(timeout time.Duration) {
	if server.diagnosticRead {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case server.diagnosticErr = <-server.diagnostics:
		server.diagnosticRead = true
	case <-timer.C:
	}
}

// drainReaders waits for both message-reader completion and stderr result under one supplied timer,
// remembering already joined sides and preserving diagnostic failure. It does not close blocked I/O;
// false leaves escalation/stream closure to the lifecycle owner.
// drainReaders ждёт завершения message reader и результата stderr в одном переданном бюджете,
// помнит уже завершённые стороны и сохраняет diagnostic failure. Заблокированный I/O она не закрывает:
// false передаёт эскалацию и закрытие потоков владельцу lifecycle.
func (server *appServer) drainReaders(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	readerDone, diagnostics := server.readerDone, server.diagnostics
	if server.readerRead {
		readerDone = nil
	}
	if server.diagnosticRead {
		diagnostics = nil
	}
	for !server.readerRead || !server.diagnosticRead {
		select {
		case <-readerDone:
			server.readerRead = true
			readerDone = nil
		case server.diagnosticErr = <-diagnostics:
			server.diagnosticRead = true
			diagnostics = nil
		case <-timer.C:
			return false
		}
	}
	return true
}

// closeStreams best-effort closes parent stdout/stderr exactly once when nonnil; it does not close
// stdin, signal processes or itself wait for readers.
// closeStreams по возможности закрывает ненулевые parent stdout/stderr ровно один раз;
// stdin, сигналы процессам и ожидание readers сюда не входят.
func (server *appServer) closeStreams() {
	server.streamsOnce.Do(func() {
		if server.stdout != nil {
			_ = server.stdout.Close()
		}
		if server.stderr != nil {
			_ = server.stderr.Close()
		}
	})
}

// verifyAccountPin delegates exact protected snapshot verification and discards only the optional
// financial identity, not errors from pin or file checks.
// verifyAccountPin вызывает точную проверку защищённого snapshot, отбрасывая лишь необязательную
// финансовую идентичность, но не ошибки pin или файла.
func verifyAccountPin(input model.Input) error {
	_, err := readPinnedFinancialIdentity(input)
	return err
}

// readPinnedFinancialIdentity reads a bounded single-link auth snapshot, compares its exact private
// content pin and observed file identity/size/mtime, rejects duplicate keys, then selects optional
// managed finance identity. Raw credential bytes are cleared on return and never added to diagnostics.
// readPinnedFinancialIdentity ограниченно читает single-link auth snapshot, сверяет точный приватный
// digest, inode/размер/mtime, отвергает повторные ключи и выбирает optional managed finance identity.
// Исходные байты реквизитов очищаются при выходе и не добавляются в диагностику.
func readPinnedFinancialIdentity(input model.Input) (string, error) {
	file, info, err := openProtectedFile(input.WorkspaceRoot, filepath.Join(input.CodexHome, "auth.json"))
	if err != nil {
		return "", errors.New("Codex authentication snapshot is unavailable")
	}
	defer file.Close()
	if info.Size() <= 0 || info.Size() > 1<<20 {
		return "", errors.New("Codex authentication snapshot metadata is invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	defer clear(raw)
	digest := sha256.Sum256(raw)
	expected, expectedErr := pinnedProviderDigest(input)
	after, statErr := file.Stat()
	if err != nil || expectedErr != nil || hex.EncodeToString(digest[:]) != expected || int64(len(raw)) != info.Size() || statErr != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || rejectDuplicateJSONKeys(raw) != nil {
		return "", errors.New("Codex authentication snapshot does not match the pinned provider account")
	}
	return managedFinancialIdentity(raw)
}

// managedFinancialIdentity selects a nullable bounded account ID only for ordinary chatgpt auth,
// not API keys or externally supplied auth tokens. The caller must first verify the immutable byte
// pin; this selector does not authenticate a process or establish remote account ownership.
// managedFinancialIdentity выбирает nullable ограниченный account ID лишь для обычного chatgpt auth,
// не API key и не внешних auth tokens. Вызывающий код предварительно сверяет immutable byte pin;
// сам selector не авторизует процесс и не устанавливает владение внешним аккаунтом.
func managedFinancialIdentity(raw []byte) (string, error) {
	var identity struct {
		AuthMode string `json:"auth_mode"`
		Tokens   *struct {
			AccountID *string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &identity) != nil {
		return "", errors.New("Codex authentication snapshot is invalid")
	}
	if identity.AuthMode != "chatgpt" {
		return "", nil
	}
	if identity.Tokens == nil || identity.Tokens.AccountID == nil {
		return "", nil
	}
	account := *identity.Tokens.AccountID
	if account == "" || len(account) > 512 {
		return "", errors.New("Codex authentication identity is invalid")
	}
	return account, nil
}

// verifyRestoreArchive is explicitly a no-op: continuation uses the retained session PVC and
// server-owned context, with no caller-provided archive locator to restore. It provides no archive
// verification or copying guarantee merely by returning nil.
// verifyRestoreArchive явно ничего не делает: continuation использует сохранённый PVC и серверный
// context без переданного вызывающим archive locator. Один возврат nil не даёт гарантии проверки
// или копирования архива.
func verifyRestoreArchive(input model.Input) error {
	// Session continuation uses the retained session PVC and server-owned
	// context. A caller-provided archive locator is intentionally absent.
	return nil
}

// captureRollout validates the canonical dated path under CodexHome, opens a protected single-link
// regular source owned by the current UID, hashes it and attempts group29000/mode0640 publication.
// Any hash/chown/chmod/close failure withholds metadata; preceding permission changes are not rolled
// back and the result is not a sealed execution proof until the separate stopped-source capture.
// captureRollout проверяет канонический dated path внутри CodexHome, открывает защищённый single-link
// regular источник текущего UID, хеширует и пытается назначить group29000/mode0640. При ошибке
// hash/chown/chmod/close метаданные не выдаются; прежние изменения прав не откатываются, а закрытое
// доказательство исполнения создаёт лишь отдельный stopped-source capture.
func captureRollout(input model.Input, returnedPath string) (string, string, string, int64, error) {
	if !filepath.IsAbs(returnedPath) || filepath.Clean(returnedPath) != returnedPath ||
		!strings.HasPrefix(returnedPath, input.CodexHome+string(os.PathSeparator)) {
		return "", "", "", 0, errors.New("Codex app-server returned an unsafe rollout path")
	}
	relativePath, err := filepath.Rel(input.WorkspaceRoot, returnedPath)
	if err != nil {
		return "", "", "", 0, errors.New("resolve Codex rollout path")
	}
	relativePath = filepath.ToSlash(relativePath)
	if !validRolloutRelativePath(relativePath) {
		return "", "", "", 0, errors.New("Codex app-server rollout identity changed")
	}
	file, info, err := openProtectedFile(input.WorkspaceRoot, returnedPath)
	if err != nil {
		return "", "", "", 0, errors.New("open Codex app-server rollout")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		file.Close()
		return "", "", "", 0, errors.New("verify Codex app-server rollout")
	}
	digest, hashErr := digestArchive(file, info)
	groupErr := file.Chown(-1, 29000)
	modeErr := file.Chmod(0o640)
	closeErr := file.Close()
	if hashErr != nil || groupErr != nil || modeErr != nil || closeErr != nil {
		return "", "", "", 0, errors.New("verify Codex app-server rollout")
	}
	return returnedPath, relativePath, digest, info.Size(), nil
}

// validRolloutRelativePath requires a canonical slash path matching the retained dated rollout pattern
// and rejects every double-dot substring; it does not open a file or infer a session from its name.
// validRolloutRelativePath требует канонический slash path закреплённого dated rollout и запрещает
// любую подстроку с двумя точками; она не открывает файл и не выводит сессию из одного имени.
func validRolloutRelativePath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	return clean == value && rolloutPathPattern.MatchString(value) && !strings.Contains(value, "..")
}

// openProtectedFile walks at most32 components from a caller-trusted root with directory-relative
// no-follow opens, requiring a regular single-link leaf. It does not verify owner/mode/size, prohibit
// mount crossings or validate ancestors preceding the opened root; callers apply their own exact checks
// and must not log returned OS errors as if they were automatically redacted.
// openProtectedFile проходит максимум32 компонента от доверенного вызывающим root через relative
// no-follow открытия, требуя regular single-link leaf. Она не проверяет owner/mode/size, mount
// crossings или ancestors до root; вызывающий код добавляет свои точные проверки и не считает
// возвращённые ошибки ОС автоматически безопасными для лога.
func openProtectedFile(root, path string) (*os.File, os.FileInfo, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, nil, errors.New("path is outside the workspace")
	}
	components := strings.Split(filepath.Clean(relative), string(os.PathSeparator))
	if len(components) == 0 || len(components) > 32 {
		return nil, nil, errors.New("path depth is invalid")
	}
	directory, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			unix.Close(directory)
			return nil, nil, errors.New("path component is invalid")
		}
		next, openErr := unix.Openat(directory, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(directory)
		if openErr != nil {
			return nil, nil, openErr
		}
		directory = next
	}
	fd, err := unix.Openat(directory, components[len(components)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	unix.Close(directory)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("file is not regular")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		file.Close()
		return nil, nil, errors.New("file link count is invalid")
	}
	return file, info, nil
}

// digestArchive hashes from the current file offset within64 MiB plus one detection byte, requiring
// exact original size and stable inode/size/mtime/ctime after reading. It neither rewinds the file nor
// validates ownership, permissions or the archive's semantic records.
// digestArchive хеширует с текущего смещения в пределах64 MiB с дополнительным detection byte,
// требуя точного исходного размера и стабильных inode/size/mtime/ctime после чтения. Перемотки,
// проверки owner, permissions или смысловых записей архива здесь нет.
func digestArchive(file *os.File, info os.FileInfo) (string, error) {
	if info.Size() <= 0 || info.Size() > maximumArchiveBytes {
		return "", errors.New("Codex rollout size is invalid")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maximumArchiveBytes+1))
	after, statErr := file.Stat()
	beforeStat, beforeOK := info.Sys().(*syscall.Stat_t)
	var afterStat *syscall.Stat_t
	if statErr == nil {
		afterStat, _ = after.Sys().(*syscall.Stat_t)
	}
	if err != nil || written != info.Size() || written > maximumArchiveBytes || statErr != nil || !beforeOK || afterStat == nil ||
		!os.SameFile(info, after) || after.Size() != info.Size() || beforeStat.Mtim != afterStat.Mtim || beforeStat.Ctim != afterStat.Ctim {
		return "", errors.New("Codex rollout content is invalid")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// ReadCredential reads then trims a trusted-path credential, accepting nonempty content up to16 KiB.
// The size check follows ReadFile rather than bounding allocation, and this helper does not verify
// file mode, inode confinement or secret ownership; those are caller/deployment responsibilities.
// ReadCredential читает и обрезает пробелы реквизитов по доверенному пути, принимая непустое до16 KiB.
// Размер проверяется после ReadFile, не ограничивая выделение памяти; mode, inode confinement
// и владение секретом обеспечивает вызывающий код или развёртывание.
func ReadCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return "", errors.New("read runtime credential")
	}
	value := string(bytes.TrimSpace(raw))
	if value == "" {
		return "", errors.New("runtime credential is empty")
	}
	return value, nil
}

// suppressedNotificationMethods requests suppression of raw/delta/reasoning and optional UI events
// at initialize while leaving authoritative token-usage observations enabled. It does not authorize
// unregistered notifications or replace the parser's closed method/payload validation.
// suppressedNotificationMethods просит отключить raw/delta/reasoning и optional UI события при initialize,
// оставляя включёнными авторитетные token-usage наблюдения. Он не разрешает неизвестные уведомления
// и не заменяет закрытую проверку методов и payload в parser.
var suppressedNotificationMethods = []string{
	"thread/status/changed", "skills/changed", "thread/name/updated", "thread/goal/updated", "thread/goal/cleared",
	"thread/settings/updated", "hook/started", "hook/completed", "turn/diff/updated",
	"turn/plan/updated", "item/autoApprovalReview/started", "item/autoApprovalReview/completed", "item/agentMessage/delta",
	"item/plan/delta", "command/exec/outputDelta", "process/outputDelta", "process/exited",
	"item/commandExecution/outputDelta", "item/commandExecution/terminalInteraction", "item/fileChange/outputDelta",
	"item/fileChange/patchUpdated", "serverRequest/resolved", "item/mcpToolCall/progress",
	"rawResponseItem/completed",
	"app/list/updated", "remoteControl/status/changed", "externalAgentConfig/import/progress",
	"externalAgentConfig/import/completed", "fs/changed", "item/reasoning/summaryTextDelta",
	"item/reasoning/summaryPartAdded", "item/reasoning/textDelta", "thread/compacted", "model/rerouted",
	"model/verification", "turn/moderationMetadata", "model/safetyBuffering/updated", "fuzzyFileSearch/sessionUpdated",
	"fuzzyFileSearch/sessionCompleted", "thread/realtime/started", "thread/realtime/itemAdded",
	"thread/realtime/transcript/delta", "thread/realtime/transcript/done", "thread/realtime/outputAudio/delta",
	"thread/realtime/sdp", "thread/realtime/error", "thread/realtime/closed",
}
