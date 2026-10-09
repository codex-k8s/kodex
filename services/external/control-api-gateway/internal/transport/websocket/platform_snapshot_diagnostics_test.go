package websockettransport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
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
				observePlatformSnapshotReadFailure(ctx, time.Now(), -1, tc.kind, status.Error(tc.code, "PRIVATE_SENTINEL"), 1, platformSnapshotPageSize)
			})
			if len(rows) != 1 || len(rows[0]) != 14 {
				t.Fatal("platform diagnostic changed its exact field set or count")
			}
			row := rows[0]
			if row["msg"] != platformSnapshotReadFailure || row["kind"] != tc.wantKind ||
				row["stage"] != platformSnapshotDiagnosticStage || row["grpc_code"] != tc.wantCode ||
				row["read_stage"] != "UNKNOWN" || row["error_class"] != "dependency" || row["parent_state"] != "ACTIVE" ||
				row[platformSnapshotAttemptKey] != float64(0) || row[platformSnapshotAssistantPageKey] != float64(0) ||
				row["parent_budget_start_ms"] != float64(-1) || row["parent_budget_remaining_ms"] != float64(-1) {
				t.Fatal("platform diagnostic lost its closed code or parent context state")
			}
		})
	}
}

func TestPlatformSnapshotDiagnosticStagePreservesClosedMapping(t *testing.T) {
	for _, stage := range []platformSnapshotReadStage{
		platformSnapshotAssistantGetStage, platformSnapshotConversationListStage,
		platformSnapshotBootstrapGetStage, platformSnapshotAssistantProjectStage,
		"PRIVATE_SENTINEL",
	} {
		for _, kind := range []string{"SYSTEM_ASSISTANT", "RUN"} {
			t.Run(kind+"/"+string(stage), func(t *testing.T) {
				cause := status.Error(codes.DeadlineExceeded, "PRIVATE_SENTINEL")
				wrapped := fmt.Errorf("PRIVATE_SENTINEL: %w", withPlatformSnapshotReadStage(stage, cause))
				if !errors.Is(wrapped, cause) || status.Code(wrapped) != codes.DeadlineExceeded {
					t.Fatal("stage attribution changed the original error mapping")
				}
				rows := snapshotDiagnosticLogs(t, func() {
					observePlatformSnapshotReadFailure(t.Context(), time.Now(), -1, kind, wrapped, 1, platformSnapshotPageSize)
				})
				want := string(stage)
				if stage == "PRIVATE_SENTINEL" || kind != "SYSTEM_ASSISTANT" {
					want = platformSnapshotDiagnosticUnknown
				}
				if len(rows) != 1 || len(rows[0]) != 14 || rows[0][platformSnapshotReadStageKey] != want || rows[0][runSnapshotDiagnosticGRPCCodeKey] != "DeadlineExceeded" {
					t.Fatal("stage attribution exposed an unknown stage or changed the single boundary log")
				}
			})
		}
	}
	if withPlatformSnapshotReadStage(platformSnapshotAssistantGetStage, nil) != nil {
		t.Fatal("stage attribution invented a successful read failure")
	}
}

type platformSnapshotStageRecorder struct {
	assistantSnapshotSizeRecorder
	failStage        platformSnapshotReadStage
	failure          error
	invalidBootstrap bool
	deadline         time.Time
	calls            int
	failOnCall       int
}

func (c *platformSnapshotStageRecorder) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.Equal(c.deadline) {
		return errors.New("stage read changed its parent deadline")
	}
	c.calls++
	var stage platformSnapshotReadStage
	switch response.(type) {
	case *cp.GetSystemAssistantResponse:
		stage = platformSnapshotAssistantGetStage
	case *cp.ListAssistantConversationsResponse:
		stage = platformSnapshotConversationListStage
	case *cp.GetBootstrapStateResponse:
		stage = platformSnapshotBootstrapGetStage
		if c.invalidBootstrap {
			return nil
		}
	}
	if stage == c.failStage && (c.failOnCall == 0 || c.calls == c.failOnCall) {
		return c.failure
	}
	return c.assistantSnapshotSizeRecorder.Invoke(ctx, method, request, response, options...)
}

