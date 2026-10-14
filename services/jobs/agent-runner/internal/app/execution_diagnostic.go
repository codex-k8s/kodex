package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
)

const (
	runtimeExecutionFailureMessage    = "runtime execution failed before completion"
	runtimeExecutionCategoryKey       = "category"
	runtimeExecutionContextStateKey   = "context_state"
	runtimeExecutionActivityFailedKey = "activity_failed"
)

// На границе завершения пишутся только закрытые признаки. Исходная ошибка,
// provider/frame/input payload и context cause в logger не передаются.
func logRuntimeExecutionFailure(ctx context.Context, logger *slog.Logger, err error, activityFailed bool) {
	if logger == nil {
		return
	}
	contextState := "ACTIVE"
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		contextState = "DEADLINE_EXCEEDED"
	case errors.Is(ctx.Err(), context.Canceled):
		contextState = "CANCELED"
	case ctx.Err() != nil:
		contextState = "UNKNOWN"
	}
	logger.WarnContext(ctx, runtimeExecutionFailureMessage,
		runtimeExecutionCategoryKey, codex.BrokerResponseFailureCategory(err),
		runtimeExecutionContextStateKey, contextState,
		runtimeExecutionActivityFailedKey, activityFailed)
}
