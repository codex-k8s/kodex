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

func TestSelfDeployGatePreparerRecoversExistingGateRefsAfterPrepareError(t *testing.T) {
	t.Parallel()

	riskAssessmentID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	gateRequestID := "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan := validGatePlan()
	planRef := "agent:self-deploy-plan:" + plan.ID.String()
	riskResponse, gateResponse := existingSelfDeployGateResponses(plan, riskAssessmentID, gateRequestID)
	client := &fakeSelfDeployGateClient{
		err:          status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponse: riskResponse,
		gateResponse: gateResponse,
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	result, err := preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v", err)
	}
	if result.Status != agentservice.SelfDeployPlanGateStatusPending ||
		result.GovernanceContext.RiskAssessmentRef != "governance:risk_assessment/"+riskAssessmentID ||
		result.GovernanceContext.GateRequestRef != "governance:gate_request/"+gateRequestID {
		t.Fatalf("result = %+v, want existing governance refs", result)
	}
	if client.riskRequest.GetTarget().GetRef() != planRef ||
		client.gateRequest.GetRiskAssessmentId() != riskAssessmentID ||
		client.gateRequest.GetTarget().GetRef() != "" {
		t.Fatalf("lookup requests = %+v / %+v", client.riskRequest, client.gateRequest)
	}
}

func TestSelfDeployGatePreparerRecoversExistingGateRefsWithRiskReadOnly(t *testing.T) {
	t.Parallel()

	riskAssessmentID := "59c67c5d-847b-4296-9afa-25aa3028a313"
	gateRequestID := "61159123-6947-4864-897b-2eb97980eb6b"
	plan := validGatePlan()
	riskResponse, gateResponse := existingSelfDeployGateResponses(plan, riskAssessmentID, gateRequestID)
	client := &fakeSelfDeployGateClient{
		err:              status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponse:     riskResponse,
		gateResponse:     gateResponse,
		rejectGateTarget: true,
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	result, err := preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v", err)
	}
	if result.GovernanceContext.RiskAssessmentRef != "governance:risk_assessment/"+riskAssessmentID ||
		result.GovernanceContext.GateRequestRef != "governance:gate_request/"+gateRequestID {
		t.Fatalf("result = %+v, want existing governance refs", result)
	}
	if client.gateRequest.GetRiskAssessmentId() != riskAssessmentID || client.gateRequest.GetTarget().GetRef() != "" {
		t.Fatalf("gate lookup request = %+v, want assessment-only lookup", client.gateRequest)
	}
}

func TestSelfDeployGatePreparerRecoversExistingGateRefsWithTargetOnlyRiskLookup(t *testing.T) {
	t.Parallel()

	riskAssessmentID := "59c67c5d-847b-4296-9afa-25aa3028a313"
	gateRequestID := "61159123-6947-4864-897b-2eb97980eb6b"
	plan := validGatePlan()
	riskResponse, gateResponse := existingSelfDeployGateResponses(plan, riskAssessmentID, gateRequestID)
	client := &fakeSelfDeployGateClient{
		err: status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponseForRequest: func(request *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error) {
			if request.GetProjectContext().GetRepositoryRef() != "" {
				return &governancev1.ListRiskAssessmentsResponse{}, nil
			}
			return riskResponse, nil
		},
		gateResponse: gateResponse,
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	result, err := preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v", err)
	}
	if result.GovernanceContext.RiskAssessmentRef != "governance:risk_assessment/"+riskAssessmentID ||
		result.GovernanceContext.GateRequestRef != "governance:gate_request/"+gateRequestID {
		t.Fatalf("result = %+v, want existing governance refs", result)
	}
	if len(client.riskRequests) != 2 {
		t.Fatalf("risk lookup requests = %d, want project-scoped lookup then target-only fallback", len(client.riskRequests))
	}
	if client.riskRequests[0].GetProjectContext().GetRepositoryRef() == "" ||
		client.riskRequests[1].GetProjectContext().GetRepositoryRef() != "" {
		t.Fatalf("risk lookup project contexts = %+v / %+v", client.riskRequests[0].GetProjectContext(), client.riskRequests[1].GetProjectContext())
	}
	if client.gateRequest.GetRiskAssessmentId() != riskAssessmentID || client.gateRequest.GetTarget().GetRef() != "" {
		t.Fatalf("gate lookup request = %+v, want assessment-only lookup", client.gateRequest)
	}
}

