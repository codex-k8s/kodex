package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	governanceevents "github.com/codex-k8s/kodex/libs/go/platformevents/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

func TestBacklogOperationReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	service := newTestService(&fakeRepository{ready: true})
	err := service.BacklogOperation(context.Background(), BacklogOperationInput{Operation: enum.OperationReevaluateRisk})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("BacklogOperation() error = %v, want ErrNotImplemented", err)
	}
}

func TestReadyRequiresRepository(t *testing.T) {
	t.Parallel()

	if New(nil).Ready() {
		t.Fatal("Ready() = true for missing repository, want false")
	}
	if New(&fakeRepository{ready: true}).Ready() {
		t.Fatal("Ready() = true for missing authorizer, want false")
	}
	if !newTestService(&fakeRepository{ready: true}).Ready() {
		t.Fatal("Ready() = false for ready repository and explicit authorizer, want true")
	}
}

func TestEvaluateRiskStoresAssessmentAndOutboxEvents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	eventOneID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	eventTwoID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{assessmentID, eventOneID, eventTwoID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	assessment, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target:         value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/1"},
		ProjectContext: value.ProjectContextRef{ProjectRef: "project:core"},
		Meta: CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "provider-hub"},
			Reason:    "provider checks changed",
			RequestID: "trace-risk-1",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateRisk(): %v", err)
	}
	if assessment.ID != assessmentID || assessment.EffectiveRiskClass != enum.RiskClassR0 {
		t.Fatalf("assessment = %#v, want id %s and R0", assessment, assessmentID)
	}
	if repository.assessment.ID != assessmentID {
		t.Fatalf("stored assessment id = %s, want %s", repository.assessment.ID, assessmentID)
	}
	if len(repository.events) != 2 {
		t.Fatalf("stored events = %d, want 2", len(repository.events))
	}
	if repository.result.CommandID == nil || *repository.result.CommandID != commandID {
		t.Fatalf("command result command id = %v, want %s", repository.result.CommandID, commandID)
	}
	requestedPayload := string(repository.events[0].Payload)
	for _, want := range []string{
		`"actor_ref":"service:provider-hub"`,
		`"idempotency_key":"command:aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"`,
		`"request_id":"trace-risk-1"`,
		`"target_type":"provider_native.pr"`,
		`"target_ref":"github://codex-k8s/kodex/pull/1"`,
	} {
		if !strings.Contains(requestedPayload, want) {
			t.Fatalf("risk requested payload = %s, want %s", requestedPayload, want)
		}
	}
}

func TestEvaluateRiskClassifiesSafeFactorsAndPolicyRules(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	gatePolicyID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	activeVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		profile: entity.RiskProfile{
			VersionedBase: entity.VersionedBase{ID: profileID, Version: 2},
			Status:        enum.RiskProfileStatusActive,
			ActiveVersion: &activeVersion,
		},
		profileVersion: entity.RiskProfileVersion{
			RiskProfileID:  profileID,
			ProfileVersion: activeVersion,
			Status:         enum.RiskProfileVersionStatusActive,
			GatePolicies: []entity.GatePolicy{{
				ID:           gatePolicyID,
				GateKind:     enum.GateKindRelease,
				MinRiskClass: enum.RiskClassR2,
				Status:       enum.RuleStatusActive,
			}},
			Rules: []entity.RiskRule{{
				ID:                   uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"),
				RiskProfileID:        profileID,
				ProfileVersion:       activeVersion,
				RuleKind:             enum.RiskRuleKindDatabase,
				MatcherJSON:          []byte(`{"path_glob":"services/api/*.sql","factor_tag":"migration"}`),
				MinRiskClass:         enum.RiskClassR2,
				RequiredGatePolicyID: &gatePolicyID,
				ReasonTemplate:       []value.LocalizedText{{Locale: "ru", Text: "DB migration needs release gate"}},
				Status:               enum.RuleStatusActive,
			}},
		},
	}
	service := newTestService(repository)

	assessment, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target: value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
		ProjectContext: value.ProjectContextRef{
			ProjectRef:     "project:core",
			RepositoryRef:  "repo:kodex",
			ReleaseLineRef: "stable",
		},
		EvaluationSummary: value.RiskEvaluationSummary{
			ChangedFilesSummaryRef: "provider-summary:pr-827-files",
			Summary:                "bounded summary",
			Factors: []value.RiskEvaluationFactor{{
				SourceType: string(enum.RiskFactorSourceTypeDatabase),
				Ref:        "services/api/schema.sql",
				Summary:    "schema migration",
				Tags:       []string{"migration"},
			}, {
				SourceType: string(enum.RiskFactorSourceTypeSecret),
				Ref:        "secret-scope:runtime-token",
				Summary:    "secret scope changed",
				Tags:       []string{"auth"},
			}},
		},
		RiskProfileRef: profileID.String(),
		Meta:           CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("EvaluateRisk(): %v", err)
	}
	if assessment.EffectiveRiskClass != enum.RiskClassR3 {
		t.Fatalf("effective risk = %s, want R3", assessment.EffectiveRiskClass)
	}
	if len(assessment.RequiredGates) != 1 || assessment.RequiredGates[0].GatePolicyID != gatePolicyID {
		t.Fatalf("required gates = %+v, want gate policy %s", assessment.RequiredGates, gatePolicyID)
	}
	if assessment.RiskProfileID == nil || *assessment.RiskProfileID != profileID || assessment.RiskProfileVersion == nil || *assessment.RiskProfileVersion != activeVersion {
		t.Fatalf("assessment profile refs = %v/%v, want %s/%d", assessment.RiskProfileID, assessment.RiskProfileVersion, profileID, activeVersion)
	}
	if len(repository.riskFactors) < 3 {
		t.Fatalf("risk factors = %+v, want input and policy factors", repository.riskFactors)
	}
	for _, event := range repository.events {
		payload := string(event.Payload)
		if strings.Contains(payload, "schema.sql") || strings.Contains(payload, "runtime-token") || strings.Contains(payload, "raw_diff") {
			t.Fatalf("outbox payload leaked unsafe evaluation detail: %s", payload)
		}
	}
}

func TestEvaluateRiskClassifiesSelfDeployGatePolicy(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	gatePolicyID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	activeVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		profile: entity.RiskProfile{
			VersionedBase: entity.VersionedBase{ID: profileID, Version: 2},
			Status:        enum.RiskProfileStatusActive,
			ActiveVersion: &activeVersion,
		},
		profileVersion: entity.RiskProfileVersion{
			RiskProfileID:  profileID,
			ProfileVersion: activeVersion,
			Status:         enum.RiskProfileVersionStatusActive,
			GatePolicies: []entity.GatePolicy{{
				ID:           gatePolicyID,
				GateKind:     enum.GateKindRelease,
				MinRiskClass: enum.RiskClassR2,
				Status:       enum.RuleStatusActive,
			}},
		},
	}

	assessment, err := newTestService(repository).EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target: value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"},
		ProjectContext: value.ProjectContextRef{
			ProjectRef:     "project:kodex",
			RepositoryRef:  "repo:codex-k8s/kodex",
			ReleaseLineRef: "self-deploy",
		},
		EvaluationSummary: value.RiskEvaluationSummary{
			ChangedFilesSummaryRef: "provider-summary:self-deploy-plan",
			Summary:                "bounded self-deploy plan summary",
			Factors: []value.RiskEvaluationFactor{{
				SourceType: string(enum.RiskFactorSourceTypeChangedFile),
				Ref:        "path-category:services-yaml",
				Summary:    "project declaration changed",
				Tags:       []string{"services_yaml", "self_deploy"},
			}, {
				SourceType: string(enum.RiskFactorSourceTypeRuntime),
				Ref:        "path-category:deploy-manifest",
				Summary:    "deploy manifest changed",
				Tags:       []string{"deploy_manifest", "runtime_executor"},
			}},
		},
		RiskProfileRef: profileID.String(),
		Meta:           CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("EvaluateRisk(): %v", err)
	}
	if assessment.EffectiveRiskClass != enum.RiskClassR2 {
		t.Fatalf("effective risk = %s, want R2", assessment.EffectiveRiskClass)
	}
	if len(assessment.RequiredGates) != 1 || assessment.RequiredGates[0].GateKind != enum.GateKindRelease {
		t.Fatalf("required gates = %+v, want release gate", assessment.RequiredGates)
	}
}

func TestSelfDeployRiskTagsClassifyBaseline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags []string
		want enum.RiskClass
	}{
		{name: "low risk docs", tags: []string{"documentation", "tests"}, want: enum.RiskClassR1},
		{name: "owner gate services yaml", tags: []string{"services_yaml"}, want: enum.RiskClassR2},
		{name: "owner gate deploy manifest", tags: []string{"deploy_manifest", "rbac"}, want: enum.RiskClassR2},
		{name: "owner gate runtime runner", tags: []string{"runtime_executor", "agent_runner"}, want: enum.RiskClassR2},
		{name: "blocking secret value", tags: []string{"secret_value"}, want: enum.RiskClassR3},
		{name: "blocking kubeconfig", tags: []string{"kubeconfig"}, want: enum.RiskClassR3},
		{name: "blocking auth bypass", tags: []string{"auth_bypass"}, want: enum.RiskClassR3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultRiskForInputFactor(value.RiskEvaluationFactor{
				SourceType: string(enum.RiskFactorSourceTypeChangedFile),
				Ref:        "path-category:self-deploy",
				Summary:    "bounded factor summary",
				Tags:       tt.tags,
			})
			if got != tt.want {
				t.Fatalf("risk = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPrepareSelfDeployPlanGateCreatesAssessmentAndGate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: uuidGenerator{},
		Authorizer:  AllowAllAuthorizer{},
	})

	result, err := service.PrepareSelfDeployPlanGate(context.Background(), selfDeployPlanGateTestInput("sha256:plan"))
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if result.Status != enum.SelfDeployPlanGateStatusPending {
		t.Fatalf("status = %s, want pending", result.Status)
	}
	if result.RiskAssessment.Target.Type != "self_deploy_plan" ||
		result.RiskAssessment.Target.Ref != "agent:self-deploy-plan:1" ||
		result.RiskAssessment.EffectiveRiskClass != enum.RiskClassR2 {
		t.Fatalf("assessment = %+v, want self-deploy R2 target", result.RiskAssessment)
	}
	if len(result.RiskAssessment.RequiredGates) != 1 || result.RiskAssessment.RequiredGates[0].GateKind != enum.GateKindRelease {
		t.Fatalf("required gates = %+v, want release owner gate", result.RiskAssessment.RequiredGates)
	}
	if result.RiskAssessment.RequiredGates[0].GatePolicyID != uuid.Nil {
		t.Fatalf("required gate policy id = %s, want empty built-in self-deploy gate policy", result.RiskAssessment.RequiredGates[0].GatePolicyID)
	}
	if result.GateRequest.ID == uuid.Nil || result.GateRequest.RiskAssessmentID == nil || *result.GateRequest.RiskAssessmentID != result.RiskAssessment.ID {
		t.Fatalf("gate request = %+v, want linked assessment", result.GateRequest)
	}
	if result.GateRequest.GatePolicyID != nil {
		t.Fatalf("gate request policy id = %v, want nil built-in self-deploy gate policy", result.GateRequest.GatePolicyID)
	}
	if result.GateRequest.Status != enum.GateRequestStatusRequested {
		t.Fatalf("gate status = %s, want requested", result.GateRequest.Status)
	}
	if !selfDeployAssessmentMatchesFingerprint(result.RiskAssessment, normalizedSelfDeployPlanGateInput{SelfDeployPlanRef: "agent:self-deploy-plan:1", PlanFingerprint: "sha256:plan"}) {
		t.Fatalf("assessment evidence refs = %+v, want plan fingerprint", result.RiskAssessment.EvidenceRefs)
	}
	if repository.mutationCalls != 2 {
		t.Fatalf("mutation calls = %d, want assessment and gate request", repository.mutationCalls)
	}
	for _, event := range repository.events {
		payload := string(event.Payload)
		for _, unsafe := range []string{"raw_diff", "webhook body", "secret=", "kubeconfig", "stdout", "stderr", "provider response"} {
			if strings.Contains(payload, unsafe) {
				t.Fatalf("outbox payload leaked unsafe marker %q: %s", unsafe, payload)
			}
		}
	}
}

func TestPrepareSelfDeployPlanGateUsesProjectScopedSelfDeployAccess(t *testing.T) {
	t.Parallel()

	var captured []AuthorizationRequest
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)},
		IDGenerator: uuidGenerator{},
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			captured = append(captured, request)
			return nil
		}),
	})

	if _, err := service.PrepareSelfDeployPlanGate(context.Background(), selfDeployPlanGateTestInput("sha256:plan")); err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	for _, request := range captured {
		if request.Subject.Type != "service" || request.Subject.ID != "agent-manager" {
			t.Fatalf("access subject = %+v, want service/agent-manager", request.Subject)
		}
		if request.ScopeType != accesscatalog.ScopeProject || request.ScopeID != "project:kodex" {
			t.Fatalf("access request = %+v, want project scoped self-deploy check", request)
		}
		if request.ResourceID != "agent:self-deploy-plan:1" {
			t.Fatalf("access resource id = %q, want self-deploy plan target", request.ResourceID)
		}
		if request.ActionKey == actionGateDecide {
			t.Fatalf("self-deploy gate preparation must not request decision access: %+v", request)
		}
	}
	assertCapturedGovernanceAccess(t, captured, actionRiskRead, accesscatalog.ResourceGovernanceRiskAssessment)
	assertCapturedGovernanceAccess(t, captured, actionRiskEvaluate, accesscatalog.ResourceGovernanceRiskAssessment)
	assertCapturedGovernanceAccess(t, captured, actionGateRead, accesscatalog.ResourceGovernanceGate)
	assertCapturedGovernanceAccess(t, captured, actionGateRequest, accesscatalog.ResourceGovernanceGate)
}

func TestPrepareSelfDeployPlanGateClassifiesR3SafeFactors(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	input := selfDeployPlanGateTestInput("sha256:r3-plan")
	input.PathCategories = []string{"auth_bypass"}
	input.SafeSummary = "bounded self-deploy security-sensitive plan"

	result, err := newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), input)
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if result.RiskAssessment.EffectiveRiskClass != enum.RiskClassR3 ||
		len(result.RiskAssessment.RequiredGates) != 1 ||
		result.Status != enum.SelfDeployPlanGateStatusPending {
		t.Fatalf("result = %+v, want pending R3 owner/governance gate", result)
	}
	for _, ref := range result.RiskAssessment.EvidenceRefs {
		if strings.Contains(ref.Summary, "secret=") || strings.Contains(ref.Summary, "kubeconfig:") {
			t.Fatalf("evidence leaked unsafe detail: %+v", ref)
		}
	}
}

func TestPrepareSelfDeployPlanGateReusesExistingAssessmentAndGate(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"}
	repository := &fakeRepository{
		ready: true,
		riskAssessments: []entity.RiskAssessment{{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			Target:             target,
			ProjectContext:     value.ProjectContextRef{ProjectRef: "project:kodex", RepositoryRef: "repo:codex-k8s/kodex"},
			EvidenceRefs:       []value.EvidenceRef{{Kind: selfDeployPlanEvidenceKind, Ref: "agent:self-deploy-plan:1", Digest: "sha256:plan", Summary: selfDeployPlanGateEvidenceSummary}},
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy gate required"}},
		}},
		gateRequests: []entity.GateRequest{{
			VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 1},
			RiskAssessmentID: &assessmentID,
			Target:           target,
			Status:           enum.GateRequestStatusAwaitingDecision,
			EvidenceSummary:  "bounded self-deploy plan",
		}},
	}

	result, err := newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), selfDeployPlanGateTestInput("sha256:plan"))
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if result.Status != enum.SelfDeployPlanGateStatusPending || result.GateRequest.ID != gateRequestID {
		t.Fatalf("result = %+v, want existing pending gate request", result)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want idempotent read-only replay", repository.mutationCalls)
	}
}

func TestPrepareSelfDeployPlanGateReplaysCommandResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	input := selfDeployPlanGateTestInput("sha256:plan")
	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"}
	resultPayload := map[string]any{
		"self_deploy_plan_ref": "agent:self-deploy-plan:1",
		"plan_fingerprint":     "sha256:plan",
		"risk_assessment_id":   assessmentID.String(),
		"gate_request_id":      gateRequestID.String(),
		"status":               string(enum.SelfDeployPlanGateStatusPending),
	}
	repository := &fakeRepository{
		ready:            true,
		hasCommandResult: true,
		commandResult:    commandResultWithPayload(input.Meta, enum.OperationPrepareSelfDeployPlanGate.String(), aggregateSelfDeployPlanGate, assessmentID, now, resultPayload),
		riskAssessments: []entity.RiskAssessment{{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			Target:             target,
			EvidenceRefs:       []value.EvidenceRef{{Kind: selfDeployPlanEvidenceKind, Ref: "agent:self-deploy-plan:1", Digest: "sha256:plan", Summary: selfDeployPlanGateEvidenceSummary}},
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy gate required"}},
		}},
		gateRequests: []entity.GateRequest{{
			VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 1},
			RiskAssessmentID: &assessmentID,
			Target:           target,
			Status:           enum.GateRequestStatusAwaitingDecision,
			EvidenceSummary:  "bounded self-deploy plan",
		}},
	}

	result, err := newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), input)
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate() replay: %v", err)
	}
	if result.Status != enum.SelfDeployPlanGateStatusPending || result.GateRequest.ID != gateRequestID || repository.mutationCalls != 0 {
		t.Fatalf("replay result = %+v mutation calls = %d, want pending existing gate without writes", result, repository.mutationCalls)
	}

	input.PlanFingerprint = "sha256:changed"
	_, err = newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), input)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("PrepareSelfDeployPlanGate() changed replay error = %v, want ErrConflict", err)
	}
}

func TestPrepareSelfDeployPlanGateConflictsOnChangedFingerprint(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"}
	repository := &fakeRepository{
		ready: true,
		riskAssessments: []entity.RiskAssessment{{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			Target:             target,
			EvidenceRefs:       []value.EvidenceRef{{Kind: selfDeployPlanEvidenceKind, Ref: "agent:self-deploy-plan:1", Digest: "sha256:old", Summary: selfDeployPlanGateEvidenceSummary}},
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
		}},
	}

	_, err := newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), selfDeployPlanGateTestInput("sha256:new"))
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("PrepareSelfDeployPlanGate() error = %v, want ErrConflict", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want no writes on conflict", repository.mutationCalls)
	}
}

func TestPrepareSelfDeployPlanGateReturnsApprovedDecision(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	gateDecisionID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"}
	repository := &fakeRepository{
		ready: true,
		riskAssessments: []entity.RiskAssessment{{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			Target:             target,
			EvidenceRefs:       []value.EvidenceRef{{Kind: selfDeployPlanEvidenceKind, Ref: "agent:self-deploy-plan:1", Digest: "sha256:plan", Summary: selfDeployPlanGateEvidenceSummary}},
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy gate required"}},
		}},
		gateRequests: []entity.GateRequest{{
			VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 2},
			RiskAssessmentID: &assessmentID,
			Target:           target,
			Status:           enum.GateRequestStatusResolved,
			EvidenceSummary:  "bounded self-deploy plan",
		}},
		gateDecisions: []entity.GateDecision{{
			ID:               gateDecisionID,
			GateRequestID:    gateRequestID,
			DecisionActorRef: "user:owner",
			Outcome:          enum.GateOutcomeApprove,
			Reason:           "safe owner approval",
		}},
	}

	result, err := newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), selfDeployPlanGateTestInput("sha256:plan"))
	if err != nil {
		t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
	}
	if result.Status != enum.SelfDeployPlanGateStatusApproved || result.GateDecision == nil || result.GateDecision.ID != gateDecisionID {
		t.Fatalf("result = %+v, want approved gate decision", result)
	}
}

