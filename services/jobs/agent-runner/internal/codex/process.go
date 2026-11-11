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
)

var (
	// ErrAuthorityRequestUnsupported preserves a typed authority denial without granting a model's request.
	// ErrAuthorityRequestUnsupported сохраняет типизированный отказ в полномочиях, не выдавая их по запросу модели.
	ErrAuthorityRequestUnsupported = errors.New("Provider authority request is unsupported")
	// ErrRequiredMCPUnavailable marks failure of a mandatory tool path rather than optional degradation.
	// ErrRequiredMCPUnavailable обозначает отказ обязательного пути инструментов, а не необязательное ухудшение.
	ErrRequiredMCPUnavailable = errors.New("Required MCP runtime is unavailable")
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
	// providerStageTerminalWait identifies notification/request processing while awaiting the authoritative terminal.
	// providerStageTerminalWait обозначает обработку уведомлений и запросов при ожидании авторитетного итога.
	providerStageTerminalWait providerExecutionStage = "TERMINAL_WAIT"
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
func (failure *providerStageError) Error() string { return "Provider execution stage failed" }

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
