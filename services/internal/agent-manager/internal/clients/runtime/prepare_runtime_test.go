package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func TestCreateAgentRunJobMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("a1a1a1a1-1111-2222-3333-444444444444")
	agentRunID := uuid.MustParse("a1a1a1a1-2222-3333-4444-555555555555")
	slotID := uuid.MustParse("a1a1a1a1-3333-4444-5555-666666666666")
	jobID := uuid.MustParse("a1a1a1a1-4444-5555-6666-777777777777")
	agentRunText := agentRunID.String()
	slotText := slotID.String()
	client := &fakeRuntimeManagerClient{
		jobResponse: &runtimev1.JobResponse{Job: &runtimev1.Job{
			JobId:      jobID.String(),
			JobType:    runtimev1.JobType_JOB_TYPE_AGENT_RUN,
			Status:     runtimev1.JobStatus_JOB_STATUS_PENDING,
			SlotId:     &slotText,
			AgentRunId: &agentRunText,
			NextAction: "claim_by_agent_executor",
		}},
	}
	preparer, err := newPreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newPreparer() err = %v", err)
	}

	result, err := preparer.CreateAgentRunJob(context.Background(), agentservice.RuntimeJobInput{
		Meta:          value.CommandMeta{CommandID: commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		AgentRunID:    agentRunID,
		SlotRef:       slotID.String(),
		ExecutionSpec: testAgentRunExecutionSpec(agentRunID, slotID),
	})
	if err != nil {
		t.Fatalf("CreateAgentRunJob() err = %v", err)
	}
	if result.JobRef != jobID.String() || result.Status != "pending" || result.DiagnosticSummary == "" {
		t.Fatalf("result = %+v", result)
	}
	request := client.createJobRequest
	if request.GetJobType() != runtimev1.JobType_JOB_TYPE_AGENT_RUN || request.GetPriority() != runtimev1.JobPriority_JOB_PRIORITY_NORMAL {
		t.Fatalf("job kind/priority = %s/%s", request.GetJobType(), request.GetPriority())
	}
	if request.GetSlotId() != slotID.String() || request.GetAgentRunId() != agentRunID.String() || request.GetJobInputJson() != "{}" {
		t.Fatalf("job refs/input = slot %q run %q input %q", request.GetSlotId(), request.GetAgentRunId(), request.GetJobInputJson())
	}
	spec := request.GetAgentRunExecutionSpec()
	if spec == nil || spec.GetAgentRunId() != agentRunID.String() || spec.GetSlotId() != slotID.String() {
		t.Fatalf("agent run execution spec = %+v", spec)
	}
	if spec.GetRunnerMode() != runtimev1.AgentRunRunnerMode_AGENT_RUN_RUNNER_MODE_CODEX_AGENT ||
		spec.GetRunnerImageRef() != "image://codex-agent@sha256:runner" ||
		spec.GetContextDigest() != "sha256:agent-run-context" {
		t.Fatalf("agent run execution runner/context = %+v", spec)
	}
	if len(spec.GetAllowedSecretRefs()) != 1 || spec.GetAllowedSecretRefs()[0].GetSecretRef() != "secret://runtime/agent-token" {
		t.Fatalf("allowed secret refs = %+v", spec.GetAllowedSecretRefs())
	}
	if len(spec.GetReportingTargetRefs()) != 1 || spec.GetReportingTargetRefs()[0].GetKind() != "agent_run_state" {
		t.Fatalf("reporting target refs = %+v", spec.GetReportingTargetRefs())
	}
	codexSpec := spec.GetCodexSessionExecutionSpec()
	if codexSpec == nil ||
		codexSpec.GetInstructionObjectRef() != "object://instructions/agent-run" ||
		codexSpec.GetInstructionObjectDigest() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		codexSpec.GetResultSchemaRef() != "object://schemas/codex-result-v1" ||
		codexSpec.GetResultSchemaDigest() != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		codexSpec.GetWorkspaceSnapshotRef() != "runtime://workspace-snapshots/agent-run" ||
		codexSpec.GetHookEndpointRef() != "hook://codex-hook-ingress/agent-runner" ||
		codexSpec.GetTimeoutSeconds() != 1800 ||
		codexSpec.GetRunnerMode() != runtimev1.AgentRunRunnerMode_AGENT_RUN_RUNNER_MODE_CODEX_AGENT {
		t.Fatalf("codex session execution spec = %+v", codexSpec)
	}
	if len(codexSpec.GetCallbackRefs()) != 1 || len(codexSpec.GetOutputRefs()) != 1 || len(codexSpec.GetResultRefs()) != 1 {
		t.Fatalf("codex execution refs = %+v/%+v/%+v", codexSpec.GetCallbackRefs(), codexSpec.GetOutputRefs(), codexSpec.GetResultRefs())
	}
	requestPayload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	for _, forbidden := range []string{"prompt_text", "transcript", "raw_provider_payload", "secret_value", "kubeconfig", "stdout", "stderr"} {
		if strings.Contains(string(requestPayload), forbidden) {
			t.Fatalf("request contains forbidden payload marker %q: %s", forbidden, requestPayload)
		}
	}
	if request.GetMeta().GetCommandId() != commandID.String() || request.GetMeta().GetActor().GetId() != "agent-manager" {
		t.Fatalf("meta = %+v", request.GetMeta())
	}
}

