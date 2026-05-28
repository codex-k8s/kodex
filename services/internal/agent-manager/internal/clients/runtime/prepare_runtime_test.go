package runtime

import (
	"context"
	"errors"
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
		Meta:       value.CommandMeta{CommandID: commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		AgentRunID: agentRunID,
		SlotRef:    slotID.String(),
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
	if request.GetMeta().GetCommandId() != commandID.String() || request.GetMeta().GetActor().GetId() != "agent-manager" {
		t.Fatalf("meta = %+v", request.GetMeta())
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

type fakeRuntimeManagerClient struct {
	createJobRequest *runtimev1.CreateJobRequest
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