func TestSelfDeployGatePreparerReportsExistingRiskLookupFailure(t *testing.T) {
	t.Parallel()

	client := &fakeSelfDeployGateClient{
		err:     status.Error(codes.Unavailable, "prepare command replay failed"),
		riskErr: status.Error(codes.PermissionDenied, "read denied"),
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: validGatePlan(),
	})
	if code := agentservice.SelfDeployGateRecoveryErrorCode(err); code != agentservice.SelfDeployGateRecoveryCodeExistingRiskLookupFailed {
		t.Fatalf("recovery code = %q, want %q", code, agentservice.SelfDeployGateRecoveryCodeExistingRiskLookupFailed)
	}
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want dependency lookup error", err)
	}
}

func TestSelfDeployGatePreparerReportsExistingRiskFingerprintMismatch(t *testing.T) {
	t.Parallel()

	plan := validGatePlan()
	riskResponse, _ := existingSelfDeployGateResponses(plan, "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	riskResponse.RiskAssessments[0].EvidenceRefs[0].Digest = ptrString("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	client := &fakeSelfDeployGateClient{
		err:          status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponse: riskResponse,
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if code := agentservice.SelfDeployGateRecoveryErrorCode(err); code != agentservice.SelfDeployGateRecoveryCodeExistingRiskFingerprintMismatch {
		t.Fatalf("recovery code = %q, want %q", code, agentservice.SelfDeployGateRecoveryCodeExistingRiskFingerprintMismatch)
	}
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want fingerprint conflict", err)
	}
}

func TestSelfDeployGatePreparerReportsExistingRiskConflict(t *testing.T) {
	t.Parallel()

	plan := validGatePlan()
	riskResponse, _ := existingSelfDeployGateResponses(plan, "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	planRef := "agent:self-deploy-plan:" + plan.ID.String()
	riskResponse.RiskAssessments = append(riskResponse.RiskAssessments, &governancev1.RiskAssessment{
		Id: "cccccccc-cccc-4ccc-cccc-cccccccccccc",
		Target: &governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN,
			Ref:  planRef,
		},
		Status: governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE,
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:   governancev1.EvidenceKind_EVIDENCE_KIND_SELF_DEPLOY_PLAN,
			Ref:    planRef,
			Digest: ptrString(plan.PlanFingerprint),
		}},
	})
	client := &fakeSelfDeployGateClient{
		err:          status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponse: riskResponse,
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if code := agentservice.SelfDeployGateRecoveryErrorCode(err); code != agentservice.SelfDeployGateRecoveryCodeExistingRiskConflict {
		t.Fatalf("recovery code = %q, want %q", code, agentservice.SelfDeployGateRecoveryCodeExistingRiskConflict)
	}
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want conflict", err)
	}
}

func TestSelfDeployGatePreparerReportsExistingGateLookupFailure(t *testing.T) {
	t.Parallel()

	plan := validGatePlan()
	riskResponse, _ := existingSelfDeployGateResponses(plan, "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	client := &fakeSelfDeployGateClient{
		err:          status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponse: riskResponse,
		gateErr:      status.Error(codes.PermissionDenied, "gate read denied"),
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if code := agentservice.SelfDeployGateRecoveryErrorCode(err); code != agentservice.SelfDeployGateRecoveryCodeExistingGateLookupFailed {
		t.Fatalf("recovery code = %q, want %q", code, agentservice.SelfDeployGateRecoveryCodeExistingGateLookupFailed)
	}
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want dependency lookup error", err)
	}
}