func TestPrepareRuntimeResultExtractsGeneratedContextDigest(t *testing.T) {
	t.Parallel()

	slotID := uuid.MustParse("d1d1d1d1-1111-2222-3333-444444444444")
	materializationID := uuid.MustParse("d1d1d1d1-2222-3333-4444-555555555555")
	slotText := slotID.String()
	result, err := prepareRuntimeResult(agentservice.RuntimePreparationInput{
		WorkspacePolicy: agentservice.RuntimeWorkspacePolicy{PolicyDigest: "sha256:policy"},
	}, &runtimev1.PrepareRuntimeResponse{
		Slot: &runtimev1.Slot{SlotId: slotText, Status: runtimev1.SlotStatus_SLOT_STATUS_READY, Fingerprint: "sha256:slot"},
		WorkspaceMaterialization: &runtimev1.WorkspaceMaterialization{
			WorkspaceMaterializationId: materializationID.String(),
			SlotId:                     slotText,
			Status:                     runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_COMPLETED,
			Fingerprint:                "sha256:workspace",
			Sources: []*runtimev1.WorkspaceSource{{
				Kind:   runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT,
				Digest: optionalString("sha256:agent-run-context"),
			}},
		},
		RuntimeContext: &runtimev1.RuntimeContext{
			SlotId:                     slotText,
			MaterializationFingerprint: "sha256:workspace",
		},
	})
	if err != nil {
		t.Fatalf("prepareRuntimeResult() err = %v", err)
	}
	if result.ContextDigest != "sha256:agent-run-context" ||
		result.ContextRef != "runtime://workspace-materializations/"+materializationID.String()+"/context/agent-run.json" {
		t.Fatalf("context refs = %+v", result)
	}
	if result.SlotStatus != agentservice.RuntimeSlotStatusReady ||
		result.WorkspaceMaterializationStatus != agentservice.RuntimeWorkspaceMaterializationStatusCompleted {
		t.Fatalf("runtime statuses = %+v", result)
	}
}

func TestCreateAgentRunJobRejectsIncompleteResponse(t *testing.T) {
	t.Parallel()

	preparer, err := newPreparer(&fakeRuntimeManagerClient{jobResponse: &runtimev1.JobResponse{}}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newPreparer() err = %v", err)
	}
	_, err = preparer.CreateAgentRunJob(context.Background(), agentservice.RuntimeJobInput{AgentRunID: uuid.New(), SlotRef: uuid.NewString()})
	var classified *agentservice.RuntimeJobError
	if !errors.As(err, &classified) || !classified.Retryable || classified.Code != "dependency_unavailable" {
		t.Fatalf("CreateAgentRunJob() err = %v, want retryable dependency error", err)
	}
}