func TestPrepareSelfDeployPlanGateMapsDecisionOutcomes(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"}
	for _, tc := range []struct {
		name    string
		outcome enum.GateOutcome
		want    enum.SelfDeployPlanGateStatus
	}{
		{name: "approve", outcome: enum.GateOutcomeApprove, want: enum.SelfDeployPlanGateStatusApproved},
		{name: "reject", outcome: enum.GateOutcomeReject, want: enum.SelfDeployPlanGateStatusRejected},
		{name: "request changes", outcome: enum.GateOutcomeRevise, want: enum.SelfDeployPlanGateStatusRequestChanges},
		{name: "hold blocks", outcome: enum.GateOutcomeHold, want: enum.SelfDeployPlanGateStatusBlocked},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gateDecisionID := uuid.New()
			repository := &fakeRepository{
				ready: true,
				riskAssessments: []entity.RiskAssessment{{
					VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
					Target:             target,
					EvidenceRefs:       []value.EvidenceRef{{Kind: selfDeployPlanEvidenceKind, Ref: "agent:self-deploy-plan:1", Digest: "sha256:plan", Summary: selfDeployPlanGateEvidenceSummary}},
					EffectiveRiskClass: enum.RiskClassR2,
					Status:             enum.RiskAssessmentStatusActive,
					RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy gate required"}},
				}},
				gateRequests: []entity.GateRequest{{
					VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 2},
					RiskAssessmentID: &assessmentID,
					Target:           target,
					Status:           enum.GateRequestStatusResolved,
					EvidenceSummary:  "bounded self-deploy plan",
				}},
				gateDecisions: []entity.GateDecision{{
					ID:               gateDecisionID,
					GateRequestID:    gateRequestID,
					DecisionActorRef: "user:owner",
					Outcome:          tc.outcome,
					Reason:           "safe owner decision",
				}},
			}

			result, err := newTestService(repository).PrepareSelfDeployPlanGate(context.Background(), selfDeployPlanGateTestInput("sha256:plan"))
			if err != nil {
				t.Fatalf("PrepareSelfDeployPlanGate(): %v", err)
			}
			if result.Status != tc.want || result.GateDecision == nil || result.GateDecision.ID != gateDecisionID {
				t.Fatalf("status/decision = %s/%+v, want %s/%s", result.Status, result.GateDecision, tc.want, gateDecisionID)
			}
		})
	}
}

func TestSubmitGateDecisionRequiresSupportedOutcome(t *testing.T) {
	t.Parallel()

	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: expectedVersion},
			Target:        value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"},
			Status:        enum.GateRequestStatusAwaitingDecision,
		},
	}
	_, _, err := newTestService(repository).SubmitGateDecision(context.Background(), SubmitGateDecisionInput{
		GateRequestID:    gateRequestID,
		DecisionActorRef: "user:owner",
		Reason:           "safe owner decision",
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner"},
		},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("SubmitGateDecision() error = %v, want ErrInvalidArgument", err)
	}
	if repository.mutationCalls != 0 || len(repository.events) != 0 {
		t.Fatalf("mutation calls/events = %d/%d, want 0/0", repository.mutationCalls, len(repository.events))
	}
}

func TestSubmitGateDecisionReplayRejectsConflictingOutcome(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	gateDecisionID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	commandID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	meta := CommandMeta{
		CommandID: &commandID,
		Actor:     value.Actor{Type: "user", ID: "owner"},
		RequestID: "trace-gate-decision",
	}
	input := SubmitGateDecisionInput{
		GateRequestID:    gateRequestID,
		DecisionActorRef: "user:owner",
		Outcome:          enum.GateOutcomeApprove,
		Reason:           "safe owner approval",
		SourceRef:        "interaction:response:1",
		Meta:             meta,
	}
	replayPayload := gateDecisionReplayPayload(
		input,
		"user:owner",
		"",
		"safe owner approval",
		"",
		"interaction:response:1",
		value.InteractionDeliveryRef{},
	)
	repository := &fakeRepository{
		ready:            true,
		hasCommandResult: true,
		commandResult:    commandResultWithPayload(meta, enum.OperationSubmitGateDecision.String(), aggregateGateDecision, gateDecisionID, now, gateDecisionCommandResultPayload(replayPayload)),
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2},
			Target:        value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"},
			Status:        enum.GateRequestStatusResolved,
		},
		gateDecision: entity.GateDecision{
			ID:               gateDecisionID,
			GateRequestID:    gateRequestID,
			DecisionActorRef: "user:owner",
			Outcome:          enum.GateOutcomeApprove,
			Reason:           "safe owner approval",
			SourceRef:        "interaction:response:1",
			DecidedAt:        now,
		},
	}
	decision, request, err := newTestService(repository).SubmitGateDecision(context.Background(), input)
	if err != nil {
		t.Fatalf("SubmitGateDecision(replay): %v", err)
	}
	if decision.ID != gateDecisionID || request.ID != gateRequestID || repository.mutationCalls != 0 {
		t.Fatalf("replay result = %s/%s mutations=%d, want stored read-only result", decision.ID, request.ID, repository.mutationCalls)
	}

	input.Outcome = enum.GateOutcomeReject
	_, _, err = newTestService(repository).SubmitGateDecision(context.Background(), input)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("SubmitGateDecision(conflicting replay) error = %v, want ErrConflict", err)
	}
	if repository.mutationCalls != 0 || len(repository.events) != 0 {
		t.Fatalf("conflicting replay mutations/events = %d/%d, want 0/0", repository.mutationCalls, len(repository.events))
	}
}

func TestSubmitGateDecisionReplayRejectsAddedOptionalSafeFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	gateDecisionID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	commandID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	meta := CommandMeta{
		CommandID: &commandID,
		Actor:     value.Actor{Type: "user", ID: "owner"},
		RequestID: "trace-gate-decision",
	}
	baseInput := SubmitGateDecisionInput{
		GateRequestID:    gateRequestID,
		DecisionActorRef: "user:owner",
		Outcome:          enum.GateOutcomeApprove,
		Meta:             meta,
	}
	for _, tc := range []struct {
		name   string
		legacy bool
		change func(*SubmitGateDecisionInput)
	}{
		{
			name:   "full payload added reason",
			change: func(input *SubmitGateDecisionInput) { input.Reason = "safe added reason" },
		},
		{
			name:   "full payload added source ref",
			change: func(input *SubmitGateDecisionInput) { input.SourceRef = "interaction:response:changed" },
		},
		{
			name: "full payload added interaction ref",
			change: func(input *SubmitGateDecisionInput) {
				input.InteractionDeliveryRef = value.InteractionDeliveryRef{RequestRef: "interaction:request:changed", DecisionRef: "interaction:decision:changed"}
			},
		},
		{
			name:   "legacy payload added reason",
			legacy: true,
			change: func(input *SubmitGateDecisionInput) { input.Reason = "safe added reason" },
		},
		{
			name:   "legacy payload added source ref",
			legacy: true,
			change: func(input *SubmitGateDecisionInput) { input.SourceRef = "interaction:response:changed" },
		},
		{
			name:   "legacy payload added interaction ref",
			legacy: true,
			change: func(input *SubmitGateDecisionInput) {
				input.InteractionDeliveryRef = value.InteractionDeliveryRef{RequestRef: "interaction:request:changed", DecisionRef: "interaction:decision:changed"}
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{"gate_request_id": gateRequestID.String()}
			if !tc.legacy {
				payload = gateDecisionCommandResultPayload(gateDecisionReplayPayload(baseInput, "user:owner", "", "", "", "", value.InteractionDeliveryRef{}))
			}
			repository := &fakeRepository{
				ready:            true,
				hasCommandResult: true,
				commandResult:    commandResultWithPayload(meta, enum.OperationSubmitGateDecision.String(), aggregateGateDecision, gateDecisionID, now, payload),
				gateRequest: entity.GateRequest{
					VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2},
					Target:        value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"},
					Status:        enum.GateRequestStatusResolved,
				},
				gateDecision: entity.GateDecision{
					ID:               gateDecisionID,
					GateRequestID:    gateRequestID,
					DecisionActorRef: "user:owner",
					Outcome:          enum.GateOutcomeApprove,
					DecidedAt:        now,
				},
			}
			if _, _, err := newTestService(repository).SubmitGateDecision(context.Background(), baseInput); err != nil {
				t.Fatalf("SubmitGateDecision(base replay): %v", err)
			}

			changed := baseInput
			tc.change(&changed)
			_, _, err := newTestService(repository).SubmitGateDecision(context.Background(), changed)
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("SubmitGateDecision(changed replay) error = %v, want ErrConflict", err)
			}
			if repository.mutationCalls != 0 || len(repository.events) != 0 {
				t.Fatalf("changed replay mutations/events = %d/%d, want 0/0", repository.mutationCalls, len(repository.events))
			}
		})
	}
}

func TestSubmitGateDecisionRecordsRequestChangesEventSafely(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: expectedVersion},
			Target:        value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:1"},
			Status:        enum.GateRequestStatusAwaitingDecision,
			EvidenceRefs: []value.EvidenceRef{{
				Kind:           selfDeployPlanEvidenceKind,
				Ref:            "agent:self-deploy-plan:1",
				Digest:         "sha256:plan",
				Summary:        selfDeployPlanGateEvidenceSummary,
				RetentionClass: "safe_ref",
			}},
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: uuidGenerator{},
		Authorizer:  AllowAllAuthorizer{},
	})

	decision, request, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{
		GateRequestID:    gateRequestID,
		DecisionActorRef: "user:owner",
		Outcome:          enum.GateOutcomeRevise,
		Reason:           "owner requested manifest changes",
		SourceRef:        "staff-gateway:governance-summary",
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner"},
			RequestID:       "trace-gate-decision",
		},
	})
	if err != nil {
		t.Fatalf("SubmitGateDecision(): %v", err)
	}
	if request.Status != enum.GateRequestStatusResolved || request.Version != expectedVersion+1 {
		t.Fatalf("request status/version = %s/%d, want resolved/%d", request.Status, request.Version, expectedVersion+1)
	}
	if decision.Outcome != enum.GateOutcomeRevise || decision.DecisionActorRef != "user:owner" {
		t.Fatalf("decision = %+v, want revise by safe actor", decision)
	}
	if len(repository.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repository.events))
	}
	payload := string(repository.events[0].Payload)
	for _, want := range []string{`"outcome":"revise"`, `"target_type":"self_deploy_plan"`, `"target_ref":"agent:self-deploy-plan:1"`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("event payload = %s, want %s", payload, want)
		}
	}
	for _, unsafe := range []string{"raw_diff", "webhook body", "secret=", "kubeconfig", "stdout", "stderr", "provider response", "prompt transcript"} {
		if strings.Contains(payload, unsafe) {
			t.Fatalf("event payload leaked unsafe marker %q: %s", unsafe, payload)
		}
	}
	if !strings.Contains(string(repository.result.ResultPayload), `"outcome":"revise"`) {
		t.Fatalf("command result payload = %s, want outcome replay payload", string(repository.result.ResultPayload))
	}
}

func selfDeployPlanGateTestInput(fingerprint string) SelfDeployPlanGateInput {
	return SelfDeployPlanGateInput{
		SelfDeployPlanRef:       "agent:self-deploy-plan:1",
		ProjectContext:          value.ProjectContextRef{ProjectRef: "project:kodex", RepositoryRef: "repo:codex-k8s/kodex", ReleaseLineRef: "self-deploy"},
		ProviderSignalRef:       "provider:signal:merge-main",
		SourceRef:               "github:pull:1008",
		MergeCommitSHA:          "abcdef0123456789abcdef0123456789abcdef01",
		ServicesYAMLDigest:      "sha256:services",
		AffectedServiceKeys:     []string{"governance-manager"},
		PathCategories:          []string{"services_yaml"},
		ExpectedRuntimeJobTypes: []string{"build", "deploy"},
		ChangedFilesSummaryRef:  "provider:changed-files:1008",
		SafeSummary:             "bounded self-deploy plan",
		PlanFingerprint:         fingerprint,
		Meta: CommandMeta{
			IdempotencyKey: "agent:self-deploy-plan:1",
			Actor:          value.Actor{Type: "service", ID: "agent-manager"},
			RequestID:      "trace-self-deploy-plan",
		},
	}
}

func TestEvaluateRiskRejectsUnsafeSummary(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target:            value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
		EvaluationSummary: value.RiskEvaluationSummary{Summary: "raw_diff: full provider diff"},
		Meta:              CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("EvaluateRisk() error = %v, want ErrInvalidArgument", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestEvaluateRiskAccessDenied(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	_, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target: value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
		Meta:   CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("EvaluateRisk() error = %v, want ErrForbidden", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestRecordReviewSignalAccessDeniedBeforeRepositoryWrite(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	_, err := service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
		Target:       value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
		RoleKind:     enum.ReviewRoleKindReviewer,
		AuthorRef:    "agent-run:reviewer-1",
		Outcome:      enum.ReviewSignalOutcomeRequestChanges,
		Severity:     enum.SignalSeverityBlocking,
		EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "changes requested"}},
		Summary:      "changes requested",
		Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("RecordReviewSignal() error = %v, want ErrForbidden", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestRecordReviewSignalDeduplicatesOwnerEvidenceRefs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 14, 30, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	existingID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"}
	evidenceRefs := []value.EvidenceRef{
		{Kind: "agent_run", Ref: "agent-run:reviewer-1", Summary: "agent reviewer finished"},
		{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review approved"},
	}
	normalizedEvidence, err := normalizeReviewSignalEvidenceRefs(evidenceRefs)
	if err != nil {
		t.Fatalf("normalize evidence: %v", err)
	}
	existing := entity.ReviewSignal{
		ID:                existingID,
		Target:            target,
		RoleKind:          enum.ReviewRoleKindReviewer,
		AuthorRef:         "agent-run:reviewer-1",
		Outcome:           enum.ReviewSignalOutcomePass,
		Severity:          enum.SignalSeverityInfo,
		EvidenceRefs:      normalizedEvidence,
		Summary:           "approved",
		SourceFingerprint: reviewSignalFingerprint(target, enum.ReviewRoleKindReviewer, "agent-run:reviewer-1", normalizedEvidence),
		CreatedAt:         now.Add(-time.Minute),
	}
	repository := &fakeRepository{ready: true, reviewSignalByFingerprint: existing}
	service := NewWithConfig(Config{Repository: repository, Clock: fixedClock{now: now}, IDGenerator: uuidGenerator{}, Authorizer: AllowAllAuthorizer{}})

	signal, err := service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
		Target:       target,
		RoleKind:     enum.ReviewRoleKindReviewer,
		AuthorRef:    " agent-run:reviewer-1 ",
		Outcome:      enum.ReviewSignalOutcomePass,
		Severity:     enum.SignalSeverityInfo,
		EvidenceRefs: []value.EvidenceRef{evidenceRefs[1], evidenceRefs[0]},
		Summary:      " approved ",
		Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("RecordReviewSignal(): %v", err)
	}
	if signal.ID != existingID {
		t.Fatalf("signal id = %s, want existing %s", signal.ID, existingID)
	}
	if repository.mutationCalls != 0 || len(repository.events) != 0 {
		t.Fatalf("mutation calls/events = %d/%d, want 0/0 for source-ref replay", repository.mutationCalls, len(repository.events))
	}
	if repository.result.AggregateID != existingID {
		t.Fatalf("recorded command result aggregate = %s, want %s", repository.result.AggregateID, existingID)
	}
}

func TestRecordReviewSignalRejectsConflictingOwnerEvidenceRef(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	existingID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"}
	evidenceRefs := []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review approved"}}
	fingerprint := reviewSignalFingerprint(target, enum.ReviewRoleKindReviewer, "agent-run:reviewer-1", evidenceRefs)
	repository := &fakeRepository{ready: true, reviewSignalByFingerprint: entity.ReviewSignal{
		ID:                existingID,
		Target:            target,
		RoleKind:          enum.ReviewRoleKindReviewer,
		AuthorRef:         "agent-run:reviewer-1",
		Outcome:           enum.ReviewSignalOutcomePass,
		Severity:          enum.SignalSeverityInfo,
		EvidenceRefs:      evidenceRefs,
		Summary:           "approved",
		SourceFingerprint: fingerprint,
	}}
	service := newTestService(repository)

	_, err := service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
		Target:       target,
		RoleKind:     enum.ReviewRoleKindReviewer,
		AuthorRef:    "agent-run:reviewer-1",
		Outcome:      enum.ReviewSignalOutcomeRequestChanges,
		Severity:     enum.SignalSeverityBlocking,
		EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review changed", Digest: "digest:v2"}},
		Summary:      "changes requested",
		Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordReviewSignal() error = %v, want ErrConflict", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestRecordReviewSignalRejectsUnsafeOrMissingRefs(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	service := newTestService(&fakeRepository{ready: true})
	_, err := service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
		Target:       value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
		RoleKind:     enum.ReviewRoleKindReviewer,
		AuthorRef:    "agent-run:reviewer-1",
		Outcome:      enum.ReviewSignalOutcomePass,
		Severity:     enum.SignalSeverityInfo,
		EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "raw_provider_payload token=secret"}},
		Summary:      "approved",
		Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordReviewSignal() unsafe error = %v, want ErrInvalidArgument", err)
	}
	_, err = service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
		Target:    value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
		RoleKind:  enum.ReviewRoleKindReviewer,
		AuthorRef: "agent-run:reviewer-1",
		Outcome:   enum.ReviewSignalOutcomePass,
		Severity:  enum.SignalSeverityInfo,
		Meta:      CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordReviewSignal() missing evidence error = %v, want ErrInvalidArgument", err)
	}
	_, err = service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
		Target:    value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
		RoleKind:  enum.ReviewRoleKindReviewer,
		AuthorRef: "agent-run:reviewer-1",
		Outcome:   enum.ReviewSignalOutcomePass,
		Severity:  enum.SignalSeverityInfo,
		EvidenceRefs: []value.EvidenceRef{
			{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review approved"},
			{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review changed", Digest: "digest:v2"},
		},
		Summary: "approved",
		Meta:    CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordReviewSignal() conflicting evidence metadata error = %v, want ErrInvalidArgument", err)
	}
}

