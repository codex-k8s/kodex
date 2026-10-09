package websockettransport

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	platformSnapshotDiagnosticKindKey = "kind"
	platformSnapshotDiagnosticStage   = "PLATFORM_SNAPSHOT_READ"
	platformSnapshotDiagnosticUnknown = "UNKNOWN"
)

// Единая запись отказа содержит только закрытые диагностические значения.
func observePlatformSnapshotReadFailure(ctx context.Context, started time.Time, parentBudget int64, kind string, err error) {
	if err == nil {
		return
	}
	if !slices.Contains(platformBootstrapKinds, kind) {
		kind = platformSnapshotDiagnosticUnknown
	}
	code := status.Code(err)
	if code < codes.OK || code > codes.Unauthenticated {
		code = codes.Unknown
	}
	slog.ErrorContext(ctx, platformSnapshotReadFailure,
		platformSnapshotDiagnosticKindKey, kind,
		runSnapshotDiagnosticErrorClassKey, "dependency",
		runSnapshotDiagnosticStageKey, platformSnapshotDiagnosticStage,
		runSnapshotDiagnosticGRPCCodeKey, code.String(),
		runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds(),
		runSnapshotDiagnosticParentBudgetKey, parentBudget,
		runSnapshotDiagnosticParentRemainingKey, runSnapshotParentBudget(ctx, time.Now()),
		runSnapshotDiagnosticParentStateKey, runSnapshotContextState(ctx))
}
