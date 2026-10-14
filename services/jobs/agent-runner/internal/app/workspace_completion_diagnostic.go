package app

import (
	"context"
	"errors"
	"log/slog"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

const (
	workspaceCompletionMessage = "Runtime workspace completion denied"
	workspaceCompletionCheck   = "CHECK_WORKSPACE"
	workspaceCompletionPublish = "PUBLISH_RESULT"
	workspaceCompletionStage   = "stage"
	workspaceCompletionReason  = "reason"
)

// Один отказ terminal boundary не теряет подэтап, но не раскрывает paths,
// содержимое workspace или сырую цепочку ошибок. Logger приходит из runtime.
func logWorkspaceCompletionDenial(ctx context.Context, logger *slog.Logger, stage string, err error) {
	if logger == nil {
		return
	}
	if stage != workspaceCompletionCheck && stage != workspaceCompletionPublish {
		stage = "UNKNOWN"
	}
	reason := workspacepolicy.DenialReason(err)
	var artifactDenial *completionArtifactDenial
	if errors.As(err, &artifactDenial) {
		reason = artifactDenial.reason
	}
	switch reason {
	case runtimecontract.RuntimeWorkspaceReadOnly, runtimecontract.RuntimeWorkspaceQuotaExceeded,
		runtimecontract.RuntimeWorkspacePathOutsideWorkspace, runtimecontract.RuntimeWorkspaceIOError:
	default:
		reason = runtimecontract.RuntimeWorkspaceIOError
	}
	logger.WarnContext(ctx, workspaceCompletionMessage,
		workspaceCompletionStage, stage, workspaceCompletionReason, reason)
}

// Сборка артефактов сохраняет только закрытую причину своего guard. Исходная
// ошибка не хранится: её path, тело или driver message не нужны диагностике.
type completionArtifactDenial struct{ reason string }

func (*completionArtifactDenial) Error() string { return "runtime artifact collection denied" }

func denyCompletionArtifact(err error) error {
	reason := workspacepolicy.DenialReason(err)
	switch {
	case errors.Is(err, syscall.EROFS), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		reason = runtimecontract.RuntimeWorkspaceReadOnly
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.EINVAL):
		reason = runtimecontract.RuntimeWorkspacePathOutsideWorkspace
	}
	switch reason {
	case runtimecontract.RuntimeWorkspaceReadOnly, runtimecontract.RuntimeWorkspaceQuotaExceeded,
		runtimecontract.RuntimeWorkspacePathOutsideWorkspace, runtimecontract.RuntimeWorkspaceIOError:
	default:
		reason = runtimecontract.RuntimeWorkspaceIOError
	}
	return &completionArtifactDenial{reason: reason}
}
