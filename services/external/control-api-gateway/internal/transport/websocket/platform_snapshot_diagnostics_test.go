package websockettransport

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPlatformSnapshotDiagnosticClosedFields(t *testing.T) {
	for _, tc := range []struct {
		kind, wantKind string
		code           codes.Code
		wantCode       string
	}{
		{"RUN", "RUN", codes.Unavailable, "Unavailable"},
		{"PRIVATE_SENTINEL", "UNKNOWN", codes.Code(999), "Unknown"},
	} {
		t.Run(tc.wantKind, func(t *testing.T) {
			ctx := t.Context()
			rows := snapshotDiagnosticLogs(t, func() {
				observePlatformSnapshotReadFailure(ctx, time.Now(), -1, tc.kind, status.Error(tc.code, "PRIVATE_SENTINEL"))
			})
			if len(rows) != 1 || len(rows[0]) != 11 {
				t.Fatal("platform diagnostic changed its exact field set or count")
			}
			row := rows[0]
			if row["msg"] != platformSnapshotReadFailure || row["kind"] != tc.wantKind ||
				row["stage"] != platformSnapshotDiagnosticStage || row["grpc_code"] != tc.wantCode ||
				row["error_class"] != "dependency" || row["parent_state"] != "ACTIVE" ||
				row["parent_budget_start_ms"] != float64(-1) || row["parent_budget_remaining_ms"] != float64(-1) {
				t.Fatal("platform diagnostic lost its closed code or parent context state")
			}
		})
	}
}

func TestPlatformSnapshotDiagnosticExpiredParentAndSuccess(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	rows := snapshotDiagnosticLogs(t, func() {
		observePlatformSnapshotReadFailure(ctx, time.Now(), 0, "RUN", status.FromContextError(ctx.Err()).Err())
		observePlatformSnapshotReadFailure(ctx, time.Now(), 0, "RUN", nil)
	})
	if len(rows) != 1 || rows[0]["grpc_code"] != "DeadlineExceeded" ||
		rows[0]["parent_state"] != "DEADLINE_EXCEEDED" || rows[0]["parent_budget_remaining_ms"] != float64(0) {
		t.Fatal("platform diagnostic lost parent expiry or logged successful read")
	}
}
