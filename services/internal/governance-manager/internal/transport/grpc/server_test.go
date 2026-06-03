package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterGovernanceManagerService(t *testing.T) {
	t.Parallel()

	server := grpcruntime.NewServer()
	RegisterGovernanceManagerService(server, &fakeBacklogService{})
}

func TestNewServerRequiresService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer(nil) did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestEnumCastersUseProtoDescriptors(t *testing.T) {
	t.Parallel()

	if got := riskClass(governancev1.RiskClass_RISK_CLASS_R0); got != enum.RiskClassR0 {
		t.Fatalf("riskClass(R0) = %q", got)
	}
	if got := toRiskRuleKind(enum.RiskRuleKindAPI); got != governancev1.RiskRuleKind_RISK_RULE_KIND_API {
		t.Fatalf("toRiskRuleKind(api) = %s", got)
	}
	if got := reviewRoleKind(governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_SRE); got != enum.ReviewRoleKindSRE {
		t.Fatalf("reviewRoleKind(SRE) = %q", got)
	}
	if got := toReleaseSafetyStateKind(enum.ReleaseSafetyStateKindFollowUpRequired); got != governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_FOLLOW_UP_REQUIRED {
		t.Fatalf("toReleaseSafetyStateKind(follow_up_required) = %s", got)
	}
	if got := blockingSignalSourceType(governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_REVIEW_SIGNAL); got != enum.BlockingSignalSourceTypeReviewSignal {
		t.Fatalf("blockingSignalSourceType(review_signal) = %q", got)
	}
}

