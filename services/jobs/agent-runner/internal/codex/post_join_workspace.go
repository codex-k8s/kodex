package codex

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// normalizeCompletedProviderWorkspace runs metadata cleanup only for completed SUCCEEDED/FAILED
// outcomes under an independent bounded context. The caller must already have stopped/joined the
// provider and readers. Cleanup failure can downgrade success but never overwrite the original failure,
// usage or source pins; an earlier outbox denial is not a reason to skip this cleanup.
// normalizeCompletedProviderWorkspace выполняет metadata cleanup лишь для завершённых SUCCEEDED/FAILED
// с независимым ограниченным context. Вызывающий код обязан предварительно остановить и дождаться
// провайдера и readers. Ошибка cleanup понижает успех, но не переписывает прежний отказ, usage
// или source pins; прежний отказ outbox не является основанием пропускать очистку.
func normalizeCompletedProviderWorkspace(ctx context.Context, input model.Input, result Result, normalize func(context.Context, model.Input) error) Result {
	if result.Outcome != "SUCCEEDED" && result.Outcome != "FAILED" {
		return result
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workspaceAccessBudget)
	defer cancel()
	if err := normalize(cleanupCtx, input); err != nil {
		logProviderFilesystemDenial(providerWorkspaceStage, err)
		if result.Outcome == "SUCCEEDED" {
			result.Outcome, result.FailureCode, result.FinalMessage = "FAILED", "RUNTIME_ARTIFACT_INVALID", "i18n:RUNTIME_ARTIFACT_INVALID"
		}
	}
	return result
}

const (
	// providerFilesystemDenialMessage is the fixed log text for post-join guard failures without path/body data.
	// providerFilesystemDenialMessage задаёт фиксированный текст отказов post-join guard без путей и содержимого.
	providerFilesystemDenialMessage = "Provider post-join filesystem denied"
	// providerFilesystemStageKey names the bounded stage log attribute, not an arbitrary filesystem location.
	// providerFilesystemStageKey задаёт ключ ограниченного этапа в логе, а не произвольного расположения файла.
	providerFilesystemStageKey = "stage"
	// providerFilesystemReasonKey names the closed reason attribute instead of including a raw error chain.
	// providerFilesystemReasonKey задаёт ключ закрытой причины вместо включения исходной цепочки ошибок.
	providerFilesystemReasonKey = "reason"
	// providerOutboxStage selects outbox-publication diagnostics and permits reasons only with its sentinel.
	// providerOutboxStage выбирает диагностику публикации outbox и допускает причины лишь с её маркером.
	providerOutboxStage = "OUTBOX_PUBLICATION"
	// providerWorkspaceStage selects workspace-normalization diagnostics with their own required sentinel.
	// providerWorkspaceStage выбирает диагностику нормализации workspace с её обязательным маркером.
	providerWorkspaceStage = "WORKSPACE_NORMALIZATION"
)

// providerFilesystemReason is the internal guard-assigned reason set; String maps unsupported values
// to UNKNOWN rather than treating integer values as validated diagnostics.
// providerFilesystemReason задаёт внутренние причины, назначаемые guard; String переводит неизвестные
// значения в UNKNOWN, не считая любое целое число проверенной диагностикой.
type providerFilesystemReason uint8