func TestRecordReviewSignalRejectsEventUnsafePayload(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	tests := []struct {
		name    string
		input   RecordReviewSignalInput
		wantErr error
	}{
		{
			name: "summary stdout marker",
			input: RecordReviewSignalInput{
				Target:       value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
				RoleKind:     enum.ReviewRoleKindReviewer,
				AuthorRef:    "agent-run:reviewer-1",
				Outcome:      enum.ReviewSignalOutcomePass,
				Severity:     enum.SignalSeverityInfo,
				EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review approved"}},
				Summary:      "stdout contains provider logs",
				Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
			},
			wantErr: errs.ErrInvalidArgument,
		},
		{
			name: "evidence source bearer marker",
			input: RecordReviewSignalInput{
				Target:       value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
				RoleKind:     enum.ReviewRoleKindReviewer,
				AuthorRef:    "agent-run:reviewer-1",
				Outcome:      enum.ReviewSignalOutcomePass,
				Severity:     enum.SignalSeverityInfo,
				EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1 authorization: bearer abc", Summary: "provider review approved"}},
				Summary:      "approved",
				Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
			},
			wantErr: errs.ErrInvalidArgument,
		},
		{
			name: "evidence summary workspace path",
			input: RecordReviewSignalInput{
				Target:       value.ExternalRef{Type: "pull_request", Ref: "provider-pr:1"},
				RoleKind:     enum.ReviewRoleKindReviewer,
				AuthorRef:    "agent-run:reviewer-1",
				Outcome:      enum.ReviewSignalOutcomePass,
				Severity:     enum.SignalSeverityInfo,
				EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "workspace path /home/s/workspace/file"}},
				Summary:      "approved",
				Meta:         CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
			},
			wantErr: errs.ErrInvalidArgument,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{ready: true}
			_, err := newTestService(repository).RecordReviewSignal(context.Background(), tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RecordReviewSignal() error = %v, want %v", err, tt.wantErr)
			}
			if repository.mutationCalls != 0 || len(repository.events) != 0 {
				t.Fatalf("mutation calls/events = %d/%d, want 0/0 for event-unsafe input", repository.mutationCalls, len(repository.events))
			}
		})
	}
}

func TestRiskReadAccessDeniedBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	meta := QueryMeta{Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	if _, err := service.GetRiskAssessment(context.Background(), GetRiskAssessmentInput{RiskAssessmentID: assessmentID, Meta: meta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetRiskAssessment() error = %v, want ErrForbidden", err)
	}
	if _, _, err := service.ListRiskFactors(context.Background(), ListRiskFactorsInput{
		Filter: query.RiskFactorFilter{RiskAssessmentID: assessmentID},
		Meta:   meta,
	}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ListRiskFactors() error = %v, want ErrForbidden", err)
	}
	if _, _, err := service.ListReviewSignals(context.Background(), ListReviewSignalsInput{
		Filter: query.ReviewSignalFilter{RiskAssessmentID: &assessmentID},
		Meta:   meta,
	}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ListReviewSignals() error = %v, want ErrForbidden", err)
	}
	if _, _, err := service.ListRiskAssessments(context.Background(), ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{Target: value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"}},
		Meta:   meta,
	}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ListRiskAssessments() error = %v, want ErrForbidden", err)
	}
	if repository.assessmentReads != 0 || repository.riskAssessmentListCalls != 0 || repository.riskFactorListCalls != 0 || repository.reviewSignalListCalls != 0 {
		t.Fatalf("risk reads = assessment:%d list:%d factors:%d signals:%d, want 0 before access allow", repository.assessmentReads, repository.riskAssessmentListCalls, repository.riskFactorListCalls, repository.reviewSignalListCalls)
	}
}

func TestListRiskAssessmentsRejectsContextRefsNotAppliedBySQL(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, _, err := service.ListRiskAssessments(context.Background(), ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{
			ProjectContext:     value.ProjectContextRef{ServiceRef: "service:api"},
			EffectiveRiskClass: enum.RiskClassR2,
		},
		Meta: QueryMeta{Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListRiskAssessments() error = %v, want ErrInvalidArgument", err)
	}
	if repository.riskAssessmentListCalls != 0 {
		t.Fatalf("risk assessment list calls = %d, want 0", repository.riskAssessmentListCalls)
	}
}

func TestListReviewSignalsRejectsOutcomeOnlyFilter(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, _, err := service.ListReviewSignals(context.Background(), ListReviewSignalsInput{
		Filter: query.ReviewSignalFilter{Outcome: enum.ReviewSignalOutcomePass},
		Meta:   QueryMeta{Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListReviewSignals() error = %v, want ErrInvalidArgument", err)
	}
	if repository.reviewSignalListCalls != 0 {
		t.Fatalf("review signal list calls = %d, want 0", repository.reviewSignalListCalls)
	}
}

func TestReevaluateRiskUsesExpectedVersionAndReviewSignals(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(3)
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: expectedVersion},
			Target:             value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
			EvaluationSummary:  value.RiskEvaluationSummary{Summary: "stored safe summary"},
			InitialRiskClass:   enum.RiskClassR1,
			EffectiveRiskClass: enum.RiskClassR1,
			Status:             enum.RiskAssessmentStatusActive,
		},
		reviewSignals: []entity.ReviewSignal{{
			ID:               uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
			RiskAssessmentID: &assessmentID,
			Outcome:          enum.ReviewSignalOutcomeBlock,
			Severity:         enum.SignalSeverityCritical,
			Summary:          "critical owner block",
		}},
	}
	service := newTestService(repository)

	assessment, err := service.ReevaluateRisk(context.Background(), ReevaluateRiskInput{
		RiskAssessmentID: assessmentID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "provider-hub"},
		},
	})
	if err != nil {
		t.Fatalf("ReevaluateRisk(): %v", err)
	}
	if assessment.Version != expectedVersion+1 || assessment.EffectiveRiskClass != enum.RiskClassR3 {
		t.Fatalf("assessment version/risk = %d/%s, want %d/R3", assessment.Version, assessment.EffectiveRiskClass, expectedVersion+1)
	}
	if repository.assessmentUpdateCalls != 1 {
		t.Fatalf("assessment update calls = %d, want 1", repository.assessmentUpdateCalls)
	}
	if len(repository.events) != 2 || repository.events[1].EventType != governanceevents.EventRiskAssessmentChanged {
		t.Fatalf("events = %+v, want completed and changed", repository.events)
	}
}

func TestReevaluateRiskPublishesChangedWhenFactorSignatureChanges(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase: entity.VersionedBase{ID: assessmentID, Version: expectedVersion},
			Target:        value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
			EvaluationSummary: value.RiskEvaluationSummary{Factors: []value.RiskEvaluationFactor{{
				SourceType: string(enum.RiskFactorSourceTypeDatabase),
				Ref:        "db:migration",
				Summary:    "new migration summary",
				Tags:       []string{"migration"},
			}}},
			InitialRiskClass:   enum.RiskClassR2,
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
		},
		riskFactors: []entity.RiskFactor{{
			ID:               uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
			RiskAssessmentID: assessmentID,
			SourceType:       enum.RiskFactorSourceTypeDatabase,
			SourceRef:        "db:migration",
			RiskClass:        enum.RiskClassR2,
			Summary:          "old migration summary",
		}},
	}
	service := newTestService(repository)

	assessment, err := service.ReevaluateRisk(context.Background(), ReevaluateRiskInput{
		RiskAssessmentID: assessmentID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "provider-hub"},
		},
	})
	if err != nil {
		t.Fatalf("ReevaluateRisk(): %v", err)
	}
	if assessment.EffectiveRiskClass != enum.RiskClassR2 || len(assessment.RequiredGates) != 0 {
		t.Fatalf("assessment risk/gates = %s/%d, want same R2/0", assessment.EffectiveRiskClass, len(assessment.RequiredGates))
	}
	if len(repository.events) != 2 || repository.events[1].EventType != governanceevents.EventRiskAssessmentChanged {
		t.Fatalf("events = %+v, want changed event for factor signature change", repository.events)
	}
}

func TestReevaluateRiskRejectsStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(2)
	repository := &fakeRepository{
		ready:      true,
		assessment: entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: assessmentID, Version: 3}},
	}
	service := newTestService(repository)

	_, err := service.ReevaluateRisk(context.Background(), ReevaluateRiskInput{
		RiskAssessmentID: assessmentID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "provider-hub"},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("ReevaluateRisk() error = %v, want ErrConflict", err)
	}
	if repository.assessmentUpdateCalls != 0 {
		t.Fatalf("assessment update calls = %d, want 0", repository.assessmentUpdateCalls)
	}
}

func TestMutatingUseCasesReplayStoredCommandResults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 30, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	meta := CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	scope := value.ExternalRef{Type: "project", Ref: "project:core"}
	target := value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/804"}
	projectContext := value.ProjectContextRef{ProjectRef: "project:core"}
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	assessmentID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	reviewSignalID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	gateDecisionID := uuid.MustParse("ffffffff-ffff-4fff-ffff-ffffffffffff")
	cancelledGateID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	expiredGateID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	releasePackageID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	profileVersion := int64(20260526123000)

	tests := []struct {
		name      string
		result    entity.CommandResult
		configure func(*fakeRepository)
		run       func(*testing.T, *Service)
	}{
		{
			name:   "create risk profile",
			result: commandResult(meta, enum.OperationCreateRiskProfile.String(), governanceevents.AggregateRiskProfile, profileID, now),
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 1}, Scope: scope, Slug: "default"}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				profile, err := service.CreateRiskProfile(context.Background(), CreateRiskProfileInput{Scope: scope, Slug: "default", Meta: meta})
				if err != nil {
					t.Fatalf("CreateRiskProfile(): %v", err)
				}
				if profile.ID != profileID {
					t.Fatalf("profile id = %s, want %s", profile.ID, profileID)
				}
			},
		},
		{
			name:   "create risk profile version",
			result: commandResultWithPayload(meta, enum.OperationCreateRiskProfileVersion.String(), aggregateRiskProfileVersion, profileID, now, map[string]any{"profile_version": profileVersion}),
			configure: func(repository *fakeRepository) {
				repository.profileVersion = entity.RiskProfileVersion{RiskProfileID: profileID, ProfileVersion: profileVersion}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				version, err := service.CreateRiskProfileVersion(context.Background(), CreateRiskProfileVersionInput{RiskProfileID: profileID, Meta: meta})
				if err != nil {
					t.Fatalf("CreateRiskProfileVersion(): %v", err)
				}
				if version.ProfileVersion != profileVersion {
					t.Fatalf("profile version = %d, want %d", version.ProfileVersion, profileVersion)
				}
			},
		},
		{
			name:   "activate risk profile version",
			result: commandResultWithPayload(meta, enum.OperationActivateRiskProfileVersion.String(), governanceevents.AggregateRiskProfile, profileID, now, map[string]any{"profile_version": profileVersion}),
			configure: func(repository *fakeRepository) {
				repository.profileVersion = entity.RiskProfileVersion{RiskProfileID: profileID, ProfileVersion: profileVersion, Status: enum.RiskProfileVersionStatusActive}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				version, err := service.ActivateRiskProfileVersion(context.Background(), ActivateRiskProfileVersionInput{RiskProfileID: profileID, ProfileVersion: profileVersion, Meta: meta})
				if err != nil {
					t.Fatalf("ActivateRiskProfileVersion(): %v", err)
				}
				if version.ProfileVersion != profileVersion {
					t.Fatalf("profile version = %d, want %d", version.ProfileVersion, profileVersion)
				}
			},
		},
		{
			name:   "archive risk profile",
			result: commandResult(meta, enum.OperationArchiveRiskProfile.String(), governanceevents.AggregateRiskProfile, profileID, now),
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 2}, Scope: scope, Slug: "default", Status: enum.RiskProfileStatusArchived}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				profile, err := service.ArchiveRiskProfile(context.Background(), ArchiveRiskProfileInput{RiskProfileID: profileID, Meta: meta})
				if err != nil {
					t.Fatalf("ArchiveRiskProfile(): %v", err)
				}
				if profile.ID != profileID {
					t.Fatalf("profile id = %s, want %s", profile.ID, profileID)
				}
			},
		},
		{
			name:   "evaluate risk",
			result: commandResult(meta, enum.OperationEvaluateRisk.String(), governanceevents.AggregateRiskAssessment, assessmentID, now),
			configure: func(repository *fakeRepository) {
				repository.assessment = entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: assessmentID, Version: 1}, Target: target, ProjectContext: projectContext}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				assessment, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{Target: target, ProjectContext: projectContext, Meta: meta})
				if err != nil {
					t.Fatalf("EvaluateRisk(): %v", err)
				}
				if assessment.ID != assessmentID {
					t.Fatalf("assessment id = %s, want %s", assessment.ID, assessmentID)
				}
			},
		},
		{
			name:   "record review signal",
			result: commandResult(meta, enum.OperationRecordReviewSignal.String(), governanceevents.AggregateReviewSignal, reviewSignalID, now),
			configure: func(repository *fakeRepository) {
				evidenceRefs := []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review approved"}}
				repository.reviewSignal = entity.ReviewSignal{
					ID:                reviewSignalID,
					Target:            target,
					RoleKind:          enum.ReviewRoleKindReviewer,
					AuthorRef:         "reviewer:owner",
					Outcome:           enum.ReviewSignalOutcomePass,
					Severity:          enum.SignalSeverityInfo,
					EvidenceRefs:      evidenceRefs,
					Summary:           "approved",
					SourceFingerprint: reviewSignalFingerprint(target, enum.ReviewRoleKindReviewer, "reviewer:owner", evidenceRefs),
				}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				signal, err := service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{
					Target:       target,
					RoleKind:     enum.ReviewRoleKindReviewer,
					AuthorRef:    "reviewer:owner",
					Outcome:      enum.ReviewSignalOutcomePass,
					Severity:     enum.SignalSeverityInfo,
					EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "provider review approved"}},
					Summary:      "approved",
					Meta:         meta,
				})
				if err != nil {
					t.Fatalf("RecordReviewSignal(): %v", err)
				}
				if signal.ID != reviewSignalID {
					t.Fatalf("review signal id = %s, want %s", signal.ID, reviewSignalID)
				}
			},
		},
		{
			name:   "request gate",
			result: commandResult(meta, enum.OperationRequestGate.String(), aggregateGateRequest, gateRequestID, now),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}, Target: target}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				request, err := service.RequestGate(context.Background(), RequestGateInput{Target: target, Meta: meta})
				if err != nil {
					t.Fatalf("RequestGate(): %v", err)
				}
				if request.ID != gateRequestID {
					t.Fatalf("gate request id = %s, want %s", request.ID, gateRequestID)
				}
			},
		},
		{
			name:   "submit gate decision",
			result: commandResultWithPayload(meta, enum.OperationSubmitGateDecision.String(), aggregateGateDecision, gateDecisionID, now, map[string]any{"gate_request_id": gateRequestID.String()}),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}, Target: target, Status: enum.GateRequestStatusResolved}
				repository.gateDecision = entity.GateDecision{ID: gateDecisionID, GateRequestID: gateRequestID, DecisionActorRef: "user:owner"}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				decision, request, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{GateRequestID: gateRequestID, DecisionActorRef: "user:owner", Meta: meta})
				if err != nil {
					t.Fatalf("SubmitGateDecision(): %v", err)
				}
				if decision.ID != gateDecisionID || request.ID != gateRequestID {
					t.Fatalf("decision/request ids = %s/%s, want %s/%s", decision.ID, request.ID, gateDecisionID, gateRequestID)
				}
			},
		},
		{
			name:   "cancel gate",
			result: commandResultWithPayload(meta, enum.OperationCancelGate.String(), aggregateGateRequest, cancelledGateID, now, map[string]any{"status": string(enum.GateRequestStatusCancelled)}),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: cancelledGateID, Version: 2}, Target: target, Status: enum.GateRequestStatusCancelled}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				request, err := service.CancelGate(context.Background(), CancelGateInput{GateRequestID: cancelledGateID, Meta: meta})
				if err != nil {
					t.Fatalf("CancelGate(): %v", err)
				}
				if request.ID != cancelledGateID || request.Status != enum.GateRequestStatusCancelled {
					t.Fatalf("gate request = %+v, want cancelled %s", request, cancelledGateID)
				}
			},
		},
		{
			name:   "expire gate",
			result: commandResultWithPayload(meta, enum.OperationExpireGate.String(), aggregateGateRequest, expiredGateID, now, map[string]any{"status": string(enum.GateRequestStatusExpired)}),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: expiredGateID, Version: 2}, Target: target, Status: enum.GateRequestStatusExpired}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				request, err := service.ExpireGate(context.Background(), ExpireGateInput{GateRequestID: expiredGateID, Meta: meta})
				if err != nil {
					t.Fatalf("ExpireGate(): %v", err)
				}
				if request.ID != expiredGateID || request.Status != enum.GateRequestStatusExpired {
					t.Fatalf("gate request = %+v, want expired %s", request, expiredGateID)
				}
			},
		},
		{
			name:   "build release decision package",
			result: commandResult(meta, enum.OperationBuildReleaseDecisionPackage.String(), governanceevents.AggregateReleaseDecisionPackage, releasePackageID, now),
			configure: func(repository *fakeRepository) {
				repository.releasePackage = entity.ReleaseDecisionPackage{VersionedBase: entity.VersionedBase{ID: releasePackageID, Version: 1}, ReleaseCandidateRef: "release:v1", ProjectContext: projectContext}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				item, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{ReleaseCandidateRef: "release:v1", ProjectContext: projectContext, Meta: meta})
				if err != nil {
					t.Fatalf("BuildReleaseDecisionPackage(): %v", err)
				}
				if item.ID != releasePackageID {
					t.Fatalf("release package id = %s, want %s", item.ID, releasePackageID)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{ready: true, hasCommandResult: true, commandResult: tt.result}
			tt.configure(repository)
			service := NewWithConfig(Config{
				Repository:  repository,
				Clock:       fixedClock{now: now},
				IDGenerator: &fixedIDs{},
				Authorizer:  AllowAllAuthorizer{},
			})

			tt.run(t, service)
			if repository.mutationCalls != 0 {
				t.Fatalf("mutation calls = %d, want 0 for replay", repository.mutationCalls)
			}
		})
	}
}

func TestExistingAggregateMutationsRequireExpectedVersion(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	meta := CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	profileVersion := int64(20260526123000)
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")

	tests := []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "activate risk profile version",
			run: func(service *Service) error {
				_, err := service.ActivateRiskProfileVersion(context.Background(), ActivateRiskProfileVersionInput{RiskProfileID: profileID, ProfileVersion: profileVersion, Meta: meta})
				return err
			},
		},
		{
			name: "archive risk profile",
			run: func(service *Service) error {
				_, err := service.ArchiveRiskProfile(context.Background(), ArchiveRiskProfileInput{RiskProfileID: profileID, Meta: meta})
				return err
			},
		},
		{
			name: "submit gate decision",
			run: func(service *Service) error {
				_, _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{GateRequestID: gateRequestID, DecisionActorRef: "user:owner", Meta: meta})
				return err
			},
		},
		{
			name: "cancel gate",
			run: func(service *Service) error {
				_, err := service.CancelGate(context.Background(), CancelGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
		{
			name: "expire gate",
			run: func(service *Service) error {
				_, err := service.ExpireGate(context.Background(), ExpireGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.run(newTestService(&fakeRepository{ready: true}))
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("%s error = %v, want ErrInvalidArgument", tt.name, err)
			}
		})
	}
}

func TestExistingAggregateMutationsRejectStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	expectedVersion := int64(1)
	meta := CommandMeta{CommandID: &commandID, ExpectedVersion: &expectedVersion, Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	profileVersion := int64(20260526123000)
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")

	tests := []struct {
		name      string
		configure func(*fakeRepository)
		run       func(*Service) error
	}{
		{
			name: "activate risk profile version",
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 2}}
				repository.profileVersion = entity.RiskProfileVersion{RiskProfileID: profileID, ProfileVersion: profileVersion}
			},
			run: func(service *Service) error {
				_, err := service.ActivateRiskProfileVersion(context.Background(), ActivateRiskProfileVersionInput{RiskProfileID: profileID, ProfileVersion: profileVersion, Meta: meta})
				return err
			},
		},
		{
			name: "archive risk profile",
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 2}}
			},
			run: func(service *Service) error {
				_, err := service.ArchiveRiskProfile(context.Background(), ArchiveRiskProfileInput{RiskProfileID: profileID, Meta: meta})
				return err
			},
		},
		{
			name: "submit gate decision",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}}
			},
			run: func(service *Service) error {
				_, _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{GateRequestID: gateRequestID, DecisionActorRef: "user:owner", Meta: meta})
				return err
			},
		},
		{
			name: "cancel gate",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, err := service.CancelGate(context.Background(), CancelGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
		{
			name: "expire gate",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, err := service.ExpireGate(context.Background(), ExpireGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{ready: true}
			tt.configure(repository)
			err := tt.run(newTestService(repository))
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("%s error = %v, want ErrConflict", tt.name, err)
			}
			if repository.mutationCalls != 0 {
				t.Fatalf("mutation calls = %d, want 0 for stale expected_version", repository.mutationCalls)
			}
		})
	}
}