func TestReevaluateRiskRoutesSafeSummaryToDomainService(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	_, err := NewServer(service).ReevaluateRisk(context.Background(), &governancev1.ReevaluateRiskRequest{
		RiskAssessmentId: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
		EvaluationSummary: &governancev1.RiskEvaluationSummary{
			Summary: "bounded release summary",
			Factors: []*governancev1.RiskEvaluationFactor{{
				SourceType: governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE,
				Ref:        "release:stable",
				Summary:    "release policy changed",
				Tags:       []string{"production"},
			}},
		},
		Meta: &governancev1.CommandMeta{Actor: &governancev1.Actor{Type: "service", Id: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("ReevaluateRisk(): %v", err)
	}
	if service.reevaluateRiskInput.EvaluationSummary.Summary != "bounded release summary" {
		t.Fatalf("summary = %q, want routed summary", service.reevaluateRiskInput.EvaluationSummary.Summary)
	}
	if len(service.reevaluateRiskInput.EvaluationSummary.Factors) != 1 || service.reevaluateRiskInput.EvaluationSummary.Factors[0].SourceType != string(enum.RiskFactorSourceTypeRelease) {
		t.Fatalf("factors = %+v, want one release factor", service.reevaluateRiskInput.EvaluationSummary.Factors)
	}
}

func TestGetRiskAssessmentIncludesFactorsAndReviewSignals(t *testing.T) {
	t.Parallel()

	assessmentID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	service := &fakeBacklogService{}
	response, err := NewServer(service).GetRiskAssessment(context.Background(), &governancev1.GetRiskAssessmentRequest{
		RiskAssessmentId:     assessmentID,
		IncludeFactors:       true,
		IncludeReviewSignals: true,
		Meta:                 &governancev1.QueryMeta{Actor: &governancev1.Actor{Type: "service", Id: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("GetRiskAssessment(): %v", err)
	}
	if response.GetRiskAssessment().GetId() != assessmentID {
		t.Fatalf("risk assessment id = %q, want %q", response.GetRiskAssessment().GetId(), assessmentID)
	}
	if len(response.GetRiskFactors()) != 1 {
		t.Fatalf("risk factors = %d, want 1", len(response.GetRiskFactors()))
	}
	if len(response.GetReviewSignals()) != 1 {
		t.Fatalf("review signals = %d, want 1", len(response.GetReviewSignals()))
	}
	if service.riskFactorsInput.Filter.RiskAssessmentID != service.riskAssessmentID {
		t.Fatalf("risk factor filter id = %s, want %s", service.riskFactorsInput.Filter.RiskAssessmentID, service.riskAssessmentID)
	}
	if service.reviewSignalsInput.Filter.RiskAssessmentID == nil || *service.reviewSignalsInput.Filter.RiskAssessmentID != service.riskAssessmentID {
		t.Fatalf("review signal filter id = %v, want %s", service.reviewSignalsInput.Filter.RiskAssessmentID, service.riskAssessmentID)
	}
	if service.riskFactorsInput.Meta.Actor.ID != "provider-hub" || service.reviewSignalsInput.Meta.Actor.ID != "provider-hub" {
		t.Fatalf("meta was not propagated to include queries")
	}
}

func TestBuildReleaseDecisionPackageRoutesIntegrationRefsToDomainService(t *testing.T) {
	t.Parallel()

	packageID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	service := &fakeBacklogService{}
	response, err := NewServer(service).BuildReleaseDecisionPackage(context.Background(), &governancev1.BuildReleaseDecisionPackageRequest{
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      &governancev1.ProjectContextRef{ProjectRef: ptrString("project:alpha")},
		IntegrationRefs: []*governancev1.ReleaseIntegrationRef{{
			Domain:     "provider",
			Kind:       "pull_request",
			Ref:        "provider:pr:1",
			Status:     ptrString("checks_passed"),
			Summary:    ptrString("bounded check summary"),
			Digest:     ptrString("sha256:release-pr"),
			ObservedAt: ptrString("2026-05-27T11:00:00Z"),
			Version:    ptrString("provider-version:1"),
			ErrorCode:  ptrString("CHECK_WARNED"),
		}},
		Meta: &governancev1.CommandMeta{
			Actor:     &governancev1.Actor{Type: "service", Id: "agent-manager"},
			CommandId: ptrString("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"),
		},
	})
	if err != nil {
		t.Fatalf("BuildReleaseDecisionPackage(): %v", err)
	}
	if service.buildReleaseDecisionPackageInput.ReleaseCandidateRef != "release:v1.0.0" || len(service.buildReleaseDecisionPackageInput.IntegrationRefs) != 1 {
		t.Fatalf("input = %+v, want routed release package with integration ref", service.buildReleaseDecisionPackageInput)
	}
	if response.GetReleaseDecisionPackage().GetId() != packageID || len(response.GetReleaseDecisionPackage().GetIntegrationRefs()) != 1 {
		t.Fatalf("response = %+v, want package %s with one integration ref", response.GetReleaseDecisionPackage(), packageID)
	}
	if response.GetReleaseDecisionPackage().GetIntegrationRefs()[0].GetErrorCode() != "CHECK_WARNED" {
		t.Fatalf("error code was not round-tripped")
	}
}

func TestRecordReleaseRuntimeEvidenceRoutesToDomainService(t *testing.T) {
	t.Parallel()

	packageID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	expectedVersion := int64(7)
	service := &fakeBacklogService{}
	response, err := NewServer(service).RecordReleaseRuntimeEvidence(context.Background(), &governancev1.RecordReleaseRuntimeEvidenceRequest{
		ReleaseDecisionPackageId: packageID,
		RuntimeRefs: []*governancev1.RuntimeContextRef{{
			JobRef:     ptrString("runtime:job:deploy"),
			SummaryRef: ptrString("runtime:summary:deploy"),
		}},
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:           governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY,
			Ref:            "runtime:job:deploy",
			Summary:        "deploy status",
			Digest:         ptrString("sha256:deploy"),
			RetentionClass: ptrString("safe_ref"),
		}},
		IntegrationRefs: []*governancev1.ReleaseIntegrationRef{{
			Domain:    "runtime",
			Kind:      "deploy",
			Ref:       "runtime:job:deploy",
			Status:    ptrString("failed"),
			Summary:   ptrString("deploy failed with bounded diagnostic"),
			Digest:    ptrString("sha256:deploy"),
			Version:   ptrString("job-version:7"),
			ErrorCode: ptrString("DEPLOY_HEALTHCHECK_FAILED"),
		}},
		Meta: &governancev1.CommandMeta{
			Actor:           &governancev1.Actor{Type: "service", Id: "runtime-manager"},
			CommandId:       ptrString("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"),
			ExpectedVersion: &expectedVersion,
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseRuntimeEvidence(): %v", err)
	}
	if service.recordReleaseRuntimeEvidenceInput.ReleaseDecisionPackageID.String() != packageID || len(service.recordReleaseRuntimeEvidenceInput.IntegrationRefs) != 1 {
		t.Fatalf("input = %+v, want routed runtime evidence", service.recordReleaseRuntimeEvidenceInput)
	}
	if service.recordReleaseRuntimeEvidenceInput.IntegrationRefs[0].ErrorCode != "DEPLOY_HEALTHCHECK_FAILED" {
		t.Fatalf("input error code = %q, want bounded runtime error code", service.recordReleaseRuntimeEvidenceInput.IntegrationRefs[0].ErrorCode)
	}
	if response.GetReleaseDecisionPackage().GetIntegrationRefs()[0].GetErrorCode() != "DEPLOY_HEALTHCHECK_FAILED" {
		t.Fatalf("response = %+v, want runtime error code", response.GetReleaseDecisionPackage())
	}
}

func TestRecordReleaseAgentEvidenceRoutesToDomainService(t *testing.T) {
	t.Parallel()

	packageID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	expectedVersion := int64(7)
	service := &fakeBacklogService{}
	response, err := NewServer(service).RecordReleaseAgentEvidence(context.Background(), &governancev1.RecordReleaseAgentEvidenceRequest{
		ReleaseDecisionPackageId: packageID,
		AgentContext: &governancev1.AgentContextRef{
			SessionRef:    ptrString("agent:session:1"),
			RunRef:        ptrString("agent:run:reviewer"),
			AcceptanceRef: ptrString("agent:acceptance:qa"),
		},
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:           governancev1.EvidenceKind_EVIDENCE_KIND_AGENT_ACCEPTANCE,
			Ref:            "agent:acceptance:qa",
			Summary:        "acceptance passed",
			Digest:         ptrString("sha256:acceptance"),
			RetentionClass: ptrString("safe_ref"),
		}},
		IntegrationRefs: []*governancev1.ReleaseIntegrationRef{{
			Domain:     "agent",
			Kind:       "acceptance",
			Ref:        "agent:acceptance:qa",
			Status:     ptrString("passed"),
			Summary:    ptrString("acceptance passed"),
			Digest:     ptrString("sha256:acceptance"),
			ObservedAt: ptrString("2026-05-28T11:00:00Z"),
			Version:    ptrString("acceptance-version:7"),
		}},
		Meta: &governancev1.CommandMeta{
			Actor:           &governancev1.Actor{Type: "service", Id: "agent-manager"},
			CommandId:       ptrString("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"),
			ExpectedVersion: &expectedVersion,
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseAgentEvidence(): %v", err)
	}
	if service.recordReleaseAgentEvidenceInput.ReleaseDecisionPackageID.String() != packageID || len(service.recordReleaseAgentEvidenceInput.IntegrationRefs) != 1 {
		t.Fatalf("input = %+v, want routed agent evidence", service.recordReleaseAgentEvidenceInput)
	}
	if service.recordReleaseAgentEvidenceInput.IntegrationRefs[0].Status != "passed" {
		t.Fatalf("input status = %q, want passed", service.recordReleaseAgentEvidenceInput.IntegrationRefs[0].Status)
	}
	if response.GetReleaseDecisionPackage().GetAgentContext().GetAcceptanceRef() != "agent:acceptance:qa" {
		t.Fatalf("response = %+v, want agent acceptance ref", response.GetReleaseDecisionPackage())
	}
}

func TestGetReleaseDecisionPackageReturnsRuntimeEvidenceReadSurface(t *testing.T) {
	t.Parallel()

	packageID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	service := &fakeBacklogService{
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: uuid.MustParse(packageID), Version: 5},
			ReleaseCandidateRef: "release:v1.0.0",
			RuntimeRefs:         []byte(`[{"jobRef":"runtime:job:deploy","summaryRef":"runtime:summary:deploy"}]`),
			EvidenceRefs: []value.EvidenceRef{{
				Kind:           "runtime_job",
				Ref:            "runtime:job:deploy",
				Summary:        "deploy status summary",
				Digest:         "sha256:deploy",
				RetentionClass: "safe_ref",
			}},
			IntegrationRefs: []value.ReleaseIntegrationRef{{
				Domain:     "runtime",
				Kind:       "deploy",
				Ref:        "runtime:job:deploy",
				Status:     "failed",
				Summary:    "deploy failed with bounded diagnostic",
				Digest:     "sha256:deploy",
				ObservedAt: "2026-05-28T11:59:00Z",
				Version:    "job-version:5",
				ErrorCode:  "DEPLOY_HEALTHCHECK_FAILED",
			}},
			Status: enum.ReleaseDecisionPackageStatusReady,
		},
	}

	response, err := NewServer(service).GetReleaseDecisionPackage(context.Background(), &governancev1.GetReleaseDecisionPackageRequest{
		ReleaseDecisionPackageId: packageID,
		Meta:                     &governancev1.QueryMeta{Actor: &governancev1.Actor{Type: "service", Id: "staff-gateway"}},
	})
	if err != nil {
		t.Fatalf("GetReleaseDecisionPackage(): %v", err)
	}
	item := response.GetReleaseDecisionPackage()
	if item.GetReleaseCandidateRef() != "release:v1.0.0" || item.GetVersion() != 5 || len(item.GetRuntimeRefs()) != 1 || len(item.GetEvidenceRefs()) != 1 || len(item.GetIntegrationRefs()) != 1 {
		t.Fatalf("response = %+v, want release package with runtime evidence read surface", item)
	}
	if item.GetRuntimeRefs()[0].GetJobRef() != "runtime:job:deploy" || item.GetIntegrationRefs()[0].GetErrorCode() != "DEPLOY_HEALTHCHECK_FAILED" {
		t.Fatalf("response = %+v, want runtime job ref and bounded error code", item)
	}
	readSurface := item.GetRuntimeRefs()[0].GetJobRef() + " " + item.GetEvidenceRefs()[0].GetSummary() + " " + item.GetIntegrationRefs()[0].GetSummary()
	for _, unsafe := range []string{"token=", "kubeconfig", "stdout", "stderr", "raw_provider_payload"} {
		if strings.Contains(readSurface, unsafe) {
			t.Fatalf("read surface leaked unsafe marker %q: %s", unsafe, readSurface)
		}
	}
}

func TestGetReleaseDecisionPackageReturnsAgentEvidenceReadSurface(t *testing.T) {
	t.Parallel()

	packageID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	service := &fakeBacklogService{
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: uuid.MustParse(packageID), Version: 5},
			ReleaseCandidateRef: "release:v1.0.0",
			AgentContext:        []byte(`{"acceptanceRef":"agent:acceptance:qa","runRef":"agent:run:reviewer","sessionRef":"agent:session:1"}`),
			EvidenceRefs: []value.EvidenceRef{{
				Kind:           "agent_acceptance",
				Ref:            "agent:acceptance:qa",
				Summary:        "acceptance passed",
				Digest:         "sha256:acceptance",
				RetentionClass: "safe_ref",
			}},
			IntegrationRefs: []value.ReleaseIntegrationRef{{
				Domain:     "agent",
				Kind:       "acceptance",
				Ref:        "agent:acceptance:qa",
				Status:     "passed",
				Summary:    "acceptance passed",
				Digest:     "sha256:acceptance",
				ObservedAt: "2026-05-28T11:59:00Z",
				Version:    "acceptance-version:5",
			}},
			Status: enum.ReleaseDecisionPackageStatusReady,
		},
	}

	response, err := NewServer(service).GetReleaseDecisionPackage(context.Background(), &governancev1.GetReleaseDecisionPackageRequest{
		ReleaseDecisionPackageId: packageID,
		Meta:                     &governancev1.QueryMeta{Actor: &governancev1.Actor{Type: "service", Id: "staff-gateway"}},
	})
	if err != nil {
		t.Fatalf("GetReleaseDecisionPackage(): %v", err)
	}
	item := response.GetReleaseDecisionPackage()
	if item.GetAgentContext().GetAcceptanceRef() != "agent:acceptance:qa" || item.GetIntegrationRefs()[0].GetStatus() != "passed" {
		t.Fatalf("response = %+v, want agent evidence read surface", item)
	}
	readSurface := item.GetAgentContext().GetAcceptanceRef() + " " + item.GetEvidenceRefs()[0].GetSummary() + " " + item.GetIntegrationRefs()[0].GetSummary()
	for _, unsafe := range []string{"token=", "kubeconfig", "stdout", "stderr", "transcript", "workspace"} {
		if strings.Contains(readSurface, unsafe) {
			t.Fatalf("read surface leaked unsafe marker %q: %s", unsafe, readSurface)
		}
	}
}

func TestGetGovernanceSummaryRoutesToDomainService(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	service := &fakeBacklogService{}
	response, err := NewServer(service).GetGovernanceSummary(context.Background(), &governancev1.GetGovernanceSummaryRequest{
		Scope: &governancev1.GovernanceSummaryScope{
			ProjectContext:           &governancev1.ProjectContextRef{ProjectRef: ptrString("project:alpha")},
			ReleaseCandidateRef:      ptrString("release:v1.2.3"),
			ReleaseDecisionPackageId: ptrString(packageID.String()),
			IntegrationRef: &governancev1.ReleaseIntegrationRef{
				Domain: "agent",
				Kind:   "run",
				Ref:    "agent:run:42",
			},
		},
		Meta: &governancev1.QueryMeta{Actor: &governancev1.Actor{Type: "user", Id: "owner"}},
	})
	if err != nil {
		t.Fatalf("GetGovernanceSummary(): %v", err)
	}
	if service.getGovernanceSummaryInput.Scope.ReleaseDecisionPackageID == nil || *service.getGovernanceSummaryInput.Scope.ReleaseDecisionPackageID != packageID {
		t.Fatalf("scope package id = %v, want %s", service.getGovernanceSummaryInput.Scope.ReleaseDecisionPackageID, packageID)
	}
	if service.getGovernanceSummaryInput.Scope.IntegrationRef.Domain != "agent" || service.getGovernanceSummaryInput.Scope.IntegrationRef.Ref != "agent:run:42" {
		t.Fatalf("integration ref = %+v, want agent run", service.getGovernanceSummaryInput.Scope.IntegrationRef)
	}
	if len(response.GetSummary().GetPendingDecisions()) != 1 ||
		response.GetSummary().GetPendingDecisions()[0].GetKind() != governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_RELEASE_DECISION_PACKAGE ||
		response.GetSummary().GetPendingDecisions()[0].GetReleaseDecisionPackageId() != packageID.String() {
		t.Fatalf("summary response = %+v, want release package pending decision", response.GetSummary())
	}
	if len(response.GetSummary().GetEvidenceSummaries()) != 1 || response.GetSummary().GetEvidenceSummaries()[0].GetSourceKind() != "agent.run" {
		t.Fatalf("evidence summaries = %+v, want agent.run evidence", response.GetSummary().GetEvidenceSummaries())
	}
}

func TestPrepareSelfDeployPlanGateRoutesToDomainService(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	response, err := NewServer(service).PrepareSelfDeployPlanGate(context.Background(), &governancev1.PrepareSelfDeployPlanGateRequest{
		Plan: &governancev1.SelfDeployPlanGateInput{
			SelfDeployPlanRef:       "agent:self-deploy-plan:1",
			ProjectContext:          &governancev1.ProjectContextRef{ProjectRef: ptrString("project:kodex"), RepositoryRef: ptrString("repo:codex-k8s/kodex")},
			ProviderSignalRef:       ptrString("provider:signal:merge-main"),
			SourceRef:               ptrString("github:pull:1008"),
			ServicesYamlDigest:      ptrString("sha256:services"),
			AffectedServiceKeys:     []string{"governance-manager"},
			PathCategories:          []string{"services_yaml"},
			ExpectedRuntimeJobTypes: []string{"build", "deploy"},
			ChangedFilesSummaryRef:  ptrString("provider:changed-files:1008"),
			SafeSummary:             ptrString("bounded self-deploy plan"),
			PlanFingerprint:         "sha256:plan",
		},
		Meta: &governancev1.CommandMeta{
			Actor:          &governancev1.Actor{Type: "service", Id: "agent-manager"},
			IdempotencyKey: ptrString("agent:self-deploy-plan:1"),
		},
	})
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if service.selfDeployPlanGateInput.SelfDeployPlanRef != "agent:self-deploy-plan:1" ||
		service.selfDeployPlanGateInput.PlanFingerprint != "sha256:plan" ||
		service.selfDeployPlanGateInput.ExpectedRuntimeJobTypes[1] != "deploy" {
		t.Fatalf("input = %+v, want safe self-deploy plan refs", service.selfDeployPlanGateInput)
	}
	if response.GetStatus() != governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_PENDING ||
		response.GetRiskAssessment().GetEffectiveRiskClass() != governancev1.RiskClass_RISK_CLASS_R2 ||
		response.GetGateRequest().GetStatus() != governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION {
		t.Fatalf("response = %+v, want pending R2 gate", response)
	}
	if len(response.GetRiskAssessment().GetEvidenceRefs()) != 1 ||
		response.GetRiskAssessment().GetEvidenceRefs()[0].GetKind() != governancev1.EvidenceKind_EVIDENCE_KIND_SELF_DEPLOY_PLAN {
		t.Fatalf("risk assessment evidence refs = %+v, want typed self_deploy_plan evidence", response.GetRiskAssessment().GetEvidenceRefs())
	}
	rendered := response.GetGovernanceSummary().GetPendingDecisions()[0].GetSafeSummary()
	for _, unsafe := range []string{"raw_diff", "webhook body", "secret=", "kubeconfig", "stdout", "stderr"} {
		if strings.Contains(rendered, unsafe) {
			t.Fatalf("summary leaked unsafe marker %q: %s", unsafe, rendered)
		}
	}
}

func TestRequestReleaseDecisionRoutesToDomainService(t *testing.T) {
	t.Parallel()

	packageID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	service := &fakeBacklogService{}
	response, err := NewServer(service).RequestReleaseDecision(context.Background(), &governancev1.RequestReleaseDecisionRequest{
		ReleaseDecisionPackageId: packageID,
		RequestGateIfRequired:    true,
		Meta: &governancev1.CommandMeta{
			Actor:     &governancev1.Actor{Type: "service", Id: "agent-manager"},
			CommandId: ptrString("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"),
		},
	})
	if err != nil {
		t.Fatalf("RequestReleaseDecision(): %v", err)
	}
	if service.requestReleaseDecisionInput.ReleaseDecisionPackageID.String() != packageID || !service.requestReleaseDecisionInput.RequestGateIfRequired {
		t.Fatalf("input = %+v, want package %s and gate flag", service.requestReleaseDecisionInput, packageID)
	}
	if response.GetReleaseDecision().GetReleaseDecisionPackageId() != packageID {
		t.Fatalf("response package id = %q, want %q", response.GetReleaseDecision().GetReleaseDecisionPackageId(), packageID)
	}
}

func TestUnaryErrorInterceptorMapsBacklogToUnimplemented(t *testing.T) {
	t.Parallel()

	interceptor := UnaryErrorInterceptor(nil)
	_, err := interceptor(context.Background(), nil, &grpcruntime.UnaryServerInfo{FullMethod: "/test"}, func(context.Context, any) (any, error) {
		return nil, errs.ErrNotImplemented
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("status code = %s, want %s", status.Code(err), codes.Unimplemented)
	}
}

func TestUnaryErrorInterceptorMapsRepositoryDomainErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{name: "not found", err: errs.ErrNotFound, want: codes.NotFound},
		{name: "already exists", err: errs.ErrAlreadyExists, want: codes.AlreadyExists},
		{name: "conflict", err: errs.ErrConflict, want: codes.Aborted},
		{name: "forbidden", err: errs.ErrForbidden, want: codes.PermissionDenied},
		{name: "precondition failed", err: errs.ErrPreconditionFailed, want: codes.FailedPrecondition},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interceptor := UnaryErrorInterceptor(nil)
			_, err := interceptor(context.Background(), nil, &grpcruntime.UnaryServerInfo{FullMethod: "/test"}, func(context.Context, any) (any, error) {
				return nil, tt.err
			})
			if status.Code(err) != tt.want {
				t.Fatalf("status code = %s, want %s", status.Code(err), tt.want)
			}
		})
	}
}

