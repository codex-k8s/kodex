package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func snapshotDiagnosticLogs(t *testing.T, operation func()) []map[string]any {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	operation()
	if strings.Contains(output.String(), "PRIVATE_SENTINEL") {
		t.Fatal("snapshot diagnostic exposed private input or error")
	}
	var rows []map[string]any
	decoder := json.NewDecoder(&output)
	for decoder.More() {
		var row map[string]any
		if err := decoder.Decode(&row); err != nil {
			t.Fatal("snapshot diagnostic is not structured JSON")
		}
		rows = append(rows, row)
	}
	return rows
}

func TestRunSnapshotReadDiagnosticClosedFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"dependency", status.Error(codes.Unavailable, "PRIVATE_SENTINEL"), "Unavailable"},
		{"deadline", fmt.Errorf("PRIVATE_SENTINEL: %w", status.Error(codes.DeadlineExceeded, "PRIVATE_SENTINEL")), "DeadlineExceeded"},
		{"unknown", errors.New("PRIVATE_SENTINEL"), "Unknown"},
		{"invalid-code", status.Error(codes.Code(999), "PRIVATE_SENTINEL"), "Unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			rows := snapshotDiagnosticLogs(t, func() {
				observeRunSnapshotReadFailure(ctx, ctx, time.Now(), noRunSnapshotParentBudget, tc.err)
			})
			if len(rows) != 1 || len(rows[0]) != 10 {
				t.Fatal("read diagnostic changed its exact field set or count")
			}
			row := rows[0]
			if row["msg"] != runSnapshotDiagnosticMessage || row["stage"] != runSnapshotReadStage || row["grpc_code"] != tc.code ||
				row["parent_budget_start_ms"] != float64(-1) || row["parent_budget_remaining_ms"] != float64(-1) ||
				row["parent_state"] != "ACTIVE" || row["read_state"] != "ACTIVE" {
				t.Fatal("read diagnostic lost closed code or parent context state")
			}
		})
	}
}

func TestRunSnapshotReadDiagnosticParentDeadlineAndCancel(t *testing.T) {
	for _, state := range []string{"DEADLINE_EXCEEDED", "CANCELED"} {
		t.Run(state, func(t *testing.T) {
			var parent context.Context
			if state == "DEADLINE_EXCEEDED" {
				ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()
				parent = ctx
			} else {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				parent = ctx
			}
			m, c := platformWakeRaceFixture(t, 8)
			c.beforeInvoke = func(ctx context.Context, _ any) error {
				if !errors.Is(ctx.Err(), parent.Err()) {
					t.Fatal("snapshot read escaped parent cancellation")
				}
				return status.FromContextError(ctx.Err()).Err()
			}
			rows := snapshotDiagnosticLogs(t, func() {
				_, err := m.readRunSnapshotWithin(parent, "PRIVATE_SENTINEL")
				if status.Code(err) != status.Code(status.FromContextError(parent.Err()).Err()) {
					t.Fatal("diagnostic changed returned RPC failure")
				}
			})
			if len(rows) != 1 || rows[0]["parent_state"] != state || rows[0]["read_state"] != state || len(c.frames) != 0 {
				t.Fatal("diagnostic lost exact cancellation or introduced transport effects")
			}
			if state == "DEADLINE_EXCEEDED" && rows[0]["parent_budget_remaining_ms"] != float64(0) {
				t.Fatal("expired parent budget was not recorded")
			}
		})
	}
}

func TestRunSnapshotReadDiagnosticDependencyBeforeBudgetExhaustion(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), heartbeatWakeTimeout)
	defer cancel()
	m, c := platformWakeRaceFixture(t, 8)
	c.beforeInvoke = func(ctx context.Context, _ any) error {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > heartbeatWakeTimeout {
			t.Fatal("snapshot read changed the parent drain budget")
		}
		return status.Error(codes.Unavailable, "PRIVATE_SENTINEL")
	}
	rows := snapshotDiagnosticLogs(t, func() {
		_, err := m.readRunSnapshotWithin(parent, "PRIVATE_SENTINEL")
		if status.Code(err) != codes.Unavailable {
			t.Fatal("diagnostic changed dependency failure into a deadline")
		}
	})
	if len(rows) != 1 || rows[0]["grpc_code"] != "Unavailable" || rows[0]["parent_state"] != "ACTIVE" || rows[0]["read_state"] != "ACTIVE" {
		t.Fatal("dependency failure was not distinguished from exhausted parent budget")
	}
	start, ok := rows[0]["parent_budget_start_ms"].(float64)
	remaining, remainderOK := rows[0]["parent_budget_remaining_ms"].(float64)
	if !ok || !remainderOK || start <= 0 || start > 2000 || remaining <= 0 || remaining > start {
		t.Fatal("diagnostic did not preserve the exact bounded parent budget")
	}
}

func TestRunSnapshotDiagnosticSuccessAndProjectionFailure(t *testing.T) {
	m, c := platformWakeRaceFixture(t, 8)
	c.runSnapshot = completeRunSnapshotFixture()
	subscription := &runSubscription{rootRef: c.runSnapshot.Run.Ref, requestRef: "request_fixture01"}
	rows := snapshotDiagnosticLogs(t, func() {
		response, err := m.readRunSnapshotWithin(t.Context(), subscription.rootRef)
		if err != nil || response.GetRun().GetRef() != subscription.rootRef {
			t.Fatal("diagnostic changed successful owner snapshot read")
		}
		if _, err := projectCompleteRunSnapshot(response, subscription, m.localize); err != nil {
			t.Fatal("diagnostic changed valid snapshot projection")
		}
	})
	if len(rows) != 0 {
		t.Fatal("successful snapshot emitted a failure diagnostic")
	}
	c.runSnapshot.Run.Ref = "PRIVATE_SENTINEL"
	rows = snapshotDiagnosticLogs(t, func() {
		if m.sendRunSnapshot(subscription, c.runSnapshot) != true || subscription.available {
			t.Fatal("invalid snapshot no longer uses the closed stream problem")
		}
	})
	if len(rows) != 1 || len(rows[0]) != 6 || rows[0]["stage"] != runSnapshotProjectStage || rows[0]["error_class"] != "contract" || len(c.frames) != 1 {
		t.Fatal("projection diagnostic changed field set or terminal envelope count")
	}
	frames := platformWakeRaceFrames(c)
	if frame, ok := frames[0].(generated.StreamProblemEnvelope); !ok || frame.Code != "RUN_UNAVAILABLE" || frame.RequestRef != subscription.requestRef || frame.Cursor != subscription.cursor {
		t.Fatal("projection diagnostic changed its exact closed stream problem")
	}
}