func TestPlatformSnapshotSystemAssistantReadStageAttribution(t *testing.T) {
	for _, tc := range []struct {
		stage platformSnapshotReadStage
		calls int
	}{
		{platformSnapshotAssistantGetStage, 1},
		{platformSnapshotConversationListStage, 2},
		{platformSnapshotBootstrapGetStage, 3},
		{platformSnapshotAssistantProjectStage, 3},
	} {
		t.Run(string(tc.stage), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), heartbeatWakeTimeout)
			defer cancel()
			deadline, _ := ctx.Deadline()
			cause := status.Error(codes.DeadlineExceeded, "PRIVATE_SENTINEL")
			c := &platformSnapshotStageRecorder{failStage: tc.stage, failure: cause, deadline: deadline,
				invalidBootstrap: tc.stage == platformSnapshotAssistantProjectStage}
			m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "prj_fixture01")
			m.server.query = cp.NewPlatformQueryServiceClient(c)
			m.server.assistant = cp.NewSystemAssistantServiceClient(c)
			rows := snapshotDiagnosticLogs(t, func() {
				_, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindSystemAssistant})
				if err == nil || platformSnapshotErrorStage("SYSTEM_ASSISTANT", err) != string(tc.stage) {
					t.Fatal("actual snapshot path lost its exact failed stage")
				}
				if tc.stage != platformSnapshotAssistantProjectStage && (!errors.Is(err, cause) || status.Code(err) != codes.DeadlineExceeded) {
					t.Fatal("actual snapshot path changed its owner RPC error")
				}
			})
			if c.calls != tc.calls || len(rows) != 1 || rows[0][platformSnapshotReadStageKey] != string(tc.stage) || rows[0][platformSnapshotAttemptKey] != float64(1) || rows[0][platformSnapshotAssistantPageKey] != float64(platformSnapshotPageSize) || len(c.frames) != 0 || m.platformCursor != 123 || ctx.Err() != nil {
				t.Fatal("stage diagnostics changed RPC sequence, cursor, cancellation or emitted a partial snapshot")
			}
		})
	}
}

func TestPlatformSnapshotSystemAssistantSuccessDoesNotLogStage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), heartbeatWakeTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	c := &platformSnapshotStageRecorder{deadline: deadline}
	m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "prj_fixture01")
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	rows := snapshotDiagnosticLogs(t, func() {
		value, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindSystemAssistant})
		if err != nil || value.Snapshot.Assistant == nil || value.Snapshot.Conversations == nil || value.Snapshot.Bootstrap == nil {
			t.Fatal("stage diagnostics lost a complete successful snapshot")
		}
	})
	if c.calls != 3 || len(rows) != 0 || ctx.Err() != nil {
		t.Fatal("successful owner reads were retried, canceled or logged as failure")
	}
}

func TestPlatformSnapshotDiagnosticPageLadderIsClosed(t *testing.T) {
	for index, size := range []int32{50, 25, 12, 6, 3, 1} {
		attempt, page := platformSnapshotDiagnosticPage("SYSTEM_ASSISTANT", index+1, size)
		if attempt != index+1 || page != size {
			t.Fatal("diagnostic rejected the existing whole-page ladder")
		}
	}
	for _, tc := range []struct {
		kind    string
		attempt int
		page    int32
	}{
		{"RUN", 1, 50}, {"PRIVATE_SENTINEL", 1, 50},
		{"SYSTEM_ASSISTANT", 0, 50}, {"SYSTEM_ASSISTANT", -1, 50},
		{"SYSTEM_ASSISTANT", 7, 1}, {"SYSTEM_ASSISTANT", 1000000, 1},
		{"SYSTEM_ASSISTANT", 1, 25}, {"SYSTEM_ASSISTANT", 2, 50},
		{"SYSTEM_ASSISTANT", 1, -1}, {"SYSTEM_ASSISTANT", 1, 1000000},
	} {
		if attempt, page := platformSnapshotDiagnosticPage(tc.kind, tc.attempt, tc.page); attempt != 0 || page != 0 {
			t.Fatal("diagnostic accepted an unbounded or mismatched page context")
		}
	}
}

func TestPlatformSnapshotDiagnosticIdentifiesOversizedRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), heartbeatWakeTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	cause := status.Error(codes.Unavailable, "PRIVATE_SENTINEL")
	c := &platformSnapshotStageRecorder{deadline: deadline, failStage: platformSnapshotAssistantGetStage, failure: cause, failOnCall: 4}
	c.conversations = assistantSizeConversations(42, strings.Repeat("Я", 16000))
	m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "prj_fixture01")
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	rows := snapshotDiagnosticLogs(t, func() {
		_, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindSystemAssistant})
		if !errors.Is(err, cause) || status.Code(err) != codes.Unavailable {
			t.Fatal("page retry diagnostic changed the owner error")
		}
	})
	if c.calls != 4 || len(rows) != 1 || rows[0][platformSnapshotAttemptKey] != float64(2) || rows[0][platformSnapshotAssistantPageKey] != float64(25) || rows[0][platformSnapshotReadStageKey] != string(platformSnapshotAssistantGetStage) || len(c.frames) != 0 {
		t.Fatal("diagnostic confused the reduced page attempt with a new wake or emitted partial data")
	}
}

func TestPlatformSnapshotDiagnosticExpiredParentAndSuccess(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	rows := snapshotDiagnosticLogs(t, func() {
		observePlatformSnapshotReadFailure(ctx, time.Now(), 0, "RUN", status.FromContextError(ctx.Err()).Err(), 1, platformSnapshotPageSize)
		observePlatformSnapshotReadFailure(ctx, time.Now(), 0, "RUN", nil, 1, platformSnapshotPageSize)
	})
	if len(rows) != 1 || rows[0]["grpc_code"] != "DeadlineExceeded" ||
		rows[0]["parent_state"] != "DEADLINE_EXCEEDED" || rows[0]["parent_budget_remaining_ms"] != float64(0) {
		t.Fatal("platform diagnostic lost parent expiry or logged successful read")
	}
}