const (
	// filesystemUnknown is the zero-value fallback when no matching trusted guard reason can be recovered.
	// filesystemUnknown является нулевым запасным значением при отсутствии причины подходящего доверенного guard.
	filesystemUnknown providerFilesystemReason = iota
	// filesystemContext records cancellation/deadline observed by a filesystem guard without extending it.
	// filesystemContext отмечает отмену или срок, замеченные файловым guard, без их продления.
	filesystemContext
	// filesystemPolicy marks rejected workspace/root policy before traversal can be considered admissible.
	// filesystemPolicy отмечает отклонённую политику workspace или корня до признания обхода допустимым.
	filesystemPolicy
	// filesystemLock identifies unavailable cooperative workspace locking, not permission to bypass the lock.
	// filesystemLock обозначает недоступную согласующую блокировку workspace, не разрешая её обойти.
	filesystemLock
	// filesystemDirectory reports that the required directory could not be opened without exposing its path.
	// filesystemDirectory сообщает о невозможности открыть обязательный каталог без раскрытия его пути.
	filesystemDirectory
	// filesystemDirectoryIdentity marks an unexpected root/directory owner, group, type or access mode.
	// filesystemDirectoryIdentity отмечает неожиданные владельца, группу, тип или права корня либо каталога.
	filesystemDirectoryIdentity
	// filesystemEntryCount indicates the applicable entry-count budget was exceeded, not a truncatable result.
	// filesystemEntryCount означает превышение применимого лимита записей, а не допустимый усечённый результат.
	filesystemEntryCount
	// filesystemEntryName records a guard-rejected entry name without putting the untrusted name in logs.
	// filesystemEntryName фиксирует отклонённое guard имя записи без помещения недоверенного имени в лог.
	filesystemEntryName
	// filesystemEntryOpen identifies an entry-open rejection after traversal selected that entry.
	// filesystemEntryOpen обозначает отказ открытия записи, выбранной при обходе.
	filesystemEntryOpen
	// filesystemFileType reports a disallowed inode type such as a symlink or special file.
	// filesystemFileType сообщает о запрещённом типе inode, например symlink или специальном файле.
	filesystemFileType
	// filesystemOwner identifies an unexpected effective writer or entry owner, never a caller-chosen UID grant.
	// filesystemOwner обозначает неожиданного текущего writer или владельца записи, но не выдачу UID по запросу.
	filesystemOwner
	// filesystemLinkCount rejects unexpected inode link cardinality rather than accepting hardlinked content.
	// filesystemLinkCount обозначает недопустимое число ссылок inode вместо принятия hardlinked содержимого.
	filesystemLinkCount
	// filesystemFileSize records invalid size metadata under the specific guard's file-size rules.
	// filesystemFileSize фиксирует неверные метаданные размера по правилам конкретного файлового guard.
	filesystemFileSize
	// filesystemByteQuota marks an exceeded writable-byte allowance and does not authorize partial publication.
	// filesystemByteQuota отмечает превышение разрешённых записываемых байтов и не допускает частичную публикацию.
	filesystemByteQuota
	// filesystemMount reports an unverified or crossed mount identity, including unavailable required metadata.
	// filesystemMount сообщает о непроверенной или пересечённой границе mount, включая отсутствие обязательных метаданных.
	filesystemMount
	// filesystemPermissions identifies failure to apply the exact intended directory/file metadata changes.
	// filesystemPermissions обозначает отказ применения точно заданных изменений метаданных каталога или файла.
	filesystemPermissions
	// filesystemMetadataChanged records a failed identity/stability comparison rather than following a replaced inode.
	// filesystemMetadataChanged фиксирует несовпадение идентичности или стабильности вместо следования подменённому inode.
	filesystemMetadataChanged
	// filesystemIO is the closed fallback for an underlying filesystem operation failure, not its raw cause text.
	// filesystemIO является закрытой причиной ошибки файловой операции, а не её исходным текстом.
	filesystemIO
)

// String maps each registered filesystem reason to its fixed log value and every other value to UNKNOWN.
// String переводит зарегистрированную файловую причину в фиксированное значение лога, остальные — в UNKNOWN.
func (reason providerFilesystemReason) String() string {
	switch reason {
	case filesystemContext:
		return "CONTEXT_DONE"
	case filesystemPolicy:
		return "POLICY_INVALID"
	case filesystemLock:
		return "LOCK_UNAVAILABLE"
	case filesystemDirectory:
		return "DIRECTORY_UNAVAILABLE"
	case filesystemDirectoryIdentity:
		return "DIRECTORY_IDENTITY_INVALID"
	case filesystemEntryCount:
		return "ENTRY_COUNT_EXCEEDED"
	case filesystemEntryName:
		return "ENTRY_NAME_INVALID"
	case filesystemEntryOpen:
		return "ENTRY_OPEN_DENIED"
	case filesystemFileType:
		return "FILE_TYPE_INVALID"
	case filesystemOwner:
		return "OWNER_INVALID"
	case filesystemLinkCount:
		return "LINK_COUNT_INVALID"
	case filesystemFileSize:
		return "FILE_SIZE_INVALID"
	case filesystemByteQuota:
		return "BYTE_QUOTA_EXCEEDED"
	case filesystemMount:
		return "MOUNT_INVALID"
	case filesystemPermissions:
		return "PERMISSION_UPDATE_FAILED"
	case filesystemMetadataChanged:
		return "METADATA_CHANGED"
	case filesystemIO:
		return "FILESYSTEM_IO_FAILED"
	default:
		return "UNKNOWN"
	}
}

