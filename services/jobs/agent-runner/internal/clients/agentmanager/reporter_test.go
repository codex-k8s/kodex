package agentmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/app"
)

func TestReportStartedRecordsStartingStateAndActivity(t *testing.T) {
	client := &fakeClient{status: runtimeStatus(agentsv1.AgentRunStatus_AGENT_RUN_STATUS_REQUESTED, 7)}
	reporter := mustReporter(t, client)

	err := reporter.ReportStarted(context.Background(), reportInput())
	if err != nil {
		t.Fatalf("ReportStarted() err = %v", err)
	}
	if len(client.runStates) != 1 {
		t.Fatalf("run state calls = %d, want 1", len(client.runStates))
	}
	state := client.runStates[0]
	if state.GetStatus() != agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING {
		t.Fatalf("status = %s, want STARTING", state.GetStatus())
	}
	if state.GetMeta().GetExpectedVersion() != 7 {
		t.Fatalf("expected version = %d, want 7", state.GetMeta().GetExpectedVersion())
	}
	if state.GetRuntimeContext().GetSlotRef() == "" {
		t.Fatal("runtime context slot ref is empty")
	}
	if len(client.activities) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(client.activities))
	}
	activity := client.activities[0]
	if activity.GetStatus() != agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_STARTED {
		t.Fatalf("activity status = %s, want STARTED", activity.GetStatus())
	}
	assertSafeJSON(t, activity.GetSafeRefsJson())
	assertSafeJSON(t, activity.GetSafeDetailsJson())
}

func TestReportFailedRecordsFailureStateAndActivity(t *testing.T) {
	client := &fakeClient{status: runtimeStatus(agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING, 8)}
	reporter := mustReporter(t, client)
	diagnostic := app.NewDiagnostic("agent_execution_contract_unavailable", "agent execution contract is not enabled", app.ExitFailure)

	err := reporter.ReportFailed(context.Background(), reportInput(), diagnostic)
	if err != nil {
		t.Fatalf("ReportFailed() err = %v", err)
	}
	if len(client.runStates) != 1 {
		t.Fatalf("run state calls = %d, want 1", len(client.runStates))
	}
	state := client.runStates[0]
	if state.GetStatus() != agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED {
		t.Fatalf("status = %s, want FAILED", state.GetStatus())
	}
	if state.GetFailureCode() != diagnostic.Code {
		t.Fatalf("failure code = %q, want %q", state.GetFailureCode(), diagnostic.Code)
	}
	if len(client.activities) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(client.activities))
	}
	if client.activities[0].GetStatus() != agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_FAILED {
		t.Fatalf("activity status = %s, want FAILED", client.activities[0].GetStatus())
	}
	assertSafeJSON(t, client.activities[0].GetSafeDetailsJson())
}

func TestReportFailedNoopsForTerminalRun(t *testing.T) {
	client := &fakeClient{status: runtimeStatus(agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED, 9)}
	reporter := mustReporter(t, client)
	diagnostic := app.NewDiagnostic("already_failed", "already failed", app.ExitFailure)

	if err := reporter.ReportFailed(context.Background(), reportInput(), diagnostic); err != nil {
		t.Fatalf("ReportFailed() err = %v", err)
	}
	if len(client.runStates) != 0 {
		t.Fatalf("run state calls = %d, want 0", len(client.runStates))
	}
	if len(client.activities) != 0 {
		t.Fatalf("activity calls = %d, want 0", len(client.activities))
	}
}

func TestReportActivityRedactsUnsafeDetails(t *testing.T) {
	client := &fakeClient{status: runtimeStatus(agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING, 8)}
	reporter := mustReporter(t, client)
	input := reportInput()
	input.Config.ContextRef = "prompt_body:do-not-log"
	input.Config.RunnerProfileRef = "secret_value:do-not-log"
	diagnostic := app.NewDiagnostic("agent_execution_contract_unavailable", "agent execution contract is not enabled", app.ExitFailure)

	err := reporter.ReportFailed(context.Background(), input, diagnostic)
	if err != nil {
		t.Fatalf("ReportFailed() err = %v", err)
	}
	if len(client.activities) != 1 {
		t.Fatalf("activity calls = %d, want 1", len(client.activities))
	}
	assertSafeJSON(t, client.activities[0].GetSafeRefsJson())
	assertSafeJSON(t, client.activities[0].GetSafeDetailsJson())
	if !strings.Contains(client.activities[0].GetSafeRefsJson(), "redacted") {
		t.Fatalf("safe refs were not redacted: %s", client.activities[0].GetSafeRefsJson())
	}
	if !strings.Contains(client.activities[0].GetSafeDetailsJson(), "redacted") {
		t.Fatalf("safe details were not redacted: %s", client.activities[0].GetSafeDetailsJson())
	}
}

