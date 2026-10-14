package websockettransport

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	runSnapshotDiagnosticMessage            = "realtime run snapshot unavailable"
	runSnapshotReadStage                    = "RUN_SNAPSHOT_READ"
	runSnapshotProjectStage                 = "RUN_SNAPSHOT_PROJECT"
	noRunSnapshotParentBudget               = -1
	runSnapshotDiagnosticStageKey           = "stage"
	runSnapshotDiagnosticGRPCCodeKey        = "grpc_code"
	runSnapshotDiagnosticElapsedKey         = "elapsed_ms"
	runSnapshotDiagnosticParentBudgetKey    = "parent_budget_start_ms"
	runSnapshotDiagnosticParentRemainingKey = "parent_budget_remaining_ms"
	runSnapshotDiagnosticParentStateKey     = "parent_state"
	runSnapshotDiagnosticReadStateKey       = "read_state"
	runSnapshotDiagnosticErrorClassKey      = "error_class"
)

// Диагностика транспортного отказа не сериализует ошибку, refs или metadata.
func observeRunSnapshotReadFailure(parent, request context.Context, started time.Time, parentBudget int64, err error) {
	if err == nil {
		return
	}
	code := status.Code(err)
	if code < codes.OK || code > codes.Unauthenticated {
		code = codes.Unknown
	}
	slog.WarnContext(parent, runSnapshotDiagnosticMessage,
		runSnapshotDiagnosticStageKey, runSnapshotReadStage, runSnapshotDiagnosticGRPCCodeKey, code.String(),
		runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds(),
		runSnapshotDiagnosticParentBudgetKey, parentBudget,
		runSnapshotDiagnosticParentRemainingKey, runSnapshotParentBudget(parent, time.Now()),
		runSnapshotDiagnosticParentStateKey, runSnapshotContextState(parent),
		runSnapshotDiagnosticReadStateKey, runSnapshotContextState(request))
}

func observeRunSnapshotProjectionFailure(started time.Time, err error) {
	if err != nil {
		slog.Warn(runSnapshotDiagnosticMessage, runSnapshotDiagnosticStageKey, runSnapshotProjectStage,
			runSnapshotDiagnosticErrorClassKey, "contract", runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds())
	}
}

func runSnapshotParentBudget(ctx context.Context, now time.Time) int64 {
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return noRunSnapshotParentBudget
	}
	return max(0, deadline.Sub(now).Milliseconds())
}

func runSnapshotContextState(ctx context.Context) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED"
	case errors.Is(ctx.Err(), context.Canceled):
		return "CANCELED"
	default:
		return "ACTIVE"
	}
}
