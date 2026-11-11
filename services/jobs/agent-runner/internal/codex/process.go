package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