func TestGateLifecycleRejectsEventUnsafePayload(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	target := value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"}
	meta := CommandMeta{
		CommandID:       &commandID,
		ExpectedVersion: &expectedVersion,
		Actor:           value.Actor{Type: "service", ID: "agent-manager"},
	}
	tests := []struct {
		name      string
		configure func(*fakeRepository)
		run       func(*Service) error
	}{
		{
			name: "request gate evidence summary",
			run: func(service *Service) error {
				_, err := service.RequestGate(context.Background(), RequestGateInput{
					Target:          target,
					EvidenceSummary: "raw provider payload with secret=abc",
					Meta:            meta,
				})
				return err
			},
		},
		{
			name: "request gate evidence ref",
			run: func(service *Service) error {
				_, err := service.RequestGate(context.Background(), RequestGateInput{
					Target:       target,
					EvidenceRefs: []value.EvidenceRef{{Kind: "provider_review", Ref: "provider-review:1", Summary: "stderr raw logs"}},
					Meta:         meta,
				})
				return err
			},
		},
		{
			name: "request gate interaction ref",
			run: func(service *Service) error {
				_, err := service.RequestGate(context.Background(), RequestGateInput{
					Target: target,
					InteractionDeliveryRef: value.InteractionDeliveryRef{
						RequestRef: "interaction:request:1 authorization: bearer token",
					},
					Meta: meta,
				})
				return err
			},
		},
		{
			name: "request gate request id",
			run: func(service *Service) error {
				unsafeMeta := meta
				unsafeMeta.RequestID = "authorization: bearer raw-request-token"
				_, err := service.RequestGate(context.Background(), RequestGateInput{
					Target: target,
					Meta:   unsafeMeta,
				})
				return err
			},
		},
		{
			name: "submit gate decision reason",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}, Target: target, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{
					GateRequestID:    gateRequestID,
					DecisionActorRef: "user:owner",
					Reason:           "stdout contains owner prompt transcript",
					Meta:             meta,
				})
				return err
			},
		},
		{
			name: "submit gate decision source ref",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}, Target: target, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{
					GateRequestID:    gateRequestID,
					DecisionActorRef: "user:owner",
					SourceRef:        "provider-response authorization: bearer token",
					Meta:             meta,
				})
				return err
			},
		},
		{
			name: "cancel gate terminal reason",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}, Target: target, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, err := service.CancelGate(context.Background(), CancelGateInput{
					GateRequestID: gateRequestID,
					Reason:        "kubeconfig contains cluster secret",
					Meta:          meta,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{ready: true}
			if tt.configure != nil {
				tt.configure(repository)
			}
			err := tt.run(newTestService(repository))
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("%s error = %v, want ErrInvalidArgument", tt.name, err)
			}
			if repository.mutationCalls != 0 || len(repository.events) != 0 {
				t.Fatalf("mutation calls/events = %d/%d, want 0/0 for event-unsafe input", repository.mutationCalls, len(repository.events))
			}
		})
	}
}

func TestCancelGateStoresTerminalStateAndSafeOutbox(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 13, 0, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	eventID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1, CreatedAt: now, UpdatedAt: now},
			Target:        value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"},
			Status:        enum.GateRequestStatusRequested,
		},
	}
	service := NewWithConfig(Config{Repository: repository, Clock: fixedClock{now: now}, IDGenerator: &fixedIDs{ids: []uuid.UUID{eventID}}, Authorizer: AllowAllAuthorizer{}})

	request, err := service.CancelGate(context.Background(), CancelGateInput{
		GateRequestID: gateRequestID,
		Reason:        "safe operator cancellation summary",
		InteractionDeliveryRef: value.InteractionDeliveryRef{
			RequestRef: "interaction:request:1",
		},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("CancelGate(): %v", err)
	}
	if request.Status != enum.GateRequestStatusCancelled || request.Version != 2 {
		t.Fatalf("gate request status/version = %s/%d, want cancelled/2", request.Status, request.Version)
	}
	if request.TerminalActorRef != "service:agent-manager" || request.TerminalReason != "safe operator cancellation summary" || request.TerminalAt == nil {
		t.Fatalf("terminal metadata = actor %q reason %q at %v", request.TerminalActorRef, request.TerminalReason, request.TerminalAt)
	}
	if len(repository.events) != 1 || repository.events[0].EventType != governanceevents.EventGateCancelled {
		t.Fatalf("events = %+v, want one gate cancelled event", repository.events)
	}
	payload := string(repository.events[0].Payload)
	if !strings.Contains(payload, `"safe_summary":"safe operator cancellation summary"`) || !strings.Contains(payload, `"interaction_request_ref":"interaction:request:1"`) {
		t.Fatalf("outbox payload = %s, want bounded summary and interaction request ref", payload)
	}
	if strings.Contains(payload, "raw_provider_payload") || strings.Contains(payload, "secret") {
		t.Fatalf("outbox payload leaked unsafe details: %s", payload)
	}
}

func TestExpireGateRejectsClosedGate(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	service := newTestService(&fakeRepository{
		ready: true,
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1},
			Status:        enum.GateRequestStatusResolved,
		},
	})

	_, err := service.ExpireGate(context.Background(), ExpireGateInput{
		GateRequestID: gateRequestID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "interaction-hub"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ExpireGate() error = %v, want ErrPreconditionFailed", err)
	}
}

func TestGateLifecycleAccessDenied(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	meta := CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}}
	queryMeta := QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	if _, err := service.RequestGate(context.Background(), RequestGateInput{Target: value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"}, Meta: meta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("RequestGate() error = %v, want ErrForbidden", err)
	}
	if _, err := service.GetGateRequest(context.Background(), GetGateRequestInput{GateRequestID: gateRequestID, Meta: queryMeta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetGateRequest() error = %v, want ErrForbidden", err)
	}
	if _, err := service.GetGateDecision(context.Background(), GetGateDecisionInput{GateDecisionID: uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"), GateRequestID: gateRequestID, Meta: queryMeta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetGateDecision() error = %v, want ErrForbidden", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0 when access denied", repository.mutationCalls)
	}
	if repository.gateRequestReads != 0 || repository.gateDecisionReads != 0 {
		t.Fatalf("gate reads = request:%d decision:%d, want 0 before access allow", repository.gateRequestReads, repository.gateDecisionReads)
	}
}

func TestGateLifecycleRequiresExplicitAuthorizer(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	service := NewWithConfig(Config{Repository: &fakeRepository{ready: true}, Clock: systemClock{}, IDGenerator: uuidGenerator{}})

	_, err := service.RequestGate(context.Background(), RequestGateInput{
		Target: value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"},
		Meta:   CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("RequestGate() error = %v, want ErrDependencyUnavailable", err)
	}
}

func TestSubmitGateDecisionAuthorizesOwnerActor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	decisionID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	expectedVersion := int64(4)
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:latest"}
	project := value.ProjectContextRef{ProjectRef: "project-self", RepositoryRef: "repo-self", ReleaseLineRef: "self-deploy"}
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase:  entity.VersionedBase{ID: assessmentID, Version: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-90 * time.Minute)},
			Target:         target,
			ProjectContext: project,
		},
		gateRequest: entity.GateRequest{
			VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: expectedVersion, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
			RiskAssessmentID: &assessmentID,
			Target:           target,
			Status:           enum.GateRequestStatusAwaitingDecision,
		},
	}
	var captured AuthorizationRequest
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{decisionID}},
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			captured = request
			return nil
		}),
	})

	decision, request, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{
		GateRequestID:          gateRequestID,
		DecisionActorRef:       "user/owner-1",
		Outcome:                enum.GateOutcomeApprove,
		Reason:                 "owner approved self-deploy gate",
		DecisionPolicyRef:      "self_deploy.owner_gate",
		InteractionDeliveryRef: value.InteractionDeliveryRef{DecisionRef: "staff-gateway/self-deploy-gate/" + gateRequestID.String()},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner-1"},
		},
	})
	if err != nil {
		t.Fatalf("SubmitGateDecision(): %v", err)
	}
	if decision.ID != decisionID || request.Status != enum.GateRequestStatusResolved {
		t.Fatalf("decision/request = %+v/%+v, want resolved decision %s", decision, request, decisionID)
	}
	if captured.Subject.Type != "user" || captured.Subject.ID != "owner-1" ||
		captured.ActionKey != actionGateDecide ||
		captured.ResourceType != "governance_gate" ||
		captured.ResourceID != target.Ref ||
		captured.ScopeType != "project" ||
		captured.ScopeID != project.ProjectRef {
		t.Fatalf("access request = %+v, want owner gate decide in project scope", captured)
	}
}

func TestGateLifecycleRejectsMissingAccessResource(t *testing.T) {
	t.Parallel()

	service := newTestService(&fakeRepository{ready: true})
	meta := QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}

	_, _, err := service.ListGateRequests(context.Background(), ListGateRequestsInput{Meta: meta})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListGateRequests() error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListGateDecisions(context.Background(), ListGateDecisionsInput{Meta: meta})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListGateDecisions() error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListGateDecisions(context.Background(), ListGateDecisionsInput{
		Filter: query.GateDecisionFilter{Outcome: enum.GateOutcomeApprove},
		Meta:   meta,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListGateDecisions(outcome only) error = %v, want ErrInvalidArgument", err)
	}
}

func TestListGateRequestsByAssessmentUsesRiskReadAccessContext(t *testing.T) {
	t.Parallel()

	riskAssessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	var captured AuthorizationRequest
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       systemClock{},
		IDGenerator: uuidGenerator{},
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			captured = request
			return nil
		}),
	})

	_, _, err := service.ListGateRequests(context.Background(), ListGateRequestsInput{
		Filter: query.GateRequestFilter{RiskAssessmentID: &riskAssessmentID},
		Meta:   QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("ListGateRequests(): %v", err)
	}
	if captured.ActionKey != actionRiskRead || captured.ResourceType != "governance_risk_assessment" || captured.ResourceID != riskAssessmentID.String() {
		t.Fatalf("access request = %+v, want risk read on assessment %s", captured, riskAssessmentID)
	}
	if repository.gateRequestListCalls != 1 {
		t.Fatalf("gate request list calls = %d, want 1", repository.gateRequestListCalls)
	}
}

func TestGetGateDecisionRequiresGateRequestForAuthorization(t *testing.T) {
	t.Parallel()

	decisionID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	service := newTestService(&fakeRepository{ready: true})

	_, err := service.GetGateDecision(context.Background(), GetGateDecisionInput{
		GateDecisionID: decisionID,
		Meta:           QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("GetGateDecision() error = %v, want ErrInvalidArgument", err)
	}
}

func TestGetGateDecisionRejectsMismatchedGateRequest(t *testing.T) {
	t.Parallel()

	decisionID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	otherGateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	service := newTestService(&fakeRepository{
		ready:        true,
		gateDecision: entity.GateDecision{ID: decisionID, GateRequestID: otherGateRequestID, DecisionActorRef: "user:owner"},
	})

	_, err := service.GetGateDecision(context.Background(), GetGateDecisionInput{
		GateDecisionID: decisionID,
		GateRequestID:  gateRequestID,
		Meta:           QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("GetGateDecision() error = %v, want ErrNotFound", err)
	}
}

func TestCancelGateReturnsNotFound(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	service := newTestService(&fakeRepository{ready: true, gateRequestErr: errs.ErrNotFound})

	_, err := service.CancelGate(context.Background(), CancelGateInput{
		GateRequestID: gateRequestID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("CancelGate() error = %v, want ErrNotFound", err)
	}
}

func TestReleaseDecisionLifecycleHappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	decisionID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	requestCommandID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	submitCommandID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	requestEventID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	submitEventID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	expectedPackageVersion := int64(1)
	expectedDecisionVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 1, CreatedAt: now, UpdatedAt: now},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha", ReleasePolicyRef: "policy:stable"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{decisionID, requestEventID, submitEventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	decision, pkg, err := service.RequestReleaseDecision(context.Background(), RequestReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		RequestGateIfRequired:    true,
		Meta: CommandMeta{
			CommandID:       &requestCommandID,
			ExpectedVersion: &expectedPackageVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RequestReleaseDecision(): %v", err)
	}
	if decision.Status != enum.ReleaseDecisionStatusRequested || pkg.Status != enum.ReleaseDecisionPackageStatusDecisionRequested || pkg.Version != 2 {
		t.Fatalf("decision/package = %+v/%+v, want requested package v2", decision, pkg)
	}
	if repository.events[0].EventType != governanceevents.EventReleaseDecisionRequested {
		t.Fatalf("event type = %q, want release decision requested", repository.events[0].EventType)
	}

	decision, pkg, err = service.SubmitReleaseDecision(context.Background(), SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Outcome:                  enum.ReleaseDecisionOutcomeHold,
		DecisionActorRef:         "user:release-owner",
		DecisionPolicyRef:        "policy:stable",
		Reason:                   "waiting for rollout window",
		Meta: CommandMeta{
			CommandID:       &submitCommandID,
			ExpectedVersion: &expectedDecisionVersion,
			Actor:           value.Actor{Type: "user", ID: "release-owner"},
		},
	})
	if err != nil {
		t.Fatalf("SubmitReleaseDecision(): %v", err)
	}
	if decision.Status != enum.ReleaseDecisionStatusResolved || decision.Outcome != enum.ReleaseDecisionOutcomeHold || decision.Version != 2 {
		t.Fatalf("decision = %+v, want resolved hold v2", decision)
	}
	if pkg.Status != enum.ReleaseDecisionPackageStatusClosed || pkg.Version != 3 {
		t.Fatalf("package = %+v, want closed v3", pkg)
	}
	if payload := string(repository.events[0].Payload); !strings.Contains(payload, `"safe_summary":"waiting for rollout window"`) || !strings.Contains(payload, `"decision_policy_ref":"policy:stable"`) {
		t.Fatalf("release decision event payload = %s, want bounded decision summary and policy ref", payload)
	} else if strings.Contains(payload, "raw_diff") || strings.Contains(payload, "provider_response") {
		t.Fatalf("release decision event leaked unsafe details: %s", payload)
	}
}

func TestReleaseDecisionBlocksGoWhenRiskRequiresGate(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	decisionID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			RiskAssessmentID:    &assessmentID,
			Status:              enum.ReleaseDecisionPackageStatusDecisionRequested,
		},
		releaseDecision: entity.ReleaseDecision{
			VersionedBase:            entity.VersionedBase{ID: decisionID, Version: 1},
			ReleaseDecisionPackageID: packageID,
			Status:                   enum.ReleaseDecisionStatusRequested,
		},
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			EffectiveRiskClass: enum.RiskClassR3,
		},
	}
	service := newTestService(repository)

	_, _, err := service.SubmitReleaseDecision(context.Background(), SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Outcome:                  enum.ReleaseDecisionOutcomeGo,
		DecisionActorRef:         "user:owner",
		Reason:                   "release",
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("SubmitReleaseDecision() error = %v, want ErrPreconditionFailed", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestReleaseDecisionBlocksGoWhenActiveSignalExists(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	decisionID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusDecisionRequested,
		},
		releaseDecision: entity.ReleaseDecision{
			VersionedBase:            entity.VersionedBase{ID: decisionID, Version: 1},
			ReleaseDecisionPackageID: packageID,
			Status:                   enum.ReleaseDecisionStatusRequested,
		},
		blockingSignals: []entity.BlockingSignal{{
			VersionedBase: entity.VersionedBase{ID: uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc"), Version: 1},
			Target:        value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
			Status:        enum.BlockingSignalStatusActive,
			Severity:      enum.SignalSeverityBlocking,
		}},
	}
	service := newTestService(repository)

	_, _, err := service.SubmitReleaseDecision(context.Background(), SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Outcome:                  enum.ReleaseDecisionOutcomeGoWithConditions,
		DecisionActorRef:         "user:owner",
		Reason:                   "release",
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("SubmitReleaseDecision() error = %v, want ErrPreconditionFailed", err)
	}
}

func TestReleaseDecisionExpectedVersionConflict(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	}
	service := newTestService(repository)

	_, _, err := service.RequestReleaseDecision(context.Background(), RequestReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RequestReleaseDecision() error = %v, want ErrConflict", err)
	}
}

