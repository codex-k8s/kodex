package governance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSelfDeployGatePreparerCallsGovernanceManager(t *testing.T) {
	t.Parallel()

	client := &fakeSelfDeployGateClient{
		response: &governancev1.SelfDeployPlanGateResponse{
			Status: governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_PENDING,
			RiskAssessment: &governancev1.RiskAssessment{
				Id:          "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
				Explanation: "owner approval required",
			},
			GateRequest: &governancev1.GateRequest{Id: "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"},
		},
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}
	input := agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{
			IdempotencyKey: "self_deploy_plan_gate:5f7f3a10-0001-4000-8000-000000000001",
			Actor:          value.Actor{Type: "service", ID: "agent-manager"},
		},
		Plan: validGatePlan(),
	}

	result, err := preparer.PrepareSelfDeployPlanGate(context.Background(), input)
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if result.Status != agentservice.SelfDeployPlanGateStatusPending {
		t.Fatalf("status = %s, want %s", result.Status, agentservice.SelfDeployPlanGateStatusPending)
	}
	if result.GovernanceContext.RiskAssessmentRef != "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa" ||
		result.GovernanceContext.GateRequestRef != "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb" {
		t.Fatalf("governance refs = %+v", result.GovernanceContext)
	}
	request := client.request
	if request.GetPlan().GetSelfDeployPlanRef() != "agent:self-deploy-plan:5f7f3a10-0001-4000-8000-000000000001" {
		t.Fatalf("self deploy plan ref = %q", request.GetPlan().GetSelfDeployPlanRef())
	}
	if request.GetPlan().GetServicesYamlDigest() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("services yaml digest = %q", request.GetPlan().GetServicesYamlDigest())
	}
	if len(request.GetPlan().GetAffectedServiceKeys()) != 1 || request.GetPlan().GetAffectedServiceKeys()[0] != "agent-manager" {
		t.Fatalf("affected service keys = %v", request.GetPlan().GetAffectedServiceKeys())
	}
	if request.GetPlan().GetProjectContext().GetProjectRef() != "project:platform" ||
		request.GetPlan().GetProjectContext().GetRepositoryRef() != "repository:kodex" {
		t.Fatalf("project context = %+v", request.GetPlan().GetProjectContext())
	}
	if request.GetPlan().GetRiskProfileRef() != "" {
		t.Fatalf("risk profile ref = %q, want empty built-in self-deploy profile", request.GetPlan().GetRiskProfileRef())
	}
	encoded := request.String()
	for _, forbidden := range []string{"webhook_body", "raw_provider_payload", "full_yaml", "secret_value", "token", "workspace_path"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("governance request contains forbidden marker %q", forbidden)
		}
	}
}

func TestSelfDeployGatePreparerPassesUUIDRiskProfileRef(t *testing.T) {
	t.Parallel()

	client := &fakeSelfDeployGateClient{
		response: &governancev1.SelfDeployPlanGateResponse{
			Status:         governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_APPROVED,
			RiskAssessment: &governancev1.RiskAssessment{Id: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"},
		},
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}
	plan := validGatePlan()
	plan.GovernanceContext.RiskProfileRef = "governance:risk_profile/cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if client.request.GetPlan().GetRiskProfileRef() != "governance:risk_profile:cccccccc-cccc-4ccc-8ccc-cccccccccccc" {
		t.Fatalf("risk profile ref = %q", client.request.GetPlan().GetRiskProfileRef())
	}
}

func TestSelfDeployGatePreparerRejectsIncompletePendingResponse(t *testing.T) {
	t.Parallel()

	client := &fakeSelfDeployGateClient{
		response: &governancev1.SelfDeployPlanGateResponse{
			Status:         governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_PENDING,
			RiskAssessment: &governancev1.RiskAssessment{Id: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"},
		},
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: validGatePlan(),
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want %v", err, errs.ErrDependencyUnavailable)
	}
}

func TestSelfDeployGatePreparerMapsGovernanceErrors(t *testing.T) {
	t.Parallel()

	preparer, err := newSelfDeployGatePreparer(&fakeSelfDeployGateClient{err: status.Error(codes.Aborted, "conflict")}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: validGatePlan(),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want %v", err, errs.ErrConflict)
	}
}

func validGatePlan() entity.SelfDeployPlan {
	return entity.SelfDeployPlan{
		VersionedBase: entity.VersionedBase{
			ID:      uuid.MustParse("5f7f3a10-0001-4000-8000-000000000001"),
			Version: 1,
		},
		ProjectRef:          "project:platform",
		RepositoryRef:       "repository:kodex",
		ProviderSignalRef:   "provider:repository_change/1",
		SourceRef:           "source:main",
		MergeCommitSHA:      "0123456789abcdef0123456789abcdef01234567",
		ServicesYAMLRef:     "project-catalog:services-policy/1",
		ServicesYAMLDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AffectedServiceKeys: []string{"agent-manager"},
		PathCategories: []enum.SelfDeployPathCategory{
			enum.SelfDeployPathCategoryServicesPolicy,
			enum.SelfDeployPathCategoryServiceSource,
		},
		ExpectedRuntimeJobTypes: []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild},
		GovernanceContext: value.GovernanceContextRef{
			RiskProfileRef: "governance:risk_profile/self-deploy",
			GatePolicyRef:  "governance:gate_policy/owner-approval",
		},
		SafeSummary:     "safe self-deploy summary",
		PlanFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status:          enum.SelfDeployPlanStatusPendingApproval,
	}
}

type fakeSelfDeployGateClient struct {
	request  *governancev1.PrepareSelfDeployPlanGateRequest
	response *governancev1.SelfDeployPlanGateResponse
	err      error
}

func (f *fakeSelfDeployGateClient) PrepareSelfDeployPlanGate(_ context.Context, request *governancev1.PrepareSelfDeployPlanGateRequest, _ ...grpc.CallOption) (*governancev1.SelfDeployPlanGateResponse, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}