type fakeBacklogService struct {
	governanceService
	operation                         enum.Operation
	reevaluateRiskInput               governanceservice.ReevaluateRiskInput
	riskAssessmentID                  uuid.UUID
	riskFactorsInput                  governanceservice.ListRiskFactorsInput
	reviewSignalsInput                governanceservice.ListReviewSignalsInput
	buildReleaseDecisionPackageInput  governanceservice.BuildReleaseDecisionPackageInput
	recordReleaseRuntimeEvidenceInput governanceservice.RecordReleaseRuntimeEvidenceInput
	recordReleaseAgentEvidenceInput   governanceservice.RecordReleaseAgentEvidenceInput
	getReleaseDecisionPackageInput    governanceservice.GetReleaseDecisionPackageInput
	listReleaseDecisionPackagesInput  governanceservice.ListReleaseDecisionPackagesInput
	releasePackage                    entity.ReleaseDecisionPackage
	getGovernanceSummaryInput         governanceservice.GetGovernanceSummaryInput
	selfDeployPlanGateInput           governanceservice.SelfDeployPlanGateInput
	requestReleaseDecisionInput       governanceservice.RequestReleaseDecisionInput
}

func (service *fakeBacklogService) BacklogOperation(_ context.Context, input governanceservice.BacklogOperationInput) error {
	service.operation = input.Operation
	return errs.ErrNotImplemented
}