func TestBuildReleaseDecisionPackageRejectsUnsafeEvidence(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		ProviderRefs:        []byte(`[{"pull_request_ref":"provider:pr:1","raw_provider_payload":"token=secret"}]`),
		EvidenceRefs: []value.EvidenceRef{{
			Kind:    "provider_check",
			Ref:     "provider:check:1",
			Summary: "bounded check summary",
		}},
		Meta: CommandMeta{
			CommandID: ptrUUID(uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")),
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("BuildReleaseDecisionPackage() error = %v, want ErrInvalidArgument", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestBuildReleaseDecisionPackageStoresIntegrationRefs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	eventID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	assessmentID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	gateDecisionID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	repository := &fakeRepository{
		ready:        true,
		assessment:   entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: assessmentID}},
		gateDecision: entity.GateDecision{ID: gateDecisionID},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{packageID, eventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	item, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		IntegrationRefs: []value.ReleaseIntegrationRef{
			{
				Domain:     " PROVIDER ",
				Kind:       " pull_request ",
				Ref:        " provider:pr:1 ",
				Status:     "checks_passed",
				Summary:    "bounded merge status",
				Digest:     "sha256:release-pr",
				ObservedAt: "2026-05-27T11:00:00Z",
				Version:    "provider-version:1",
			},
			{Domain: "governance", Kind: "risk_assessment", Ref: assessmentID.String()},
			{Domain: "governance", Kind: "gate_decision", Ref: gateDecisionID.String()},
		},
		Meta: CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("BuildReleaseDecisionPackage(): %v", err)
	}
	if item.ID != packageID || len(item.IntegrationRefs) != 3 {
		t.Fatalf("release package = %+v, want package with three integration refs", item)
	}
	var providerRef value.ReleaseIntegrationRef
	for _, ref := range item.IntegrationRefs {
		if ref.Domain == "provider" && ref.Kind == "pull_request" {
			providerRef = ref
		}
	}
	if providerRef.Ref != "provider:pr:1" {
		t.Fatalf("normalized provider ref = %+v", providerRef)
	}
	if item.IntegrationRefs[0].Domain != "governance" || item.IntegrationRefs[0].Kind != "gate_decision" {
		t.Fatalf("first integration ref = %+v, want canonical domain/kind/ref order", item.IntegrationRefs[0])
	}
	if repository.assessmentReads != 1 || repository.gateDecisionReads != 1 {
		t.Fatalf("local governance ref reads = assessment %d gate decision %d, want 1/1", repository.assessmentReads, repository.gateDecisionReads)
	}
	if len(repository.releasePackage.IntegrationRefs) != 3 {
		t.Fatalf("stored integration refs = %+v, want three refs", repository.releasePackage.IntegrationRefs)
	}
}

func TestBuildReleaseDecisionPackageEnrichesIntegrationRefs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	assessmentUpdatedAt := time.Date(2026, 5, 27, 11, 59, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	eventID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	assessmentID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 7, CreatedAt: assessmentUpdatedAt.Add(-time.Minute), UpdatedAt: assessmentUpdatedAt},
			Status:             enum.RiskAssessmentStatusActive,
			EffectiveRiskClass: enum.RiskClassR2,
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{packageID, eventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	item, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		IntegrationRefs: []value.ReleaseIntegrationRef{
			{Domain: "governance", Kind: "risk_assessment", Ref: assessmentID.String()},
			{Domain: "provider", Kind: "check", Ref: "provider:check:1"},
		},
		Meta: CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("BuildReleaseDecisionPackage(): %v", err)
	}
	var governanceRef value.ReleaseIntegrationRef
	var providerRef value.ReleaseIntegrationRef
	for _, ref := range item.IntegrationRefs {
		if ref.Domain == "governance" && ref.Kind == "risk_assessment" {
			governanceRef = ref
		}
		if ref.Domain == "provider" && ref.Kind == "check" {
			providerRef = ref
		}
	}
	if governanceRef.Status != "active" || governanceRef.Summary != "risk assessment active R2" || governanceRef.Version != "7" {
		t.Fatalf("governance ref = %+v, want enriched local risk snapshot", governanceRef)
	}
	if governanceRef.ObservedAt != "2026-05-27T11:59:00Z" || !strings.HasPrefix(governanceRef.Digest, "sha256:") {
		t.Fatalf("governance ref metadata = %+v, want digest and observed_at", governanceRef)
	}
	if providerRef.Status != "" || providerRef.Summary != "explicit_ref_unvalidated: provider check explicit ref retained; owner read client not connected" {
		t.Fatalf("provider ref = %+v, want explicit ref diagnostic", providerRef)
	}
	if providerRef.Digest != "" || providerRef.ObservedAt != "" || providerRef.Version != "" {
		t.Fatalf("provider ref metadata = %+v, want no fabricated owner snapshot", providerRef)
	}
	if payload := string(repository.events[0].Payload); strings.Contains(payload, "provider:check:1") || strings.Contains(payload, "risk assessment active") {
		t.Fatalf("release package event leaked enriched details: %s", payload)
	}
}

func TestBuildReleaseDecisionPackageRejectsConflictingLocalIntegrationSnapshot(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 7, UpdatedAt: time.Date(2026, 5, 27, 11, 59, 0, 0, time.UTC)},
			Status:             enum.RiskAssessmentStatusActive,
			EffectiveRiskClass: enum.RiskClassR2,
		},
	}
	service := newTestService(repository)

	_, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain: "governance",
			Kind:   "risk_assessment",
			Ref:    assessmentID.String(),
			Status: "draft",
		}},
		Meta: CommandMeta{
			CommandID: ptrUUID(uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")),
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("BuildReleaseDecisionPackage() error = %v, want ErrInvalidArgument", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestNormalizeReleaseIntegrationRefsCanonicalizesAndRejectsConflicts(t *testing.T) {
	t.Parallel()

	first := []value.ReleaseIntegrationRef{
		{Domain: "provider", Kind: "check", Ref: "provider:check:2", Status: "passed", Summary: "checks passed"},
		{Domain: "project", Kind: "repository", Ref: "repo:alpha", Version: "repository-version:2"},
		{Domain: "provider", Kind: "check", Ref: "provider:check:2", Status: "passed", Summary: "checks passed"},
	}
	second := []value.ReleaseIntegrationRef{
		{Domain: " PROVIDER ", Kind: " check ", Ref: " provider:check:2 ", Status: "passed", Summary: "checks passed"},
		{Domain: "PROJECT", Kind: "REPOSITORY", Ref: "repo:alpha", Version: "repository-version:2"},
	}
	normalizedFirst, err := normalizeReleaseIntegrationRefs(first)
	if err != nil {
		t.Fatalf("normalize first refs: %v", err)
	}
	normalizedSecond, err := normalizeReleaseIntegrationRefs(second)
	if err != nil {
		t.Fatalf("normalize second refs: %v", err)
	}
	if len(normalizedFirst) != len(normalizedSecond) || len(normalizedFirst) != 2 {
		t.Fatalf("normalized refs = %+v / %+v, want two canonical refs", normalizedFirst, normalizedSecond)
	}
	for index := range normalizedFirst {
		if normalizedFirst[index] != normalizedSecond[index] {
			t.Fatalf("normalized ref[%d] = %+v, want %+v", index, normalizedSecond[index], normalizedFirst[index])
		}
	}
	if normalizedFirst[0].Domain != "project" || normalizedFirst[1].Domain != "provider" {
		t.Fatalf("normalized order = %+v, want domain/kind/ref order", normalizedFirst)
	}

	_, err = normalizeReleaseIntegrationRefs([]value.ReleaseIntegrationRef{
		{Domain: "provider", Kind: "check", Ref: "provider:check:2", Status: "passed"},
		{Domain: "provider", Kind: "check", Ref: "provider:check:2", Status: "failed"},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("conflicting duplicate error = %v, want ErrInvalidArgument", err)
	}
}

func TestBuildReleaseDecisionPackageRejectsUnsafeIntegrationRef(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain:  "runtime",
			Kind:    "job",
			Ref:     "runtime:job:1",
			Summary: "stdout raw logs with token=secret",
		}},
		Meta: CommandMeta{
			CommandID: ptrUUID(uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")),
			Actor:     value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("BuildReleaseDecisionPackage() error = %v, want ErrInvalidArgument", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestRecordReleaseRuntimeEvidenceAppendsRefsAndPublishesSafeEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	eventID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	expectedVersion := int64(3)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha", RepositoryRef: "repo:alpha"},
			RuntimeRefs:         []byte(`[{"job_ref":"runtime:job:build"}]`),
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{eventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	item, err := service.RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		RuntimeRefs:              []byte(`[{"job_ref":"runtime:job:deploy","summary_ref":"runtime:summary:deploy"}]`),
		EvidenceRefs: []value.EvidenceRef{{
			Kind:           "runtime_job",
			Ref:            "runtime:job:deploy",
			Summary:        "deploy job status",
			Digest:         "sha256:deploystatus",
			RetentionClass: "safe_ref",
		}},
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain:     "runtime",
			Kind:       "deploy",
			Ref:        "runtime:job:deploy",
			Status:     "failed",
			Summary:    "deploy failed with bounded diagnostic",
			Digest:     "sha256:deploystatus",
			ObservedAt: "2026-05-28T11:59:00Z",
			Version:    "job-version:4",
			ErrorCode:  "DEPLOY_HEALTHCHECK_FAILED",
		}},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseRuntimeEvidence(): %v", err)
	}
	if item.Version != 4 || len(item.EvidenceRefs) != 1 || len(item.IntegrationRefs) != 1 {
		t.Fatalf("item = %+v, want updated package with evidence and integration ref", item)
	}
	if item.IntegrationRefs[0].ErrorCode != "DEPLOY_HEALTHCHECK_FAILED" {
		t.Fatalf("runtime error code = %q, want bounded owner code", item.IntegrationRefs[0].ErrorCode)
	}
	if !strings.Contains(string(item.RuntimeRefs), "runtime:job:build") || !strings.Contains(string(item.RuntimeRefs), "runtime:job:deploy") {
		t.Fatalf("runtime refs = %s, want existing and new refs", string(item.RuntimeRefs))
	}
	if payload := string(repository.events[0].Payload); !strings.Contains(payload, `"runtime_job_ref":"runtime:job:deploy"`) || strings.Contains(payload, "DEPLOY_HEALTHCHECK_FAILED") || strings.Contains(payload, "deploy failed with bounded diagnostic") {
		t.Fatalf("runtime evidence event payload = %s, want safe ref without raw evidence details", payload)
	}
}

func TestRecordReleaseAgentEvidenceAppendsRefsAndPublishesSafeEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	reviewSignalID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	eventID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	expectedVersion := int64(3)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha", RepositoryRef: "repo:alpha"},
			AgentContext:        []byte(`{"runRef":"agent:run:reviewer","sessionRef":"agent:session:1"}`),
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
		reviewSignal: entity.ReviewSignal{
			ID:        reviewSignalID,
			Outcome:   enum.ReviewSignalOutcomePass,
			Severity:  enum.SignalSeverityInfo,
			CreatedAt: now.Add(-2 * time.Minute),
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{eventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	item, err := service.RecordReleaseAgentEvidence(context.Background(), RecordReleaseAgentEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		AgentContext:             []byte(`{"acceptanceRef":"agent:acceptance:qa","runRef":"agent:run:reviewer"}`),
		EvidenceRefs: []value.EvidenceRef{{
			Kind:           "agent_acceptance",
			Ref:            "agent:acceptance:qa",
			Summary:        "acceptance passed",
			Digest:         "sha256:acceptance",
			RetentionClass: "safe_ref",
		}},
		IntegrationRefs: []value.ReleaseIntegrationRef{
			{
				Domain:     "agent",
				Kind:       "acceptance",
				Ref:        "agent:acceptance:qa",
				Status:     "passed",
				Summary:    "acceptance passed",
				Digest:     "sha256:acceptance",
				ObservedAt: "2026-05-28T11:50:00Z",
				Version:    "acceptance-version:4",
			},
			{
				Domain:  "agent",
				Kind:    "run",
				Ref:     "agent:run:reviewer",
				Status:  "completed",
				Summary: "reviewer run completed",
				Digest:  "sha256:run",
				Version: "run-version:8",
			},
			{
				Domain: "runtime",
				Kind:   "job",
				Ref:    "runtime:job:agent-reviewer",
				Status: "succeeded",
				Digest: "sha256:runtimejob",
			},
			{
				Domain: "governance",
				Kind:   "review_signal",
				Ref:    reviewSignalID.String(),
			},
		},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseAgentEvidence(): %v", err)
	}
	if item.Version != 4 || len(item.EvidenceRefs) != 1 || len(item.IntegrationRefs) != 4 {
		t.Fatalf("item = %+v, want updated package with agent evidence", item)
	}
	if agentContext := string(item.AgentContext); !strings.Contains(agentContext, "agent:session:1") || !strings.Contains(agentContext, "agent:acceptance:qa") {
		t.Fatalf("agent context = %s, want merged session/run/acceptance refs", agentContext)
	}
	if repository.events[0].EventType != governanceevents.EventReleaseDecisionPackageAgentEvidenceRecorded {
		t.Fatalf("event type = %q, want agent evidence event", repository.events[0].EventType)
	}
	payload := string(repository.events[0].Payload)
	if !strings.Contains(payload, `"agent_acceptance_ref":"agent:acceptance:qa"`) ||
		!strings.Contains(payload, `"agent_run_ref":"agent:run:reviewer"`) ||
		!strings.Contains(payload, `"runtime_job_ref":"runtime:job:agent-reviewer"`) {
		t.Fatalf("agent evidence event payload = %s, want safe refs", payload)
	}
	for _, unsafe := range []string{"acceptance passed", "reviewer run completed", "sha256:acceptance", "stdout", "stderr", "workspace"} {
		if strings.Contains(payload, unsafe) {
			t.Fatalf("agent evidence event payload leaked %q: %s", unsafe, payload)
		}
	}
}

func TestRecordReleaseRuntimeEvidenceReplayAndConflictHandling(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	commandID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	repository := &fakeRepository{
		ready:            true,
		hasCommandResult: true,
		commandResult: commandResult(CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "runtime-manager"},
		}, enum.OperationRecordReleaseRuntimeEvidence.String(), governanceevents.AggregateReleaseDecisionPackage, packageID, now),
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 4},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			IntegrationRefs: []value.ReleaseIntegrationRef{{
				Domain: "runtime",
				Kind:   "job",
				Ref:    "runtime:job:deploy",
				Status: "succeeded",
				Digest: "sha256:deploystatus",
			}},
			Status: enum.ReleaseDecisionPackageStatusReady,
		},
	}
	service := newTestService(repository)

	item, err := service.RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain: "runtime",
			Kind:   "job",
			Ref:    "runtime:job:deploy",
			Status: "succeeded",
			Digest: "sha256:deploystatus",
		}},
		Meta: CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseRuntimeEvidence(replay): %v", err)
	}
	if item.Version != 4 || repository.mutationCalls != 0 {
		t.Fatalf("replayed item = %+v, mutation calls = %d, want no-op replay", item, repository.mutationCalls)
	}

	_, err = service.RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain: "runtime",
			Kind:   "job",
			Ref:    "runtime:job:deploy",
			Status: "failed",
			Digest: "sha256:different",
		}},
		Meta: CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordReleaseRuntimeEvidence(conflict replay) error = %v, want ErrConflict", err)
	}
}

func TestRecordReleaseRuntimeEvidenceDuplicateFingerprintIsIdempotent(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	commandID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	expectedVersion := int64(4)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			RuntimeRefs:         []byte(`[{"job_ref":"runtime:job:deploy","summary_ref":"runtime:summary:deploy"}]`),
			EvidenceRefs: []value.EvidenceRef{{
				Kind:           "runtime_job",
				Ref:            "runtime:job:deploy",
				Summary:        "deploy job status",
				Digest:         "sha256:deploystatus",
				RetentionClass: "safe_ref",
			}},
			IntegrationRefs: []value.ReleaseIntegrationRef{{
				Domain:     "runtime",
				Kind:       "job",
				Ref:        "runtime:job:deploy",
				Status:     "succeeded",
				Summary:    "deploy completed",
				Digest:     "sha256:deploystatus",
				ObservedAt: "2026-05-28T12:00:00Z",
				Version:    "job-version:4",
			}},
			Status: enum.ReleaseDecisionPackageStatusReady,
		},
	}

	item, err := newTestService(repository).RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		RuntimeRefs:              []byte(`[{"job_ref":"runtime:job:deploy","summary_ref":"runtime:summary:deploy"}]`),
		EvidenceRefs: []value.EvidenceRef{{
			Kind:           "runtime_job",
			Ref:            "runtime:job:deploy",
			Summary:        "deploy job status",
			Digest:         "sha256:deploystatus",
			RetentionClass: "safe_ref",
		}},
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain:     "runtime",
			Kind:       "job",
			Ref:        "runtime:job:deploy",
			Status:     "succeeded",
			Summary:    "deploy completed",
			Digest:     "sha256:deploystatus",
			ObservedAt: "2026-05-28T12:00:00Z",
			Version:    "job-version:4",
		}},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseRuntimeEvidence(duplicate fingerprint): %v", err)
	}
	if item.Version != expectedVersion || repository.mutationCalls != 0 || len(repository.events) != 0 {
		t.Fatalf("duplicate result version/mutations/events = %d/%d/%d, want %d/0/0", item.Version, repository.mutationCalls, len(repository.events), expectedVersion)
	}
	if repository.result.AggregateID != packageID || repository.result.CommandID == nil || *repository.result.CommandID != commandID {
		t.Fatalf("command result = %+v, want idempotent command result for package", repository.result)
	}
}

func TestRecordReleaseAgentEvidenceVerifiesGovernanceRefs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	reviewSignalID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	expectedVersion := int64(3)
	basePackage := entity.ReleaseDecisionPackage{
		VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		Status:              enum.ReleaseDecisionPackageStatusReady,
	}

	for _, tc := range []struct {
		name       string
		repository *fakeRepository
		ref        value.ReleaseIntegrationRef
		want       error
	}{
		{
			name: "unknown local governance ref",
			repository: &fakeRepository{
				ready:           true,
				releasePackage:  basePackage,
				reviewSignalErr: errs.ErrNotFound,
			},
			ref:  value.ReleaseIntegrationRef{Domain: "governance", Kind: "review_signal", Ref: reviewSignalID.String()},
			want: errs.ErrNotFound,
		},
		{
			name: "mismatched local governance snapshot",
			repository: &fakeRepository{
				ready:          true,
				releasePackage: basePackage,
				reviewSignal: entity.ReviewSignal{
					ID:        reviewSignalID,
					Outcome:   enum.ReviewSignalOutcomePass,
					Severity:  enum.SignalSeverityInfo,
					CreatedAt: now,
				},
			},
			ref:  value.ReleaseIntegrationRef{Domain: "governance", Kind: "review_signal", Ref: reviewSignalID.String(), Status: string(enum.ReviewSignalOutcomeBlock)},
			want: errs.ErrInvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestService(tc.repository).RecordReleaseAgentEvidence(context.Background(), RecordReleaseAgentEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				IntegrationRefs:          []value.ReleaseIntegrationRef{tc.ref},
				Meta: CommandMeta{
					CommandID:       ptrUUID(uuid.New()),
					ExpectedVersion: &expectedVersion,
					Actor:           value.Actor{Type: "service", ID: "agent-manager"},
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RecordReleaseAgentEvidence() error = %v, want %v", err, tc.want)
			}
			if tc.repository.mutationCalls != 0 || len(tc.repository.events) != 0 {
				t.Fatalf("mutations/events = %d/%d, want none", tc.repository.mutationCalls, len(tc.repository.events))
			}
		})
	}
}

func TestRecordReleaseAgentEvidenceReplayKeepsStoredGovernanceSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	commandID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	expectedVersion := int64(3)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)},
			Target:        value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
			Status:        enum.GateRequestStatusAwaitingDecision,
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")}},
		Authorizer:  AllowAllAuthorizer{},
	})
	input := RecordReleaseAgentEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		IntegrationRefs:          []value.ReleaseIntegrationRef{{Domain: "governance", Kind: "gate_request", Ref: gateRequestID.String()}},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	}

	item, err := service.RecordReleaseAgentEvidence(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordReleaseAgentEvidence(): %v", err)
	}
	if len(item.IntegrationRefs) != 1 || item.IntegrationRefs[0].Status != string(enum.GateRequestStatusAwaitingDecision) {
		t.Fatalf("integration refs = %+v, want first authoritative gate snapshot", item.IntegrationRefs)
	}
	repository.hasCommandResult = true
	repository.commandResult = repository.result
	repository.gateRequest.Status = enum.GateRequestStatusResolved
	repository.gateRequest.Version = 2
	repository.gateRequest.UpdatedAt = now
	mutationCalls := repository.mutationCalls
	eventCount := len(repository.events)

	replayed, err := service.RecordReleaseAgentEvidence(context.Background(), input)
	if err != nil {
		t.Fatalf("RecordReleaseAgentEvidence(replay): %v", err)
	}
	if replayed.Version != item.Version || len(replayed.IntegrationRefs) != 1 || replayed.IntegrationRefs[0].Status != string(enum.GateRequestStatusAwaitingDecision) {
		t.Fatalf("replayed package = %+v, want stored first-write snapshot", replayed)
	}
	if repository.mutationCalls != mutationCalls || len(repository.events) != eventCount {
		t.Fatalf("replay mutations/events = %d/%d, want %d/%d", repository.mutationCalls, len(repository.events), mutationCalls, eventCount)
	}
}