func TestSelfDeployGatePreparerReportsExistingGateMismatch(t *testing.T) {
	t.Parallel()

	plan := validGatePlan()
	riskAssessmentID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	riskResponse, gateResponse := existingSelfDeployGateResponses(plan, riskAssessmentID, "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	gateResponse.GateRequests[0].Status = governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED
	client := &fakeSelfDeployGateClient{
		err:          status.Error(codes.Unavailable, "prepare command replay failed"),
		riskResponse: riskResponse,
		gateResponse: gateResponse,
	}
	preparer, err := newSelfDeployGatePreparer(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployGatePreparer(): %v", err)
	}

	_, err = preparer.PrepareSelfDeployPlanGate(context.Background(), agentservice.SelfDeployPlanGatePreparationInput{
		Meta: value.CommandMeta{IdempotencyKey: "gate", Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Plan: plan,
	})
	if code := agentservice.SelfDeployGateRecoveryErrorCode(err); code != agentservice.SelfDeployGateRecoveryCodeExistingGateMismatch {
		t.Fatalf("recovery code = %q, want %q", code, agentservice.SelfDeployGateRecoveryCodeExistingGateMismatch)
	}
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("PrepareSelfDeployPlanGate() err = %v, want safe non-match error", err)
	}
	if client.gateRequest.GetRiskAssessmentId() != riskAssessmentID || client.gateRequest.GetTarget().GetRef() != "" {
		t.Fatalf("gate lookup request = %+v, want assessment-only lookup", client.gateRequest)
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
	request                *governancev1.PrepareSelfDeployPlanGateRequest
	riskRequest            *governancev1.ListRiskAssessmentsRequest
	riskRequests           []*governancev1.ListRiskAssessmentsRequest
	gateRequest            *governancev1.ListGateRequestsRequest
	response               *governancev1.SelfDeployPlanGateResponse
	riskResponse           *governancev1.ListRiskAssessmentsResponse
	gateResponse           *governancev1.ListGateRequestsResponse
	riskResponseForRequest func(*governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error)
	err                    error
	riskErr                error
	gateErr                error
	rejectGateTarget       bool
}

func (f *fakeSelfDeployGateClient) PrepareSelfDeployPlanGate(_ context.Context, request *governancev1.PrepareSelfDeployPlanGateRequest, _ ...grpc.CallOption) (*governancev1.SelfDeployPlanGateResponse, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakeSelfDeployGateClient) ListRiskAssessments(_ context.Context, request *governancev1.ListRiskAssessmentsRequest, _ ...grpc.CallOption) (*governancev1.ListRiskAssessmentsResponse, error) {
	f.riskRequest = request
	f.riskRequests = append(f.riskRequests, request)
	if f.riskResponseForRequest != nil {
		return f.riskResponseForRequest(request)
	}
	if f.riskErr != nil {
		return nil, f.riskErr
	}
	return f.riskResponse, nil
}

func (f *fakeSelfDeployGateClient) ListGateRequests(_ context.Context, request *governancev1.ListGateRequestsRequest, _ ...grpc.CallOption) (*governancev1.ListGateRequestsResponse, error) {
	f.gateRequest = request
	if f.rejectGateTarget && request.GetTarget().GetRef() != "" {
		return nil, status.Error(codes.PermissionDenied, "target read denied")
	}
	if f.gateErr != nil {
		return nil, f.gateErr
	}
	return f.gateResponse, nil
}

func existingSelfDeployGateResponses(plan entity.SelfDeployPlan, riskAssessmentID string, gateRequestID string) (*governancev1.ListRiskAssessmentsResponse, *governancev1.ListGateRequestsResponse) {
	planRef := "agent:self-deploy-plan:" + plan.ID.String()
	riskResponse := &governancev1.ListRiskAssessmentsResponse{RiskAssessments: []*governancev1.RiskAssessment{{
		Id: riskAssessmentID,
		Target: &governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN,
			Ref:  planRef,
		},
		Status: governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE,
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:   governancev1.EvidenceKind_EVIDENCE_KIND_SELF_DEPLOY_PLAN,
			Ref:    planRef,
			Digest: ptrString(plan.PlanFingerprint),
		}},
		Explanation: "owner approval required",
	}}}
	gateResponse := &governancev1.ListGateRequestsResponse{GateRequests: []*governancev1.GateRequest{{
		Id:               gateRequestID,
		RiskAssessmentId: ptrString(riskAssessmentID),
		Target: &governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN,
			Ref:  planRef,
		},
		Status: governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
	}}}
	return riskResponse, gateResponse
}

func ptrString(value string) *string {
	return &value
}