func (service *fakeBacklogService) ReevaluateRisk(_ context.Context, input governanceservice.ReevaluateRiskInput) (entity.RiskAssessment, error) {
	service.reevaluateRiskInput = input
	return entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: input.RiskAssessmentID}}, nil
}

func (service *fakeBacklogService) GetRiskAssessment(_ context.Context, input governanceservice.GetRiskAssessmentInput) (entity.RiskAssessment, error) {
	service.riskAssessmentID = input.RiskAssessmentID
	return entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: input.RiskAssessmentID}}, nil
}

func (service *fakeBacklogService) ListRiskFactors(_ context.Context, input governanceservice.ListRiskFactorsInput) ([]entity.RiskFactor, query.PageResult, error) {
	service.riskFactorsInput = input
	return []entity.RiskFactor{{
		ID:               uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"),
		RiskAssessmentID: input.Filter.RiskAssessmentID,
		SourceType:       enum.RiskFactorSourceTypeDatabase,
		RiskClass:        enum.RiskClassR2,
		Summary:          "migration risk",
	}}, query.PageResult{}, nil
}

func (service *fakeBacklogService) ListReviewSignals(_ context.Context, input governanceservice.ListReviewSignalsInput) ([]entity.ReviewSignal, query.PageResult, error) {
	service.reviewSignalsInput = input
	riskAssessmentID := input.Filter.RiskAssessmentID
	return []entity.ReviewSignal{{
		ID:               uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
		RiskAssessmentID: riskAssessmentID,
		RoleKind:         enum.ReviewRoleKindSecurity,
		Outcome:          enum.ReviewSignalOutcomePass,
		Summary:          "approved",
	}}, query.PageResult{}, nil
}