func TestRecordReleaseAgentEvidenceDuplicateFingerprintIsIdempotent(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	commandID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	expectedVersion := int64(4)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			AgentContext:        []byte(`{"acceptanceRef":"agent:acceptance:qa","runRef":"agent:run:reviewer"}`),
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
				ObservedAt: "2026-05-28T12:00:00Z",
				Version:    "acceptance-version:4",
			}},
			Status: enum.ReleaseDecisionPackageStatusReady,
		},
	}

	item, err := newTestService(repository).RecordReleaseAgentEvidence(context.Background(), RecordReleaseAgentEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		AgentContext:             []byte(`{"acceptanceRef":"agent:acceptance:qa","runRef":"agent:run:reviewer"}`),
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
			ObservedAt: "2026-05-28T12:00:00Z",
			Version:    "acceptance-version:4",
		}},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseAgentEvidence(duplicate fingerprint): %v", err)
	}
	if item.Version != expectedVersion || repository.mutationCalls != 0 || len(repository.events) != 0 {
		t.Fatalf("duplicate result version/mutations/events = %d/%d/%d, want %d/0/0", item.Version, repository.mutationCalls, len(repository.events), expectedVersion)
	}
	if repository.result.AggregateID != packageID || repository.result.CommandID == nil || *repository.result.CommandID != commandID {
		t.Fatalf("command result = %+v, want idempotent command result for package", repository.result)
	}
}

func TestRecordReleaseRuntimeEvidenceRejectsConflictingFingerprintAndStaleStatus(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	expectedVersion := int64(4)
	basePackage := entity.ReleaseDecisionPackage{
		VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain:     "runtime",
			Kind:       "job",
			Ref:        "runtime:job:deploy",
			Status:     "succeeded",
			Summary:    "deploy completed",
			Digest:     "sha256:deploystatus",
			ObservedAt: "2026-05-28T12:00:00Z",
			Version:    "job-version:4",
		}},
		Status: enum.ReleaseDecisionPackageStatusReady,
	}

	for _, tc := range []struct {
		name string
		ref  value.ReleaseIntegrationRef
		want error
	}{
		{
			name: "conflicting digest",
			ref: value.ReleaseIntegrationRef{
				Domain:     "runtime",
				Kind:       "job",
				Ref:        "runtime:job:deploy",
				Status:     "succeeded",
				Summary:    "deploy completed",
				Digest:     "sha256:different",
				ObservedAt: "2026-05-28T12:00:00Z",
				Version:    "job-version:4",
			},
			want: errs.ErrConflict,
		},
		{
			name: "stale status",
			ref: value.ReleaseIntegrationRef{
				Domain:     "runtime",
				Kind:       "job",
				Ref:        "runtime:job:deploy",
				Status:     "running",
				Summary:    "deploy still running",
				Digest:     "sha256:deploystatus",
				ObservedAt: "2026-05-28T12:00:00Z",
				Version:    "job-version:4",
			},
			want: errs.ErrPreconditionFailed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &fakeRepository{ready: true, releasePackage: basePackage}
			_, err := newTestService(repository).RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				IntegrationRefs:          []value.ReleaseIntegrationRef{tc.ref},
				Meta: CommandMeta{
					CommandID:       ptrUUID(uuid.New()),
					ExpectedVersion: &expectedVersion,
					Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RecordReleaseRuntimeEvidence() error = %v, want %v", err, tc.want)
			}
			if repository.mutationCalls != 0 || len(repository.events) != 0 {
				t.Fatalf("mutation calls/events = %d/%d, want 0/0", repository.mutationCalls, len(repository.events))
			}
		})
	}
}

func TestRecordReleaseAgentEvidenceRejectsConflictingFingerprintAndStaleStatus(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	expectedVersion := int64(4)
	basePackage := entity.ReleaseDecisionPackage{
		VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
		ReleaseCandidateRef: "release:v1.0.0",
		ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		IntegrationRefs: []value.ReleaseIntegrationRef{{
			Domain:     "agent",
			Kind:       "acceptance",
			Ref:        "agent:acceptance:qa",
			Status:     "passed",
			Summary:    "acceptance passed",
			Digest:     "sha256:acceptance",
			ObservedAt: "2026-05-28T12:00:00Z",
			Version:    "acceptance-version:4",
		}},
		Status: enum.ReleaseDecisionPackageStatusReady,
	}

	for _, tc := range []struct {
		name string
		ref  value.ReleaseIntegrationRef
		want error
	}{
		{
			name: "conflicting digest",
			ref: value.ReleaseIntegrationRef{
				Domain:     "agent",
				Kind:       "acceptance",
				Ref:        "agent:acceptance:qa",
				Status:     "passed",
				Summary:    "acceptance passed",
				Digest:     "sha256:different",
				ObservedAt: "2026-05-28T12:00:00Z",
				Version:    "acceptance-version:4",
			},
			want: errs.ErrConflict,
		},
		{
			name: "stale status",
			ref: value.ReleaseIntegrationRef{
				Domain:     "agent",
				Kind:       "acceptance",
				Ref:        "agent:acceptance:qa",
				Status:     "waiting",
				Summary:    "acceptance still waiting",
				Digest:     "sha256:acceptance",
				ObservedAt: "2026-05-28T12:00:00Z",
				Version:    "acceptance-version:4",
			},
			want: errs.ErrPreconditionFailed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &fakeRepository{ready: true, releasePackage: basePackage}
			_, err := newTestService(repository).RecordReleaseAgentEvidence(context.Background(), RecordReleaseAgentEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				IntegrationRefs:          []value.ReleaseIntegrationRef{tc.ref},
				Meta: CommandMeta{
					CommandID:       ptrUUID(uuid.New()),
					ExpectedVersion: &expectedVersion,
					Actor:           value.Actor{Type: "service", ID: "agent-manager"},
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RecordReleaseAgentEvidence() error = %v, want %v", err, tc.want)
			}
			if repository.mutationCalls != 0 || len(repository.events) != 0 {
				t.Fatalf("mutation calls/events = %d/%d, want 0/0", repository.mutationCalls, len(repository.events))
			}
		})
	}
}

func TestRecordReleaseRuntimeEvidenceRejectsUnsafeEvidenceMetadata(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	expectedVersion := int64(3)
	baseRepository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: expectedVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	}

	for _, tc := range []struct {
		name        string
		evidenceRef value.EvidenceRef
	}{
		{
			name: "unsafe digest",
			evidenceRef: value.EvidenceRef{
				Kind:           "runtime_job",
				Ref:            "runtime:job:deploy",
				Summary:        "deploy job status",
				Digest:         "secret=runtime-token",
				RetentionClass: "safe_ref",
			},
		},
		{
			name: "unsafe retention class",
			evidenceRef: value.EvidenceRef{
				Kind:           "runtime_job",
				Ref:            "runtime:job:deploy",
				Summary:        "deploy job status",
				Digest:         "sha256:deploy",
				RetentionClass: "kubeconfig",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := *baseRepository
			_, err := newTestService(&repository).RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				EvidenceRefs:             []value.EvidenceRef{tc.evidenceRef},
				IntegrationRefs:          []value.ReleaseIntegrationRef{{Domain: "runtime", Kind: "job", Ref: "runtime:job:deploy", Status: "running"}},
				Meta: CommandMeta{
					CommandID:       ptrUUID(uuid.New()),
					ExpectedVersion: &expectedVersion,
					Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
				},
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("RecordReleaseRuntimeEvidence() error = %v, want ErrInvalidArgument", err)
			}
			if repository.mutationCalls != 0 || len(repository.events) != 0 {
				t.Fatalf("mutation calls/events = %d/%d, want 0/0", repository.mutationCalls, len(repository.events))
			}
		})
	}
}

func TestRecordReleaseRuntimeEvidenceRejectsStaleUnknownAndUnsafeInputs(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	staleVersion := int64(2)
	currentVersion := int64(3)
	service := newTestService(&fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: currentVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	})

	for _, tc := range []struct {
		name            string
		expectedVersion *int64
		ref             value.ReleaseIntegrationRef
		want            error
	}{
		{
			name:            "stale version",
			expectedVersion: &staleVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "runtime", Kind: "job", Ref: "runtime:job:1"},
			want:            errs.ErrPreconditionFailed,
		},
		{
			name:            "unknown source",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "provider", Kind: "check", Ref: "provider:check:1"},
			want:            errs.ErrInvalidArgument,
		},
		{
			name:            "unknown status",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "runtime", Kind: "job", Ref: "runtime:job:1", Status: "completed"},
			want:            errs.ErrInvalidArgument,
		},
		{
			name:            "unsafe summary",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "runtime", Kind: "job", Ref: "runtime:job:1", Summary: "raw logs token=secret"},
			want:            errs.ErrInvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				IntegrationRefs:          []value.ReleaseIntegrationRef{tc.ref},
				Meta: CommandMeta{
					CommandID:       ptrUUID(uuid.New()),
					ExpectedVersion: tc.expectedVersion,
					Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RecordReleaseRuntimeEvidence() error = %v, want %v", err, tc.want)
			}
		})
	}

	closedService := newTestService(&fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: currentVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusClosed,
		},
	})
	_, err := closedService.RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		IntegrationRefs:          []value.ReleaseIntegrationRef{{Domain: "runtime", Kind: "job", Ref: "runtime:job:1"}},
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.New()),
			ExpectedVersion: &currentVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("RecordReleaseRuntimeEvidence(closed) error = %v, want ErrPreconditionFailed", err)
	}
}

func TestRecordReleaseAgentEvidenceRejectsStaleUnknownUnsafeAndClosedInputs(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	staleVersion := int64(2)
	currentVersion := int64(3)
	service := newTestService(&fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: currentVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	})

	for _, tc := range []struct {
		name            string
		expectedVersion *int64
		ref             value.ReleaseIntegrationRef
		want            error
	}{
		{
			name:            "stale version",
			expectedVersion: &staleVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "agent", Kind: "acceptance", Ref: "agent:acceptance:qa"},
			want:            errs.ErrPreconditionFailed,
		},
		{
			name:            "unknown source",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "provider", Kind: "review", Ref: "provider:review:1"},
			want:            errs.ErrInvalidArgument,
		},
		{
			name:            "unknown status",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "agent", Kind: "acceptance", Ref: "agent:acceptance:qa", Status: "done"},
			want:            errs.ErrInvalidArgument,
		},
		{
			name:            "unsafe summary",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "agent", Kind: "acceptance", Ref: "agent:acceptance:qa", Summary: "prompt transcript token=secret"},
			want:            errs.ErrInvalidArgument,
		},
		{
			name:            "unsafe agent context",
			expectedVersion: &currentVersion,
			ref:             value.ReleaseIntegrationRef{Domain: "agent", Kind: "acceptance", Ref: "agent:acceptance:qa"},
			want:            errs.ErrInvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentContext := []byte(nil)
			if tc.name == "unsafe agent context" {
				agentContext = []byte(`{"workspace_path":"/workspace/secret"}`)
			}
			_, err := service.RecordReleaseAgentEvidence(context.Background(), RecordReleaseAgentEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				AgentContext:             agentContext,
				IntegrationRefs:          []value.ReleaseIntegrationRef{tc.ref},
				Meta: CommandMeta{
					CommandID:       ptrUUID(uuid.New()),
					ExpectedVersion: tc.expectedVersion,
					Actor:           value.Actor{Type: "service", ID: "agent-manager"},
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RecordReleaseAgentEvidence() error = %v, want %v", err, tc.want)
			}
		})
	}

	closedService := newTestService(&fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: currentVersion},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusClosed,
		},
	})
	_, err := closedService.RecordReleaseAgentEvidence(context.Background(), RecordReleaseAgentEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		IntegrationRefs:          []value.ReleaseIntegrationRef{{Domain: "agent", Kind: "acceptance", Ref: "agent:acceptance:qa"}},
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.New()),
			ExpectedVersion: &currentVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("RecordReleaseAgentEvidence(closed) error = %v, want ErrPreconditionFailed", err)
	}
}

func TestRecordReleaseRuntimeEvidenceAccessDeniedBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	expectedVersion := int64(3)
	commandID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	_, err := service.RecordReleaseRuntimeEvidence(context.Background(), RecordReleaseRuntimeEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		IntegrationRefs:          []value.ReleaseIntegrationRef{{Domain: "runtime", Kind: "job", Ref: "runtime:job:1"}},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("RecordReleaseRuntimeEvidence() error = %v, want ErrForbidden", err)
	}
	if repository.commandResultReads != 0 || repository.releasePackageReads != 0 || repository.mutationCalls != 0 {
		t.Fatalf("repository calls = command:%d package:%d mutation:%d, want 0/0/0 before access allow", repository.commandResultReads, repository.releasePackageReads, repository.mutationCalls)
	}
}

func TestReleaseDecisionPackageReadSurfaceReturnsRuntimeDeployEvidence(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	gateDecisionID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 5, CreatedAt: time.Date(2026, 5, 28, 11, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha", RepositoryRef: "repo:alpha", ReleaseLineRef: "release-line:stable"},
			RuntimeRefs:         []byte(`[{"job_ref":"runtime:job:deploy","summary_ref":"runtime:summary:deploy","artifact_ref":"runtime:artifact:image"}]`),
			EvidenceRefs: []value.EvidenceRef{{
				Kind:           "runtime_job",
				Ref:            "runtime:job:deploy",
				Summary:        "deploy job status summary",
				Digest:         "sha256:deploy",
				RetentionClass: "safe_ref",
			}},
			IntegrationRefs: []value.ReleaseIntegrationRef{
				{Domain: "governance", Kind: "gate_request", Ref: gateRequestID.String(), Status: "resolved", Summary: "gate request resolved", Digest: "sha256:gate-request", ObservedAt: "2026-05-28T11:30:00Z", Version: "2"},
				{Domain: "governance", Kind: "gate_decision", Ref: gateDecisionID.String(), Status: "approve", Summary: "gate decision approved", Digest: "sha256:gate-decision", ObservedAt: "2026-05-28T11:31:00Z"},
				{Domain: "runtime", Kind: "deploy", Ref: "runtime:job:deploy", Status: "failed", Summary: "deploy failed with bounded diagnostic", Digest: "sha256:deploy", ObservedAt: "2026-05-28T11:59:00Z", Version: "job-version:5", ErrorCode: "DEPLOY_HEALTHCHECK_FAILED"},
			},
			Status: enum.ReleaseDecisionPackageStatusReady,
		},
	}

	item, err := newTestService(repository).GetReleaseDecisionPackage(context.Background(), GetReleaseDecisionPackageInput{
		ReleaseDecisionPackageID: packageID,
		Meta:                     QueryMeta{Actor: value.Actor{Type: "service", ID: "staff-gateway"}},
	})
	if err != nil {
		t.Fatalf("GetReleaseDecisionPackage(): %v", err)
	}
	if item.ReleaseCandidateRef != "release:v1.0.0" || item.Version != 5 || !strings.Contains(string(item.RuntimeRefs), "runtime:job:deploy") {
		t.Fatalf("read package = %+v, want release candidate, version and runtime job ref", item)
	}
	if len(item.EvidenceRefs) != 1 || item.EvidenceRefs[0].Digest != "sha256:deploy" {
		t.Fatalf("evidence refs = %+v, want bounded runtime evidence digest", item.EvidenceRefs)
	}
	if len(item.IntegrationRefs) != 3 || item.IntegrationRefs[2].ErrorCode != "DEPLOY_HEALTHCHECK_FAILED" || item.IntegrationRefs[0].Kind != "gate_request" || item.IntegrationRefs[1].Kind != "gate_decision" {
		t.Fatalf("integration refs = %+v, want gate and runtime/deploy read surface", item.IntegrationRefs)
	}
	readSurface := string(item.RuntimeRefs) + " " + item.EvidenceRefs[0].Summary + " " + item.IntegrationRefs[2].Summary + " " + item.IntegrationRefs[2].ErrorCode
	for _, unsafe := range []string{"token=", "kubeconfig", "stdout", "stderr", "raw_provider_payload"} {
		if strings.Contains(readSurface, unsafe) {
			t.Fatalf("read surface leaked unsafe marker %q: %s", unsafe, readSurface)
		}
	}
}

func TestReleaseReadAccessDeniedBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	_, err := service.GetReleaseDecisionPackage(context.Background(), GetReleaseDecisionPackageInput{
		ReleaseDecisionPackageID: packageID,
		Meta:                     QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetReleaseDecisionPackage() error = %v, want ErrForbidden", err)
	}
	if repository.releasePackageReads != 0 {
		t.Fatalf("release package reads = %d, want 0 before access allow", repository.releasePackageReads)
	}
}

