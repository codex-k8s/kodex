package codex

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// Вызывается только после nil execute error: production executor уже остановил
// provider и readers. Ошибка публикации не отменяет обязательный metadata cleanup.
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
	providerFilesystemDenialMessage = "Provider post-join filesystem denied"
	providerFilesystemStageKey      = "stage"
	providerFilesystemReasonKey     = "reason"
	providerOutboxStage             = "OUTBOX_PUBLICATION"
	providerWorkspaceStage          = "WORKSPACE_NORMALIZATION"
)

type providerFilesystemReason uint8

const (
	filesystemUnknown providerFilesystemReason = iota
	filesystemContext
	filesystemPolicy
	filesystemLock
	filesystemDirectory
	filesystemDirectoryIdentity
	filesystemEntryCount
	filesystemEntryName
	filesystemEntryOpen
	filesystemFileType
	filesystemOwner
	filesystemLinkCount
	filesystemFileSize
	filesystemByteQuota
	filesystemMount
	filesystemPermissions
	filesystemMetadataChanged
	filesystemIO
)

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

// Причина назначается самим rejecting guard, не строкой ошибки или именем файла.
type providerFilesystemError struct {
	cause  error
	reason providerFilesystemReason
}

func (err *providerFilesystemError) Error() string { return "provider filesystem guard denied" }
func (err *providerFilesystemError) Unwrap() error { return err.cause }

func providerFilesystemDenied(cause error, reason providerFilesystemReason) error {
	return &providerFilesystemError{cause: cause, reason: reason}
}

func logProviderFilesystemDenial(stage string, err error) {
	writeProviderFilesystemDenial(slog.New(slog.NewJSONHandler(os.Stderr, nil)), stage, err)
}

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