func (service *fakeBacklogService) BuildReleaseDecisionPackage(_ context.Context, input governanceservice.BuildReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error) {
	service.buildReleaseDecisionPackageInput = input
	return entity.ReleaseDecisionPackage{
		VersionedBase:       entity.VersionedBase{ID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"), Version: 1},
		ReleaseCandidateRef: input.ReleaseCandidateRef,
		ProjectContext:      input.ProjectContext,
		IntegrationRefs:     input.IntegrationRefs,
		Status:              enum.ReleaseDecisionPackageStatusReady,
	}, nil
}

func (service *fakeBacklogService) RecordReleaseRuntimeEvidence(_ context.Context, input governanceservice.RecordReleaseRuntimeEvidenceInput) (entity.ReleaseDecisionPackage, error) {
	service.recordReleaseRuntimeEvidenceInput = input
	return entity.ReleaseDecisionPackage{
		VersionedBase:       entity.VersionedBase{ID: input.ReleaseDecisionPackageID, Version: 2},
		ReleaseCandidateRef: "release:v1.0.0",
		RuntimeRefs:         input.RuntimeRefs,
		EvidenceRefs:        input.EvidenceRefs,
		IntegrationRefs:     input.IntegrationRefs,
		Status:              enum.ReleaseDecisionPackageStatusReady,
	}, nil
}

func (service *fakeBacklogService) RecordReleaseAgentEvidence(_ context.Context, input governanceservice.RecordReleaseAgentEvidenceInput) (entity.ReleaseDecisionPackage, error) {
	service.recordReleaseAgentEvidenceInput = input
	return entity.ReleaseDecisionPackage{
		VersionedBase:       entity.VersionedBase{ID: input.ReleaseDecisionPackageID, Version: 2},
		ReleaseCandidateRef: "release:v1.0.0",
		AgentContext:        input.AgentContext,
		EvidenceRefs:        input.EvidenceRefs,
		IntegrationRefs:     input.IntegrationRefs,
		Status:              enum.ReleaseDecisionPackageStatusReady,
	}, nil
}

func (service *fakeBacklogService) GetReleaseDecisionPackage(_ context.Context, input governanceservice.GetReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error) {
	service.getReleaseDecisionPackageInput = input
	return service.releasePackage, nil
}

func (service *fakeBacklogService) ListReleaseDecisionPackages(_ context.Context, input governanceservice.ListReleaseDecisionPackagesInput) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	service.listReleaseDecisionPackagesInput = input
	return []entity.ReleaseDecisionPackage{service.releasePackage}, query.PageResult{}, nil
}