func TestReleaseListRejectsUnappliedAccessContexts(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)
	meta := QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}

	_, _, err := service.ListReleaseDecisionPackages(context.Background(), ListReleaseDecisionPackagesInput{
		Filter: query.ReleaseDecisionPackageFilter{ProjectContext: value.ProjectContextRef{RepositoryRef: "repo:alpha"}},
		Meta:   meta,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListReleaseDecisionPackages(repository only) error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListReleaseDecisionPackages(context.Background(), ListReleaseDecisionPackagesInput{
		Filter: query.ReleaseDecisionPackageFilter{ProjectContext: value.ProjectContextRef{ReleaseLineRef: "release-line:stable"}},
		Meta:   meta,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListReleaseDecisionPackages(release line only) error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListReleaseDecisions(context.Background(), ListReleaseDecisionsInput{
		Filter: query.ReleaseDecisionFilter{ProjectContext: value.ProjectContextRef{RepositoryRef: "repo:alpha"}},
		Meta:   meta,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListReleaseDecisions(repository only) error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListReleaseDecisions(context.Background(), ListReleaseDecisionsInput{
		Filter: query.ReleaseDecisionFilter{ProjectContext: value.ProjectContextRef{ReleaseLineRef: "release-line:stable"}},
		Meta:   meta,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListReleaseDecisions(release line only) error = %v, want ErrInvalidArgument", err)
	}
	if repository.releasePackageListCalls != 0 || repository.releaseDecisionListCalls != 0 {
		t.Fatalf("list calls = packages:%d decisions:%d, want 0 before valid access context", repository.releasePackageListCalls, repository.releaseDecisionListCalls)
	}
}

func TestBlockingSignalLifecycleAndSafeEventPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	signalID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	resolveCommandID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	recordEventID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	resolveEventID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	expectedVersion := int64(1)
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{signalID, recordEventID, resolveEventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	signal, err := service.RecordBlockingSignal(context.Background(), RecordBlockingSignalInput{
		Target:     value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
		SourceType: enum.BlockingSignalSourceTypeRuntime,
		SourceRef:  "runtime:job:1",
		Severity:   enum.SignalSeverityCritical,
		Summary:    "safe bounded summary without logs",
		Meta:       CommandMeta{IdempotencyKey: "runtime raw idempotency key", Actor: value.Actor{Type: "service", ID: "runtime-manager"}},
	})
	if err != nil {
		t.Fatalf("RecordBlockingSignal(): %v", err)
	}
	if signal.Status != enum.BlockingSignalStatusActive || signal.Version != 1 {
		t.Fatalf("signal = %+v, want active v1", signal)
	}
	if payload := string(repository.events[0].Payload); !strings.Contains(payload, `"safe_summary":"safe bounded summary without logs"`) || !strings.Contains(payload, `"source_ref":"runtime:job:1"`) || !strings.Contains(payload, `"idempotency_key":"idempotency_sha256:`) {
		t.Fatalf("blocking signal event payload = %s, want bounded summary and source ref", payload)
	} else if strings.Contains(payload, "runtime raw idempotency key") || strings.Contains(payload, "stdout") || strings.Contains(payload, "stderr") {
		t.Fatalf("blocking signal event leaked unsafe details: %s", payload)
	}
	signal, err = service.ResolveBlockingSignal(context.Background(), ResolveBlockingSignalInput{
		BlockingSignalID:  signalID,
		TerminalStatus:    enum.BlockingSignalStatusResolved,
		ResolutionSummary: "fixed",
		Meta: CommandMeta{
			CommandID:       &resolveCommandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveBlockingSignal(): %v", err)
	}
	if signal.Status != enum.BlockingSignalStatusResolved || signal.ResolvedAt == nil || signal.Version != 2 {
		t.Fatalf("resolved signal = %+v, want resolved v2", signal)
	}
	if payload := string(repository.events[0].Payload); !strings.Contains(payload, `"reason_code":"resolved"`) {
		t.Fatalf("resolved signal event payload = %s, want reason_code", payload)
	}
}

func TestReleaseSafetyStateCreateAndUpdate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	stateID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	createCommandID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	updateCommandID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	createEventID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	updateEventID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 3},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		},
		blockingSignals: []entity.BlockingSignal{{
			VersionedBase: entity.VersionedBase{ID: uuid.MustParse("99999999-9999-4999-8999-999999999999"), Version: 1},
			Target:        value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
			Status:        enum.BlockingSignalStatusActive,
			Severity:      enum.SignalSeverityBlocking,
		}},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{stateID, createEventID, updateEventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	state, err := service.RecordReleaseSafetyState(context.Background(), RecordReleaseSafetyStateInput{
		ReleaseDecisionPackageID: packageID,
		CurrentState:             enum.ReleaseSafetyStateKindPostdeployObservation,
		LastStateReason:          "postdeploy observation",
		Meta:                     CommandMeta{CommandID: &createCommandID, Actor: value.Actor{Type: "service", ID: "runtime-manager"}},
	})
	if err != nil {
		t.Fatalf("RecordReleaseSafetyState(create): %v", err)
	}
	if state.Version != 1 || state.BlockingSignalCount != 1 {
		t.Fatalf("state = %+v, want v1 with one active blocker", state)
	}
	if payload := string(repository.events[0].Payload); !strings.Contains(payload, `"previous_status":"none"`) || !strings.Contains(payload, `"reason_code":"created"`) {
		t.Fatalf("created safety event payload = %s, want previous_status and reason_code", payload)
	}
	state, err = service.RecordReleaseSafetyState(context.Background(), RecordReleaseSafetyStateInput{
		ReleaseDecisionPackageID: packageID,
		CurrentState:             enum.ReleaseSafetyStateKindStable,
		RuntimeJobRef:            "runtime:job:stable",
		LastStateReason:          "postdeploy healthy",
		Meta: CommandMeta{
			CommandID:       &updateCommandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseSafetyState(update): %v", err)
	}
	if state.Version != 2 || state.CurrentState != enum.ReleaseSafetyStateKindStable {
		t.Fatalf("state = %+v, want stable v2", state)
	}
	if payload := string(repository.events[0].Payload); !strings.Contains(payload, `"previous_status":"postdeploy_observation"`) || !strings.Contains(payload, `"reason_code":"stable"`) {
		t.Fatalf("updated safety event payload = %s, want previous_status and reason_code", payload)
	}
}

func TestReleaseSafetyStateRejectsTerminalRollback(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	stateID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 3},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		},
		releaseSafetyState: entity.ReleaseSafetyState{
			VersionedBase:            entity.VersionedBase{ID: stateID, Version: 1},
			ReleaseDecisionPackageID: packageID,
			CurrentState:             enum.ReleaseSafetyStateKindStable,
		},
	}
	service := newTestService(repository)

	_, err := service.RecordReleaseSafetyState(context.Background(), RecordReleaseSafetyStateInput{
		ReleaseDecisionPackageID: packageID,
		CurrentState:             enum.ReleaseSafetyStateKindDeploying,
		RuntimeJobRef:            "runtime:job:redeploy",
		LastStateReason:          "redeploy",
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("RecordReleaseSafetyState() error = %v, want ErrPreconditionFailed", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestGetGovernanceSummaryByReleasePackageReturnsSafeOwnerReadModel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	reviewSignalID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	decisionID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	safetyStateID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 5, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Minute)},
			ReleaseCandidateRef: "release:v1.2.3",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha", RepositoryRef: "repo:alpha"},
			RiskAssessmentID:    &assessmentID,
			ProviderRefs:        []byte(`[{"pull_request_ref":"provider:pr:42"}]`),
			RuntimeRefs:         []byte(`[{"job_ref":"runtime:job:deploy","summary_ref":"runtime:summary:deploy"}]`),
			AgentContext:        []byte(`{"run_ref":"agent:run:qa","acceptance_ref":"agent:acceptance:qa"}`),
			ReviewSignalIDs:     []uuid.UUID{reviewSignalID},
			EvidenceRefs: []value.EvidenceRef{{
				Kind:           "agent_acceptance",
				Ref:            "agent:acceptance:qa",
				Summary:        "acceptance failed",
				Digest:         "sha256:acceptance",
				RetentionClass: "safe_ref",
			}},
			IntegrationRefs: []value.ReleaseIntegrationRef{
				{Domain: "provider", Kind: "pull_request", Ref: "provider:pr:42", Status: "opened", Summary: "PR requires owner decision", Digest: "sha256:provider"},
				{Domain: "agent", Kind: "acceptance", Ref: "agent:acceptance:qa", Status: "failed", Summary: "acceptance failed", Digest: "sha256:acceptance", ObservedAt: "2026-05-29T09:55:00Z", Version: "acceptance-version:7"},
				{Domain: "runtime", Kind: "job", Ref: "runtime:job:deploy", Status: "failed", Summary: "deploy job failed with bounded diagnostic", Digest: "sha256:runtime", ErrorCode: "DEPLOY_FAILED"},
			},
			KnownLimitationsSummary: "release needs owner decision",
			Status:                  enum.ReleaseDecisionPackageStatusDecisionRequested,
		},
		releaseDecisions: []entity.ReleaseDecision{{
			VersionedBase:            entity.VersionedBase{ID: decisionID, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
			ReleaseDecisionPackageID: packageID,
			Status:                   enum.ReleaseDecisionStatusRequested,
		}},
		releaseSafetyState: entity.ReleaseSafetyState{
			VersionedBase:            entity.VersionedBase{ID: safetyStateID, Version: 2, CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
			ReleaseDecisionPackageID: packageID,
			CurrentState:             enum.ReleaseSafetyStateKindDeploying,
			RuntimeJobRef:            "runtime:job:deploy",
			LastStateReason:          "deploy observation is active",
		},
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 3, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-90 * time.Minute)},
			Target:             value.ExternalRef{Type: "release_candidate", Ref: "release:v1.2.3"},
			ProjectContext:     value.ProjectContextRef{ProjectRef: "project:alpha", RepositoryRef: "repo:alpha"},
			EffectiveRiskClass: enum.RiskClassR3,
			Status:             enum.RiskAssessmentStatusActive,
			Explanation:        "production release risk",
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "release approval required"}},
		},
		reviewSignal: entity.ReviewSignal{
			ID:               reviewSignalID,
			RiskAssessmentID: &assessmentID,
			Target:           value.ExternalRef{Type: "release_candidate", Ref: "release:v1.2.3"},
			RoleKind:         enum.ReviewRoleKindQA,
			Outcome:          enum.ReviewSignalOutcomePass,
			Severity:         enum.SignalSeverityInfo,
			Summary:          "QA signal passed",
			CreatedAt:        now.Add(-80 * time.Minute),
		},
	}

	summary, err := newTestService(repository).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
		Scope: entity.GovernanceSummaryScope{ReleaseDecisionPackageID: &packageID},
		Meta:  QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}},
	})
	if err != nil {
		t.Fatalf("GetGovernanceSummary(): %v", err)
	}
	if len(summary.PendingDecisions) < 4 {
		t.Fatalf("pending decisions = %+v, want package, release decision, safety state and R3 risk", summary.PendingDecisions)
	}
	if !summaryHasDecision(summary.PendingDecisions, enum.GovernanceDecisionSummaryKindReleaseDecisionPackage, packageID.String(), enum.GovernanceDecisionAttentionPending) {
		t.Fatalf("pending decisions = %+v, want release package summary", summary.PendingDecisions)
	}
	if !summaryHasDecision(summary.PendingDecisions, enum.GovernanceDecisionSummaryKindRiskAssessment, assessmentID.String(), enum.GovernanceDecisionAttentionBlocked) {
		t.Fatalf("pending decisions = %+v, want blocked risk assessment", summary.PendingDecisions)
	}
	if !summaryHasDecision(summary.CompletedDecisions, enum.GovernanceDecisionSummaryKindReviewSignal, reviewSignalID.String(), enum.GovernanceDecisionAttentionCompleted) {
		t.Fatalf("completed decisions = %+v, want review signal", summary.CompletedDecisions)
	}
	if !summaryHasEvidence(summary.EvidenceSummaries, "agent.acceptance", "agent:acceptance:qa") ||
		!summaryHasEvidence(summary.EvidenceSummaries, "runtime.job", "runtime:job:deploy") ||
		!summaryHasEvidence(summary.EvidenceSummaries, "provider.pull_request", "provider:pr:42") {
		t.Fatalf("evidence summaries = %+v, want provider, agent and runtime refs", summary.EvidenceSummaries)
	}
	if summary.Status.Attention != enum.GovernanceDecisionAttentionBlocked ||
		summary.Status.MaxRiskClass != enum.RiskClassR3 ||
		summary.Status.PendingDecisionCount != int32(len(summary.PendingDecisions)) ||
		summary.Status.BlockedDecisionCount != 1 ||
		summary.Status.CompletedDecisionCount != int32(len(summary.CompletedDecisions)) ||
		summary.Status.PendingRequiredGateCount != 1 ||
		summary.Status.EvidenceCount != int32(len(summary.EvidenceSummaries)) ||
		summary.Status.SummaryCode != governanceSummaryCodeBlocked ||
		summary.Status.NextActionCode != governanceSummaryNextActionReviewBlockingDecision {
		t.Fatalf("status = %+v, want blocked live rollup", summary.Status)
	}
	rendered := strings.ToLower(summaryRenderedText(summary))
	for _, unsafe := range []string{"stdout", "stderr", "workspace", "secret", "raw_diff", "provider payload"} {
		if strings.Contains(rendered, unsafe) {
			t.Fatalf("summary leaked unsafe marker %q: %s", unsafe, rendered)
		}
	}
}

func TestGetGovernanceSummaryMissingLinkedRiskIsPartial(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 1},
			ReleaseCandidateRef: "release:v1.2.3",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			RiskAssessmentID:    &assessmentID,
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
		assessmentErr: errs.ErrNotFound,
	}

	summary, err := newTestService(repository).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
		Scope: entity.GovernanceSummaryScope{ReleaseDecisionPackageID: &packageID},
		Meta:  QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}},
	})
	if err != nil {
		t.Fatalf("GetGovernanceSummary(): %v", err)
	}
	if !summaryHasDiagnostic(summary.Diagnostics, "missing_risk_assessment_ref") {
		t.Fatalf("diagnostics = %+v, want missing risk diagnostic", summary.Diagnostics)
	}
	if !summaryHasDecision(summary.PendingDecisions, enum.GovernanceDecisionSummaryKindReleaseDecisionPackage, packageID.String(), enum.GovernanceDecisionAttentionPending) {
		t.Fatalf("pending decisions = %+v, want partial package summary", summary.PendingDecisions)
	}
}

func TestGetGovernanceSummarySelfDeployTargetAuthorizesOwnerActor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)
	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	target := value.ExternalRef{Type: "self_deploy_plan", Ref: "agent:self-deploy-plan:latest"}
	project := value.ProjectContextRef{ProjectRef: "project-self", RepositoryRef: "repo-self", ReleaseLineRef: "self-deploy"}
	repository := &fakeRepository{
		ready: true,
		riskAssessments: []entity.RiskAssessment{{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-30 * time.Minute)},
			Target:             target,
			ProjectContext:     project,
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
			Explanation:        "self-deploy plan requires owner gate",
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy owner approval required"}},
		}},
		gateRequests: []entity.GateRequest{{
			VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 8, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-20 * time.Minute)},
			RiskAssessmentID: &assessmentID,
			Target:           target,
			EvidenceSummary:  "self-deploy owner gate",
			Status:           enum.GateRequestStatusAwaitingDecision,
		}},
	}
	var captured []AuthorizationRequest
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: uuidGenerator{},
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			captured = append(captured, request)
			return nil
		}),
	})

	summary, err := service.GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
		Scope: entity.GovernanceSummaryScope{Target: target, ProjectContext: project},
		Meta:  QueryMeta{Actor: value.Actor{Type: "user", ID: "owner-1"}},
	})
	if err != nil {
		t.Fatalf("GetGovernanceSummary(): %v", err)
	}
	if !summaryHasDecision(summary.PendingDecisions, enum.GovernanceDecisionSummaryKindGateRequest, gateRequestID.String(), enum.GovernanceDecisionAttentionPending) {
		t.Fatalf("pending decisions = %+v, want pending self-deploy gate", summary.PendingDecisions)
	}
	assertCapturedOwnerAccess(t, captured, actionRiskRead, "governance_risk_assessment", target.Ref, "project", project.ProjectRef)
	assertCapturedOwnerAccess(t, captured, actionGateRead, "governance_gate", target.Ref, "project", project.ProjectRef)
}

func TestGetGovernanceSummaryPendingRiskRequiredGateRequestsGate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	repository := &fakeRepository{
		ready: true,
		riskAssessments: []entity.RiskAssessment{{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-20 * time.Minute)},
			Target:             value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"},
			ProjectContext:     value.ProjectContextRef{ProjectRef: "project:kodex", RepositoryRef: "repo:codex-k8s/kodex", ReleaseLineRef: "self-deploy"},
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
			Explanation:        "self-deploy plan requires owner gate",
			RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy owner approval required"}},
		}},
	}

	summary, err := newTestService(repository).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
		Scope: entity.GovernanceSummaryScope{Target: value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"}},
		Meta:  QueryMeta{Actor: value.Actor{Type: "service", ID: "platform-mcp-server"}},
	})
	if err != nil {
		t.Fatalf("GetGovernanceSummary(): %v", err)
	}
	if !summaryHasDecision(summary.PendingDecisions, enum.GovernanceDecisionSummaryKindRiskAssessment, assessmentID.String(), enum.GovernanceDecisionAttentionPending) {
		t.Fatalf("pending decisions = %+v, want self-deploy risk assessment", summary.PendingDecisions)
	}
	if len(summary.PendingDecisions) != 1 || summary.PendingDecisions[0].RequiredGateCount != 1 {
		t.Fatalf("pending decisions = %+v, want one required gate", summary.PendingDecisions)
	}
	if summary.Status.Attention != enum.GovernanceDecisionAttentionPending ||
		summary.Status.PendingRequiredGateCount != 1 ||
		summary.Status.PendingGateCount != 0 ||
		summary.Status.NextActionCode != governanceSummaryNextActionRequestGate {
		t.Fatalf("status = %+v, want request governance gate", summary.Status)
	}
}

func TestGetGovernanceSummaryRequiredGateCoveredByExistingGateRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	assessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	tests := []struct {
		name             string
		gateStatus       enum.GateRequestStatus
		wantPendingGates int32
		wantNextAction   string
	}{
		{
			name:             "open gate waits decision",
			gateStatus:       enum.GateRequestStatusAwaitingDecision,
			wantPendingGates: 1,
			wantNextAction:   governanceSummaryNextActionRecordGateDecision,
		},
		{
			name:             "resolved gate does not request another gate",
			gateStatus:       enum.GateRequestStatusResolved,
			wantPendingGates: 0,
			wantNextAction:   governanceSummaryNextActionReviewPendingDecision,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeRepository{
				ready: true,
				riskAssessments: []entity.RiskAssessment{{
					VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-20 * time.Minute)},
					Target:             value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"},
					ProjectContext:     value.ProjectContextRef{ProjectRef: "project:kodex", RepositoryRef: "repo:codex-k8s/kodex", ReleaseLineRef: "self-deploy"},
					EffectiveRiskClass: enum.RiskClassR2,
					Status:             enum.RiskAssessmentStatusActive,
					Explanation:        "self-deploy plan requires owner gate",
					RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy owner approval required"}},
				}},
				gateRequests: []entity.GateRequest{{
					VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 1, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)},
					RiskAssessmentID: &assessmentID,
					Target:           value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"},
					EvidenceSummary:  "self-deploy owner gate",
					Status:           tt.gateStatus,
				}},
			}

			summary, err := newTestService(repository).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
				Scope: entity.GovernanceSummaryScope{Target: value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"}},
				Meta:  QueryMeta{Actor: value.Actor{Type: "service", ID: "platform-mcp-server"}},
			})
			if err != nil {
				t.Fatalf("GetGovernanceSummary(): %v", err)
			}
			if summary.Status.PendingRequiredGateCount != 0 ||
				summary.Status.PendingGateCount != tt.wantPendingGates ||
				summary.Status.NextActionCode != tt.wantNextAction {
				t.Fatalf("status = %+v, want covered required gate with next action %s", summary.Status, tt.wantNextAction)
			}
		})
	}
}

func TestGetGovernanceSummaryPackageRiskRequiredGateCoveredByLinkedGateRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	integrationRef := value.ReleaseIntegrationRef{Domain: "agent", Kind: "run", Ref: "agent:run:self-deploy"}
	selectors := []struct {
		name  string
		scope entity.GovernanceSummaryScope
	}{
		{
			name:  "release package id",
			scope: entity.GovernanceSummaryScope{ReleaseDecisionPackageID: &packageID},
		},
		{
			name:  "integration ref",
			scope: entity.GovernanceSummaryScope{IntegrationRef: integrationRef},
		},
	}
	gateStatuses := []struct {
		name             string
		status           enum.GateRequestStatus
		wantPendingGates int32
		wantNextAction   string
	}{
		{
			name:             "open gate waits decision",
			status:           enum.GateRequestStatusAwaitingDecision,
			wantPendingGates: 1,
			wantNextAction:   governanceSummaryNextActionRecordGateDecision,
		},
		{
			name:             "resolved gate covers required gate",
			status:           enum.GateRequestStatusResolved,
			wantPendingGates: 0,
			wantNextAction:   governanceSummaryNextActionReviewPendingDecision,
		},
	}
	for _, selector := range selectors {
		selector := selector
		t.Run(selector.name, func(t *testing.T) {
			for _, gateStatus := range gateStatuses {
				gateStatus := gateStatus
				t.Run(gateStatus.name, func(t *testing.T) {
					releasePackage := entity.ReleaseDecisionPackage{
						VersionedBase:       entity.VersionedBase{ID: packageID, Version: 1, CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now.Add(-30 * time.Minute)},
						ReleaseCandidateRef: "release:self-deploy",
						ProjectContext:      value.ProjectContextRef{ProjectRef: "project:kodex", RepositoryRef: "repo:codex-k8s/kodex", ReleaseLineRef: "self-deploy"},
						RiskAssessmentID:    &assessmentID,
						IntegrationRefs:     []value.ReleaseIntegrationRef{integrationRef},
						Status:              enum.ReleaseDecisionPackageStatusReady,
					}
					repository := &fakeRepository{
						ready:          true,
						releasePackage: releasePackage,
						releasePackages: []entity.ReleaseDecisionPackage{
							releasePackage,
						},
						assessment: entity.RiskAssessment{
							VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now.Add(-20 * time.Minute)},
							Target:             value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"},
							ProjectContext:     value.ProjectContextRef{ProjectRef: "project:kodex", RepositoryRef: "repo:codex-k8s/kodex", ReleaseLineRef: "self-deploy"},
							EffectiveRiskClass: enum.RiskClassR2,
							Status:             enum.RiskAssessmentStatusActive,
							Explanation:        "self-deploy plan requires owner gate",
							RequiredGates:      []entity.RequiredGate{{GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "self-deploy owner approval required"}},
						},
						gateRequests: []entity.GateRequest{{
							VersionedBase:    entity.VersionedBase{ID: gateRequestID, Version: 1, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute)},
							RiskAssessmentID: &assessmentID,
							Target:           value.ExternalRef{Type: "merge", Ref: "provider:merge:codex-main"},
							EvidenceSummary:  "self-deploy owner gate",
							Status:           gateStatus.status,
						}},
					}

					summary, err := newTestService(repository).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
						Scope: selector.scope,
						Meta:  QueryMeta{Actor: value.Actor{Type: "service", ID: "platform-mcp-server"}},
					})
					if err != nil {
						t.Fatalf("GetGovernanceSummary(): %v", err)
					}
					if repository.gateRequestListFilter.RiskAssessmentID == nil || *repository.gateRequestListFilter.RiskAssessmentID != assessmentID {
						t.Fatalf("gate request filter = %+v, want linked risk assessment", repository.gateRequestListFilter)
					}
					if summary.Status.PendingRequiredGateCount != 0 ||
						summary.Status.PendingGateCount != gateStatus.wantPendingGates ||
						summary.Status.NextActionCode != gateStatus.wantNextAction {
						t.Fatalf("status = %+v, want linked gate coverage with next action %s", summary.Status, gateStatus.wantNextAction)
					}
				})
			}
		})
	}
}