func TestCreateAgentRunJobMapsRuntimeErrors(t *testing.T) {
	t.Parallel()

	preparer, err := newPreparer(&fakeRuntimeManagerClient{err: status.Error(codes.InvalidArgument, "bad request")}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newPreparer() err = %v", err)
	}
	_, err = preparer.CreateAgentRunJob(context.Background(), agentservice.RuntimeJobInput{AgentRunID: uuid.New(), SlotRef: uuid.NewString()})
	var classified *agentservice.RuntimeJobError
	if !errors.As(err, &classified) || classified.Retryable || classified.Code != "invalid_argument" {
		t.Fatalf("CreateAgentRunJob() err = %v, want permanent invalid_argument", err)
	}
}

func TestGetAgentRunJobMapsSafeStatus(t *testing.T) {
	t.Parallel()

	agentRunID := uuid.MustParse("b1b1b1b1-1111-2222-3333-444444444444")
	jobID := uuid.MustParse("b1b1b1b1-2222-3333-4444-555555555555")
	createdAt := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	startedAt := time.Date(2026, 5, 28, 10, 1, 0, 0, time.UTC).Format(time.RFC3339Nano)
	agentRunText := agentRunID.String()
	client := &fakeRuntimeManagerClient{
		jobResponse: &runtimev1.JobResponse{Job: &runtimev1.Job{
			JobId:            jobID.String(),
			CommandId:        "command-123",
			JobType:          runtimev1.JobType_JOB_TYPE_AGENT_RUN,
			Status:           runtimev1.JobStatus_JOB_STATUS_RUNNING,
			AgentRunId:       &agentRunText,
			CreatedAt:        createdAt,
			StartedAt:        &startedAt,
			NextAction:       "wait_for_executor",
			LastErrorCode:    "retryable",
			LastErrorMessage: "safe retry summary",
			ShortLogTail:     "raw log must not be copied",
			FullLogRef:       "workspace/log/ref",
			Version:          9,
		}},
	}
	preparer, err := newPreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newPreparer() err = %v", err)
	}

	result, err := preparer.GetAgentRunJob(context.Background(), agentservice.RuntimeJobReadInput{
		Meta:       value.QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}},
		AgentRunID: agentRunID,
		JobRef:     jobID.String(),
	})
	if err != nil {
		t.Fatalf("GetAgentRunJob() err = %v", err)
	}
	if result.JobRef != jobID.String() || result.CommandRef != "command-123" || result.Status != agentservice.RuntimeJobStatusRunning || result.Version != 9 {
		t.Fatalf("result = %+v", result)
	}
	if result.CreatedAt == nil || result.StartedAt == nil || result.SafeErrorCode != "retryable" || result.SafeErrorSummary != "safe retry summary" {
		t.Fatalf("safe fields = %+v", result)
	}
	if result.SafeSummary == "" || result.SafeSummary == "raw log must not be copied" || result.SafeSummary == "workspace/log/ref" {
		t.Fatalf("safe summary = %q", result.SafeSummary)
	}
	request := client.getJobRequest
	if request.GetJobId() != jobID.String() || request.GetMeta().GetActor().GetId() != "owner" || request.GetMeta().GetRequestContext().GetSource() != callerID {
		t.Fatalf("request = %+v", request)
	}
}

func TestGetAgentRunJobRejectsMismatchedRefs(t *testing.T) {
	t.Parallel()

	agentRunID := uuid.MustParse("c1c1c1c1-1111-2222-3333-444444444444")
	otherRunID := uuid.MustParse("c1c1c1c1-2222-3333-4444-555555555555")
	otherRunText := otherRunID.String()
	client := &fakeRuntimeManagerClient{
		jobResponse: &runtimev1.JobResponse{Job: &runtimev1.Job{
			JobId:      "job-1",
			JobType:    runtimev1.JobType_JOB_TYPE_AGENT_RUN,
			Status:     runtimev1.JobStatus_JOB_STATUS_PENDING,
			AgentRunId: &otherRunText,
		}},
	}
	preparer, err := newPreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newPreparer() err = %v", err)
	}

	_, err = preparer.GetAgentRunJob(context.Background(), agentservice.RuntimeJobReadInput{AgentRunID: agentRunID, JobRef: "job-1"})
	var classified *agentservice.RuntimeJobError
	if !errors.As(err, &classified) || !classified.Retryable || classified.Code != "conflict" {
		t.Fatalf("GetAgentRunJob() err = %v, want retryable conflict", err)
	}
}