func (service *fakeBacklogService) GetGovernanceSummary(_ context.Context, input governanceservice.GetGovernanceSummaryInput) (entity.GovernanceSummary, error) {
	service.getGovernanceSummaryInput = input
	packageID := uuid.Nil
	if input.Scope.ReleaseDecisionPackageID != nil {
		packageID = *input.Scope.ReleaseDecisionPackageID
	}
	return entity.GovernanceSummary{
		Scope: input.Scope,
		PendingDecisions: []entity.GovernanceDecisionSummary{{
			Kind:                     enum.GovernanceDecisionSummaryKindReleaseDecisionPackage,
			Attention:                enum.GovernanceDecisionAttentionPending,
			ID:                       packageID.String(),
			ReleaseDecisionPackageID: packageID.String(),
		}},
		EvidenceSummaries: []entity.GovernanceEvidenceSummary{{
			SourceKind:      "agent.run",
			SourceRef:       input.Scope.IntegrationRef.Ref,
			IntegrationRefs: []value.ReleaseIntegrationRef{input.Scope.IntegrationRef},
		}},
	}, nil
}

func (service *fakeBacklogService) PrepareSelfDeployPlanGate(_ context.Context, input governanceservice.SelfDeployPlanGateInput) (governanceservice.SelfDeployPlanGateResult, error) {
	service.selfDeployPlanGateInput = input
	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: input.SelfDeployPlanRef}
	return governanceservice.SelfDeployPlanGateResult{
		Status: enum.SelfDeployPlanGateStatusPending,
		RiskAssessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			Target:             target,
			ProjectContext:     input.ProjectContext,
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
			EvidenceRefs:       []value.EvidenceRef{{Kind: "self_deploy_plan", Ref: input.SelfDeployPlanRef, Summary: "self-deploy plan gate input", Digest: input.PlanFingerprint}},
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy gate required"}},
		},
		GateRequest: entity.GateRequest{
			VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 1},
			RiskAssessmentID: &assessmentID,
			Target:           target,
			EvidenceSummary:  "bounded self-deploy plan",
			Status:           enum.GateRequestStatusAwaitingDecision,
		},
		GovernanceSummary: entity.GovernanceSummary{
			Scope: entity.GovernanceSummaryScope{Target: target},
			PendingDecisions: []entity.GovernanceDecisionSummary{{
				Kind:        enum.GovernanceDecisionSummaryKindGateRequest,
				Attention:   enum.GovernanceDecisionAttentionPending,
				ID:          gateRequestID.String(),
				Target:      target,
				SafeSummary: "bounded self-deploy plan",
			}},
		},
	}, nil
}

func (service *fakeBacklogService) RequestReleaseDecision(_ context.Context, input governanceservice.RequestReleaseDecisionInput) (entity.ReleaseDecision, entity.ReleaseDecisionPackage, error) {
	service.requestReleaseDecisionInput = input
	return entity.ReleaseDecision{
			VersionedBase:            entity.VersionedBase{ID: uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"), Version: 1},
			ReleaseDecisionPackageID: input.ReleaseDecisionPackageID,
			Status:                   enum.ReleaseDecisionStatusRequested,
		},
		entity.ReleaseDecisionPackage{VersionedBase: entity.VersionedBase{ID: input.ReleaseDecisionPackageID, Version: 2}},
		nil
}