func TestNewReporterFromConfigDisabled(t *testing.T) {
	reporter, closeReporter, err := NewReporterFromConfig(app.ReporterConfig{})
	if err != nil {
		t.Fatalf("NewReporterFromConfig() err = %v", err)
	}
	defer closeReporter()
	if _, ok := reporter.(app.NoopReporter); !ok {
		t.Fatalf("reporter = %T, want app.NoopReporter", reporter)
	}
}

type fakeClient struct {
	status     *agentsv1.AgentRunRuntimeStatusResponse
	runStates  []*agentsv1.RecordRunStateRequest
	activities []*agentsv1.RecordAgentActivityRequest
}

func (f *fakeClient) GetAgentRunRuntimeStatus(context.Context, *agentsv1.GetAgentRunRuntimeStatusRequest, ...grpc.CallOption) (*agentsv1.AgentRunRuntimeStatusResponse, error) {
	return f.status, nil
}

func (f *fakeClient) RecordRunState(_ context.Context, request *agentsv1.RecordRunStateRequest, _ ...grpc.CallOption) (*agentsv1.AgentRunResponse, error) {
	f.runStates = append(f.runStates, request)
	return &agentsv1.AgentRunResponse{Run: f.status.GetRun()}, nil
}

func (f *fakeClient) RecordAgentActivity(_ context.Context, request *agentsv1.RecordAgentActivityRequest, _ ...grpc.CallOption) (*agentsv1.AgentActivityResponse, error) {
	f.activities = append(f.activities, request)
	return &agentsv1.AgentActivityResponse{}, nil
}

func mustReporter(t *testing.T, client Client) *Reporter {
	t.Helper()
	reporter, err := NewReporter(client, app.ReporterConfig{AuthToken: "test-token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewReporter() err = %v", err)
	}
	return reporter
}

func runtimeStatus(status agentsv1.AgentRunStatus, version int64) *agentsv1.AgentRunRuntimeStatusResponse {
	runID := "11111111-1111-1111-1111-111111111111"
	return &agentsv1.AgentRunRuntimeStatusResponse{
		Run: &agentsv1.AgentRun{
			Id:        runID,
			SessionId: "55555555-5555-5555-5555-555555555555",
			Status:    status,
			Version:   version,
			RuntimeContext: &agentsv1.RuntimeContextRef{
				SlotRef:      strPtr("runtime.slot/33333333"),
				JobRef:       strPtr("runtime.job/22222222"),
				WorkspaceRef: strPtr("runtime.workspace/11111111"),
				ContextRef:   strPtr("runtime.context/agent-run.json"),
			},
			ProviderTarget: &agentsv1.ProviderTargetRef{WorkItemRef: strPtr("provider.work-item/123")},
		},
		RuntimeStatus: &agentsv1.AgentRunRuntimeStatus{
			RunId:      runID,
			RunStatus:  status,
			RunVersion: version,
			RuntimeContext: &agentsv1.RuntimeContextRef{
				SlotRef:      strPtr("runtime.slot/33333333"),
				JobRef:       strPtr("runtime.job/22222222"),
				WorkspaceRef: strPtr("runtime.workspace/11111111"),
				ContextRef:   strPtr("runtime.context/agent-run.json"),
			},
		},
	}
}

func reportInput() app.ReportInput {
	return app.ReportInput{
		Config: app.Config{
			AgentRunID:                         "11111111-1111-1111-1111-111111111111",
			RuntimeJobID:                       "22222222-2222-2222-2222-222222222222",
			SlotID:                             "33333333-3333-3333-3333-333333333333",
			ExpectedMaterializationID:          "44444444-4444-4444-4444-444444444444",
			ExpectedMaterializationFingerprint: "materialization:fingerprint:abc123",
			WorkspaceRef:                       "runtime.workspace/11111111",
			ContextRef:                         "runtime.context/agent-run.json",
			ContextDigest:                      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			RunnerProfileRef:                   "runner-profile/codex-agent@v1",
			RunnerMode:                         app.RunnerModeCodexAgent,
		},
		Context: app.AgentRunContext{
			AgentRunID:           "11111111-1111-1111-1111-111111111111",
			AgentSessionID:       "55555555-5555-5555-5555-555555555555",
			WorkspaceFingerprint: "materialization:fingerprint:abc123",
		},
		StartedAt:  time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 5, 29, 12, 1, 0, 0, time.UTC),
	}
}

func assertSafeJSON(t *testing.T, value string) {
	t.Helper()
	lower := strings.ToLower(value)
	for _, marker := range []string{"prompt", "transcript", "tool_input", "tool_output", "provider_payload", "secret_value", "kubeconfig"} {
		if strings.Contains(lower, marker) {
			t.Fatalf("safe JSON contains forbidden marker %q: %s", marker, value)
		}
	}
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) == "{}" {
		t.Fatalf("safe JSON is empty: %q", value)
	}
}

func strPtr(value string) *string {
	return &value
}