func testAgentRunExecutionSpec(agentRunID uuid.UUID, slotID uuid.UUID) agentservice.AgentRunExecutionSpec {
	return agentservice.AgentRunExecutionSpec{
		AgentRunID:                         agentRunID,
		SlotID:                             slotID,
		ExpectedMaterializationID:          uuid.MustParse("a1a1a1a1-5555-6666-7777-888888888888"),
		ExpectedMaterializationFingerprint: "sha256:workspace",
		WorkspaceRef:                       "runtime://workspace-materializations/a1a1a1a1-5555-6666-7777-888888888888",
		WorkspaceMountRef:                  "runtime://slots/" + slotID.String() + "/workspace-mount",
		ContextRef:                         "runtime://workspace-materializations/a1a1a1a1-5555-6666-7777-888888888888/context/agent-run.json",
		ContextDigest:                      "sha256:agent-run-context",
		RunnerProfileRef:                   "runner-profile://go-full",
		RunnerImageRef:                     "image://codex-agent@sha256:runner",
		RunnerMode:                         agentservice.RuntimeJobRunnerModeCodexAgent,
		AllowedSecretRefs: []agentservice.AgentRunExecutionRef{
			{Kind: "runtime_api", Ref: "secret://runtime/agent-token"},
		},
		ReportingTargetRefs: []agentservice.AgentRunExecutionRef{
			{Kind: "agent_run_state", Ref: "agent-manager://runs/" + agentRunID.String()},
		},
		CodexSessionExecutionSpec: &agentservice.CodexSessionExecutionSpec{
			CodexSessionExecutionInputRefs: agentservice.CodexSessionExecutionInputRefs{
				InstructionObjectRef:    "object://instructions/agent-run",
				InstructionObjectDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ResultSchemaRef:         "object://schemas/codex-result-v1",
				ResultSchemaDigest:      "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				WorkspaceSnapshotRef:    "runtime://workspace-snapshots/agent-run",
				HookEndpointRef:         "hook://codex-hook-ingress/agent-runner",
				CallbackRefs: []agentservice.AgentRunExecutionRef{
					{Kind: "agent_run_state", Ref: "agent-manager://runs/" + agentRunID.String()},
				},
			},
			CodexSessionExecutionIORefs: agentservice.CodexSessionExecutionIORefs{
				TimeoutSeconds:   1800,
				RunnerProfileRef: "runner-profile://go-full",
				RunnerMode:       agentservice.RuntimeJobRunnerModeCodexAgent,
				OutputRefs: []agentservice.AgentRunExecutionRef{
					{Kind: "codex_output", Ref: "agent-manager://runs/" + agentRunID.String() + "/codex-output"},
				},
				ResultRefs: []agentservice.AgentRunExecutionRef{
					{Kind: "codex_result", Ref: "agent-manager://runs/" + agentRunID.String() + "/codex-result"},
				},
				AllowedSecretRefs: []agentservice.AgentRunExecutionRef{
					{Kind: "runtime_api", Ref: "secret://runtime/agent-token"},
				},
			},
		},
	}
}

type fakeRuntimeManagerClient struct {
	createJobRequest *runtimev1.CreateJobRequest
	getJobRequest    *runtimev1.GetJobRequest
	jobResponse      *runtimev1.JobResponse
	err              error
}

func (f *fakeRuntimeManagerClient) PrepareRuntime(context.Context, *runtimev1.PrepareRuntimeRequest, ...grpc.CallOption) (*runtimev1.PrepareRuntimeResponse, error) {
	return nil, errors.New("PrepareRuntime should not be called")
}

func (f *fakeRuntimeManagerClient) CreateJob(_ context.Context, request *runtimev1.CreateJobRequest, _ ...grpc.CallOption) (*runtimev1.JobResponse, error) {
	f.createJobRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return f.jobResponse, nil
}

func (f *fakeRuntimeManagerClient) GetJob(_ context.Context, request *runtimev1.GetJobRequest, _ ...grpc.CallOption) (*runtimev1.JobResponse, error) {
	f.getJobRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return f.jobResponse, nil
}