func TestGetGovernanceSummaryByIntegrationRefUsesReleasePackageFilter(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	repository := &fakeRepository{
		ready: true,
		releasePackages: []entity.ReleaseDecisionPackage{{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.2.3",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			IntegrationRefs: []value.ReleaseIntegrationRef{{
				Domain: "agent",
				Kind:   "run",
				Ref:    "agent:run:42",
				Status: "completed",
				Digest: "sha256:run",
			}},
			Status: enum.ReleaseDecisionPackageStatusClosed,
		}},
	}

	summary, err := newTestService(repository).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
		Scope: entity.GovernanceSummaryScope{IntegrationRef: value.ReleaseIntegrationRef{Domain: "AGENT", Kind: "RUN", Ref: "agent:run:42"}},
		Meta:  QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}},
	})
	if err != nil {
		t.Fatalf("GetGovernanceSummary(): %v", err)
	}
	if repository.releasePackageListFilter.IntegrationRef.Domain != "agent" ||
		repository.releasePackageListFilter.IntegrationRef.Kind != "run" ||
		repository.releasePackageListFilter.IntegrationRef.Ref != "agent:run:42" {
		t.Fatalf("release package filter = %+v, want normalized agent run ref", repository.releasePackageListFilter)
	}
	if !summaryHasDecision(summary.CompletedDecisions, enum.GovernanceDecisionSummaryKindReleaseDecisionPackage, packageID.String(), enum.GovernanceDecisionAttentionCompleted) {
		t.Fatalf("completed decisions = %+v, want closed package summary", summary.CompletedDecisions)
	}
}

func TestGetGovernanceSummaryRejectsEmptyScope(t *testing.T) {
	t.Parallel()

	_, err := newTestService(&fakeRepository{ready: true}).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
		Meta: QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("GetGovernanceSummary() error = %v, want ErrInvalidArgument", err)
	}
}

func TestGetGovernanceSummaryRejectsMixedScopeSelectors(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	cases := map[string]entity.GovernanceSummaryScope{
		"package and integration ref": {
			ReleaseDecisionPackageID: &packageID,
			IntegrationRef:           value.ReleaseIntegrationRef{Domain: "agent", Kind: "run", Ref: "agent:run:42"},
		},
		"project and release candidate": {
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			ReleaseCandidateRef: "release:v1.2.3",
		},
	}
	for name, scope := range cases {
		name := name
		scope := scope
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestService(&fakeRepository{ready: true}).GetGovernanceSummary(context.Background(), GetGovernanceSummaryInput{
				Scope: scope,
				Meta:  QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}},
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("GetGovernanceSummary() error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func summaryHasDecision(items []entity.GovernanceDecisionSummary, kind enum.GovernanceDecisionSummaryKind, id string, attention enum.GovernanceDecisionAttention) bool {
	for _, item := range items {
		if item.Kind == kind && item.ID == id && item.Attention == attention {
			return true
		}
	}
	return false
}

func summaryHasEvidence(items []entity.GovernanceEvidenceSummary, kind string, ref string) bool {
	for _, item := range items {
		if item.SourceKind == kind && item.SourceRef == ref {
			return true
		}
	}
	return false
}

func summaryHasDiagnostic(items []string, diagnostic string) bool {
	for _, item := range items {
		if item == diagnostic {
			return true
		}
	}
	return false
}

func assertCapturedGovernanceAccess(t *testing.T, requests []AuthorizationRequest, actionKey string, resourceType string) {
	t.Helper()
	for _, request := range requests {
		if request.ActionKey == actionKey && request.ResourceType == resourceType {
			return
		}
	}
	t.Fatalf("access requests = %+v, want action=%s resource=%s", requests, actionKey, resourceType)
}

func assertCapturedOwnerAccess(t *testing.T, requests []AuthorizationRequest, actionKey string, resourceType string, resourceID string, scopeType string, scopeID string) {
	t.Helper()
	for _, request := range requests {
		if request.Subject.Type == "user" &&
			request.Subject.ID == "owner-1" &&
			request.ActionKey == actionKey &&
			request.ResourceType == resourceType &&
			request.ResourceID == resourceID &&
			request.ScopeType == scopeType &&
			request.ScopeID == scopeID {
			return
		}
	}
	t.Fatalf("access requests = %+v, want owner action=%s resource=%s/%s scope=%s/%s", requests, actionKey, resourceType, resourceID, scopeType, scopeID)
}

func summaryRenderedText(summary entity.GovernanceSummary) string {
	var builder strings.Builder
	for _, item := range summary.PendingDecisions {
		builder.WriteString(item.SafeSummary)
		builder.WriteString("\n")
	}
	for _, item := range summary.CompletedDecisions {
		builder.WriteString(item.SafeSummary)
		builder.WriteString("\n")
	}
	for _, item := range summary.EvidenceSummaries {
		builder.WriteString(item.SafeSummary)
		builder.WriteString("\n")
		builder.WriteString(item.ErrorCode)
		builder.WriteString("\n")
	}
	return builder.String()
}

type fakeRepository struct {
	governancerepo.Repository
	ready                     bool
	hasCommandResult          bool
	commandResult             entity.CommandResult
	profile                   entity.RiskProfile
	profileVersion            entity.RiskProfileVersion
	assessment                entity.RiskAssessment
	assessmentErr             error
	riskAssessments           []entity.RiskAssessment
	assessmentReads           int
	riskFactors               []entity.RiskFactor
	riskAssessmentListCalls   int
	riskFactorListCalls       int
	assessmentUpdateCalls     int
	reviewSignal              entity.ReviewSignal
	reviewSignalErr           error
	reviewSignals             []entity.ReviewSignal
	reviewSignalByFingerprint entity.ReviewSignal
	reviewSignalListCalls     int
	gateRequest               entity.GateRequest
	gateRequestErr            error
	gateRequests              []entity.GateRequest
	gateRequestReads          int
	gateRequestListFilter     query.GateRequestFilter
	gateRequestListCalls      int
	gateDecision              entity.GateDecision
	gateDecisionErr           error
	gateDecisions             []entity.GateDecision
	gateDecisionReads         int
	gateDecisionListCalls     int
	releasePackage            entity.ReleaseDecisionPackage
	releasePackages           []entity.ReleaseDecisionPackage
	releasePackageReads       int
	releasePackageListFilter  query.ReleaseDecisionPackageFilter
	releasePackageListCalls   int
	releaseDecision           entity.ReleaseDecision
	releaseDecisions          []entity.ReleaseDecision
	releaseDecisionReads      int
	releaseDecisionListCalls  int
	releaseSafetyState        entity.ReleaseSafetyState
	releaseSafetyStateErr     error
	blockingSignal            entity.BlockingSignal
	blockingSignals           []entity.BlockingSignal
	blockingSignalReads       int
	blockingSignalListCalls   int
	result                    entity.CommandResult
	commandResultReads        int
	events                    []entity.OutboxEvent
	mutationCalls             int
}

func newTestService(repository *fakeRepository) *Service {
	return NewWithConfig(Config{
		Repository:  repository,
		Clock:       systemClock{},
		IDGenerator: uuidGenerator{},
		Authorizer:  AllowAllAuthorizer{},
	})
}

func (repository *fakeRepository) Ready() bool {
	return repository.ready
}

func (repository *fakeRepository) GetCommandResult(_ context.Context, _ query.CommandIdentity) (entity.CommandResult, error) {
	repository.commandResultReads++
	if !repository.hasCommandResult {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	return repository.commandResult, nil
}

func (repository *fakeRepository) RecordCommandResult(_ context.Context, result entity.CommandResult) error {
	repository.result = result
	return nil
}

func (repository *fakeRepository) CreateRiskProfile(_ context.Context, profile entity.RiskProfile, result entity.CommandResult) error {
	repository.mutationCalls++
	repository.profile = profile
	repository.result = result
	return nil
}

func (repository *fakeRepository) CreateRiskProfileVersion(_ context.Context, version entity.RiskProfileVersion, result entity.CommandResult) error {
	repository.mutationCalls++
	repository.profileVersion = version
	repository.result = result
	return nil
}

func (repository *fakeRepository) ActivateRiskProfileVersion(_ context.Context, profile entity.RiskProfile, _ int64, version entity.RiskProfileVersion, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.profile = profile
	repository.profileVersion = version
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) ArchiveRiskProfile(_ context.Context, profile entity.RiskProfile, _ int64, result entity.CommandResult) error {
	repository.mutationCalls++
	repository.profile = profile
	repository.result = result
	return nil
}

func (repository *fakeRepository) GetRiskProfile(_ context.Context, _ uuid.UUID) (entity.RiskProfile, error) {
	return repository.profile, nil
}

func (repository *fakeRepository) GetRiskProfileVersion(_ context.Context, _ uuid.UUID, _ int64) (entity.RiskProfileVersion, error) {
	return repository.profileVersion, nil
}

func (repository *fakeRepository) CreateRiskAssessment(_ context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, result entity.CommandResult, events []entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.assessment = assessment
	repository.riskFactors = factors
	repository.result = result
	repository.events = events
	return nil
}

func (repository *fakeRepository) UpdateRiskAssessment(_ context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, _ int64, result entity.CommandResult, events []entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.assessmentUpdateCalls++
	repository.assessment = assessment
	repository.riskFactors = factors
	repository.result = result
	repository.events = events
	return nil
}

func (repository *fakeRepository) GetRiskAssessment(_ context.Context, _ uuid.UUID) (entity.RiskAssessment, error) {
	repository.assessmentReads++
	if repository.assessmentErr != nil {
		return entity.RiskAssessment{}, repository.assessmentErr
	}
	return repository.assessment, nil
}

func (repository *fakeRepository) ListRiskAssessments(_ context.Context, _ query.RiskAssessmentFilter) ([]entity.RiskAssessment, query.PageResult, error) {
	repository.riskAssessmentListCalls++
	if len(repository.riskAssessments) > 0 {
		return repository.riskAssessments, query.PageResult{}, nil
	}
	return nil, query.PageResult{}, nil
}

func (repository *fakeRepository) ListRiskFactors(_ context.Context, _ query.RiskFactorFilter) ([]entity.RiskFactor, query.PageResult, error) {
	repository.riskFactorListCalls++
	return repository.riskFactors, query.PageResult{}, nil
}

func (repository *fakeRepository) RecordReviewSignal(_ context.Context, signal entity.ReviewSignal, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.reviewSignal = signal
	repository.reviewSignalByFingerprint = signal
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReviewSignal(_ context.Context, _ uuid.UUID) (entity.ReviewSignal, error) {
	if repository.reviewSignalErr != nil {
		return entity.ReviewSignal{}, repository.reviewSignalErr
	}
	return repository.reviewSignal, nil
}

func (repository *fakeRepository) GetReviewSignalByFingerprint(_ context.Context, fingerprint string) (entity.ReviewSignal, error) {
	if repository.reviewSignalByFingerprint.SourceFingerprint == fingerprint && fingerprint != "" {
		return repository.reviewSignalByFingerprint, nil
	}
	return entity.ReviewSignal{}, errs.ErrNotFound
}

func (repository *fakeRepository) ListReviewSignals(_ context.Context, _ query.ReviewSignalFilter) ([]entity.ReviewSignal, query.PageResult, error) {
	repository.reviewSignalListCalls++
	return repository.reviewSignals, query.PageResult{}, nil
}

func (repository *fakeRepository) CreateGateRequest(_ context.Context, request entity.GateRequest, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.gateRequest = request
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateGateRequestStatus(_ context.Context, request entity.GateRequest, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.gateRequest = request
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateGateRequestWithDecision(_ context.Context, request entity.GateRequest, _ int64, decision entity.GateDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.gateRequest = request
	repository.gateDecision = decision
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetGateRequest(_ context.Context, _ uuid.UUID) (entity.GateRequest, error) {
	repository.gateRequestReads++
	if repository.gateRequestErr != nil {
		return entity.GateRequest{}, repository.gateRequestErr
	}
	return repository.gateRequest, nil
}

func (repository *fakeRepository) GetGateDecision(_ context.Context, _ uuid.UUID) (entity.GateDecision, error) {
	repository.gateDecisionReads++
	if repository.gateDecisionErr != nil {
		return entity.GateDecision{}, repository.gateDecisionErr
	}
	return repository.gateDecision, nil
}

func (repository *fakeRepository) ListGateRequests(_ context.Context, filter query.GateRequestFilter) ([]entity.GateRequest, query.PageResult, error) {
	repository.gateRequestListFilter = filter
	repository.gateRequestListCalls++
	if len(repository.gateRequests) > 0 {
		return repository.gateRequests, query.PageResult{}, nil
	}
	return nil, query.PageResult{}, nil
}

func (repository *fakeRepository) ListGateDecisions(_ context.Context, _ query.GateDecisionFilter) ([]entity.GateDecision, query.PageResult, error) {
	repository.gateDecisionListCalls++
	if len(repository.gateDecisions) > 0 {
		return repository.gateDecisions, query.PageResult{}, nil
	}
	return nil, query.PageResult{}, nil
}

func (repository *fakeRepository) CreateReleaseDecisionPackage(_ context.Context, item entity.ReleaseDecisionPackage, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = item
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseDecisionPackageEvidence(_ context.Context, item entity.ReleaseDecisionPackage, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = item
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseDecisionPackageStatus(_ context.Context, item entity.ReleaseDecisionPackage, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = item
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReleaseDecisionPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseDecisionPackage, error) {
	repository.releasePackageReads++
	return repository.releasePackage, nil
}

func (repository *fakeRepository) ListReleaseDecisionPackages(_ context.Context, filter query.ReleaseDecisionPackageFilter) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	repository.releasePackageListFilter = filter
	repository.releasePackageListCalls++
	if len(repository.releasePackages) > 0 {
		return repository.releasePackages, query.PageResult{}, nil
	}
	if repository.releasePackage.ID == uuid.Nil {
		return nil, query.PageResult{}, nil
	}
	return []entity.ReleaseDecisionPackage{repository.releasePackage}, query.PageResult{}, nil
}

func (repository *fakeRepository) CreateReleaseDecision(_ context.Context, pkg entity.ReleaseDecisionPackage, _ int64, decision entity.ReleaseDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = pkg
	repository.releaseDecision = decision
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseDecision(_ context.Context, pkg entity.ReleaseDecisionPackage, _ int64, decision entity.ReleaseDecision, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = pkg
	repository.releaseDecision = decision
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReleaseDecision(_ context.Context, _ uuid.UUID) (entity.ReleaseDecision, error) {
	repository.releaseDecisionReads++
	return repository.releaseDecision, nil
}

func (repository *fakeRepository) GetReleaseDecisionByPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseDecision, error) {
	repository.releaseDecisionReads++
	if repository.releaseDecision.ID == uuid.Nil {
		return entity.ReleaseDecision{}, errs.ErrNotFound
	}
	return repository.releaseDecision, nil
}

func (repository *fakeRepository) ListReleaseDecisions(_ context.Context, _ query.ReleaseDecisionFilter) ([]entity.ReleaseDecision, query.PageResult, error) {
	repository.releaseDecisionListCalls++
	if len(repository.releaseDecisions) > 0 {
		return repository.releaseDecisions, query.PageResult{}, nil
	}
	if repository.releaseDecision.ID == uuid.Nil {
		return nil, query.PageResult{}, nil
	}
	return []entity.ReleaseDecision{repository.releaseDecision}, query.PageResult{}, nil
}

func (repository *fakeRepository) RecordReleaseSafetyState(_ context.Context, state entity.ReleaseSafetyState, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releaseSafetyState = state
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseSafetyState(_ context.Context, state entity.ReleaseSafetyState, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releaseSafetyState = state
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReleaseSafetyStateByPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseSafetyState, error) {
	if repository.releaseSafetyStateErr != nil {
		return entity.ReleaseSafetyState{}, repository.releaseSafetyStateErr
	}
	if repository.releaseSafetyState.ID == uuid.Nil {
		return entity.ReleaseSafetyState{}, errs.ErrNotFound
	}
	return repository.releaseSafetyState, nil
}

func (repository *fakeRepository) RecordBlockingSignal(_ context.Context, signal entity.BlockingSignal, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.blockingSignal = signal
	repository.blockingSignals = append(repository.blockingSignals, signal)
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateBlockingSignal(_ context.Context, signal entity.BlockingSignal, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.blockingSignal = signal
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetBlockingSignal(_ context.Context, _ uuid.UUID) (entity.BlockingSignal, error) {
	repository.blockingSignalReads++
	return repository.blockingSignal, nil
}

func (repository *fakeRepository) ListBlockingSignals(_ context.Context, filter query.BlockingSignalFilter) ([]entity.BlockingSignal, query.PageResult, error) {
	repository.blockingSignalListCalls++
	if len(repository.blockingSignals) == 0 {
		return nil, query.PageResult{}, nil
	}
	items := make([]entity.BlockingSignal, 0, len(repository.blockingSignals))
	for _, signal := range repository.blockingSignals {
		if externalRefProvided(filter.Target) && !sameExternalRef(signal.Target, filter.Target) {
			continue
		}
		if filter.Status != "" && signal.Status != filter.Status {
			continue
		}
		if filter.Severity != "" && signal.Severity != filter.Severity {
			continue
		}
		items = append(items, signal)
	}
	return items, query.PageResult{}, nil
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type fixedIDs struct {
	ids []uuid.UUID
}

func (generator *fixedIDs) New() uuid.UUID {
	if len(generator.ids) == 0 {
		return uuid.Nil
	}
	id := generator.ids[0]
	generator.ids = generator.ids[1:]
	return id
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}

type authorizerFunc func(context.Context, AuthorizationRequest) error

func (fn authorizerFunc) Authorize(ctx context.Context, request AuthorizationRequest) error {
	return fn(ctx, request)
}