// providerFilesystemError retains the original cause for errors.Is and a reason assigned by the rejecting
// guard. The fixed Error string hides both; log projection also verifies the stage-specific sentinel.
// providerFilesystemError сохраняет исходную причину для errors.Is и категорию, назначенную rejecting guard.
// Фиксированный Error их скрывает; проекция логов дополнительно проверяет маркер соответствующего этапа.
type providerFilesystemError struct {
	cause  error
	reason providerFilesystemReason
}

// Error returns a constant guard-denial message without formatting the underlying cause or filename.
// Error возвращает постоянное сообщение отказа guard без форматирования вложенной причины или имени файла.
func (err *providerFilesystemError) Error() string { return "provider filesystem guard denied" }

// Unwrap deliberately preserves the original error chain for typed/sentinel checks; callers must not
// serialize that chain just because the outer Error text is safe. The receiver must be nonnil.
// Unwrap намеренно сохраняет исходную цепочку для typed/sentinel проверок; безопасный внешний Error
// не разрешает сериализовать эту цепочку. Получатель метода обязан быть ненулевым.
func (err *providerFilesystemError) Unwrap() error { return err.cause }

// providerFilesystemDenied wraps the supplied cause and guard reason without logging or validating
// the enum; the later closed diagnostic mapper rejects unknown or stage-mismatched reasons.
// providerFilesystemDenied оборачивает причину и категорию guard без лога или проверки enum;
// последующая закрытая проекция отвергает неизвестные и не соответствующие этапу категории.
func providerFilesystemDenied(cause error, reason providerFilesystemReason) error {
	return &providerFilesystemError{cause: cause, reason: reason}
}

// logProviderFilesystemDenial creates the standard stderr JSON logger and delegates to the safe mapper;
// it does not emit raw errors, paths or filesystem contents itself.
// logProviderFilesystemDenial создаёт стандартный stderr JSON logger и вызывает безопасную проекцию;
// сама она не выводит исходные ошибки, пути или содержимое файлов.
func logProviderFilesystemDenial(stage string, err error) {
	writeProviderFilesystemDenial(slog.New(slog.NewJSONHandler(os.Stderr, nil)), stage, err)
}

// writeProviderFilesystemDenial emits only fixed message, allowlisted stage and closed reason, ignoring
// nil logger/error interfaces. A reason is accepted only when the matching stage sentinel and nonnil
// typed wrapper are both present; unknown stages, foreign causes and arbitrary reason values stay UNKNOWN.
// writeProviderFilesystemDenial выводит лишь фиксированное сообщение, разрешённый этап и закрытую причину,
// пропуская nil logger/error интерфейсы. Причина принимается лишь при соответствующем stage-маркере
// и ненулевой typed wrapper; неизвестный этап, чужая причина и произвольное значение остаются UNKNOWN.
func writeProviderFilesystemDenial(logger *slog.Logger, stage string, err error) {
	if logger == nil || err == nil {
		return
	}
	if stage != providerOutboxStage && stage != providerWorkspaceStage {
		stage = "UNKNOWN"
	}
	reason := filesystemUnknown
	var denied *providerFilesystemError
	if (stage == providerOutboxStage && errors.Is(err, errProviderOutboxPublication) || stage == providerWorkspaceStage && errors.Is(err, errProviderWorkspaceAccess)) && errors.As(err, &denied) && denied != nil {
		reason = denied.reason
	}
	logger.Warn(providerFilesystemDenialMessage, providerFilesystemStageKey, stage, providerFilesystemReasonKey, reason.String())
}
