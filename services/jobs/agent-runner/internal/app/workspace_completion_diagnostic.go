package app

import (
	"context"
	"log/slog"

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
	switch reason {
	case runtimecontract.RuntimeWorkspaceReadOnly, runtimecontract.RuntimeWorkspaceQuotaExceeded,
		runtimecontract.RuntimeWorkspacePathOutsideWorkspace, runtimecontract.RuntimeWorkspaceIOError:
	default:
		reason = runtimecontract.RuntimeWorkspaceIOError
	}
	logger.WarnContext(ctx, workspaceCompletionMessage,
		workspaceCompletionStage, stage, workspaceCompletionReason, reason)
}
