// Package governance адаптирует операции governance-manager для agent-manager.
package governance

import (
	"context"
	"strings"
	"time"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const (
	callerID              = "agent-manager"
	defaultRequestTimeout = 10 * time.Second
)

// Config содержит настройки клиента governance-manager.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type selfDeployGateClient interface {
	selfDeployGateCommandClient
	selfDeployGateReadClient
}

type selfDeployGateCommandClient interface {
	PrepareSelfDeployPlanGate(context.Context, *governancev1.PrepareSelfDeployPlanGateRequest, ...grpc.CallOption) (*governancev1.SelfDeployPlanGateResponse, error)
}

type selfDeployGateReadClient interface {
	ListRiskAssessments(context.Context, *governancev1.ListRiskAssessmentsRequest, ...grpc.CallOption) (*governancev1.ListRiskAssessmentsResponse, error)
	ListGateRequests(context.Context, *governancev1.ListGateRequestsRequest, ...grpc.CallOption) (*governancev1.ListGateRequestsResponse, error)
}

// SelfDeployGatePreparer вызывает governance-manager PrepareSelfDeployPlanGate.
type SelfDeployGatePreparer struct {
	client    selfDeployGateClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.SelfDeployGatePreparer = (*SelfDeployGatePreparer)(nil)

// NewConnection создаёт gRPC-подключение к governance-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "governance-manager")
}

// NewSelfDeployGatePreparer создаёт клиент подготовки self-deploy gate.
func NewSelfDeployGatePreparer(client governancev1.GovernanceManagerServiceClient, cfg Config) (*SelfDeployGatePreparer, error) {
	return newSelfDeployGatePreparer(client, cfg)
}

func newSelfDeployGatePreparer(client selfDeployGateClient, cfg Config) (*SelfDeployGatePreparer, error) {
	settings, err := selfDeployGateSettings(client, cfg)
	if err != nil {
		return nil, err
	}
	preparer := &SelfDeployGatePreparer{client: client}
	preparer.authToken = settings.AuthToken
	preparer.timeout = settings.Timeout
	return preparer, nil
}

func selfDeployGateSettings(client selfDeployGateClient, cfg Config) (grpcclient.ClientSettings, error) {
	return grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultRequestTimeout, "governance-manager")
}

// PrepareSelfDeployPlanGate готовит или переиспользует owner/governance gate для self-deploy plan.
func (preparer *SelfDeployGatePreparer) PrepareSelfDeployPlanGate(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput) (agentservice.SelfDeployPlanGatePreparationResult, error) {
	if preparer == nil || preparer.client == nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(grpcclient.OutgoingContext(ctx, preparer.authToken, callerID), preparer.timeout)
	defer cancel()
	response, err := preparer.client.PrepareSelfDeployPlanGate(callCtx, prepareSelfDeployPlanGateRequest(input))
	if err != nil {
		result, ok, lookupErr := preparer.lookupExistingSelfDeployPlanGate(callCtx, input)
		if lookupErr != nil {
			return agentservice.SelfDeployPlanGatePreparationResult{}, agentservice.NewSelfDeployGateRecoveryError(agentservice.SelfDeployGateRecoveryCodeExistingGateLookupFailed, lookupErr)
		}
		if ok {
			return result, nil
		}
		return agentservice.SelfDeployPlanGatePreparationResult{}, grpcclient.MapReadError(err, "governance-manager self-deploy gate command failed")
	}
	result, err := selfDeployPlanGateResult(response)
	if err == nil {
		return result, nil
	}
	result, ok, lookupErr := preparer.lookupExistingSelfDeployPlanGate(callCtx, input)
	if lookupErr != nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, agentservice.NewSelfDeployGateRecoveryError(agentservice.SelfDeployGateRecoveryCodeExistingGateLookupFailed, lookupErr)
	}
	if ok {
		return result, nil
	}
	return agentservice.SelfDeployPlanGatePreparationResult{}, agentservice.NewSelfDeployGateRecoveryError(agentservice.SelfDeployGateRecoveryCodeGateResponseInvalid, err)
}

func prepareSelfDeployPlanGateRequest(input agentservice.SelfDeployPlanGatePreparationInput) *governancev1.PrepareSelfDeployPlanGateRequest {
	plan := input.Plan
	return &governancev1.PrepareSelfDeployPlanGateRequest{
		Meta: commandMeta(input.Meta),
		Plan: &governancev1.SelfDeployPlanGateInput{
			SelfDeployPlanRef:       selfDeployPlanRef(plan.ID),
			ProjectContext:          projectContext(plan),
			ProviderSignalRef:       optionalString(plan.ProviderSignalRef),
			SourceRef:               optionalString(plan.SourceRef),
			MergeCommitSha:          optionalString(plan.MergeCommitSHA),
			ServicesYamlRef:         optionalString(plan.ServicesYAMLRef),
			ServicesYamlDigest:      optionalString(plan.ServicesYAMLDigest),
			AffectedServiceKeys:     append([]string(nil), plan.AffectedServiceKeys...),
			PathCategories:          selfDeployPathCategories(plan.PathCategories),
			ExpectedRuntimeJobTypes: selfDeployRuntimeJobTypes(plan.ExpectedRuntimeJobTypes),
			SafeSummary:             optionalString(plan.SafeSummary),
			PlanFingerprint:         strings.TrimSpace(plan.PlanFingerprint),
			RiskProfileRef:          optionalString(governanceRiskProfileRef(plan.GovernanceContext.RiskProfileRef)),
		},
	}
}

func commandMeta(meta value.CommandMeta) *governancev1.CommandMeta {
	commandID := optionalUUIDString(meta.CommandID)
	idempotencyKey := optionalString(meta.IdempotencyKey)
	requestID := firstNonEmpty(optionalStringValue(commandID), strings.TrimSpace(meta.IdempotencyKey), "self-deploy-plan-gate")
	return &governancev1.CommandMeta{
		CommandId:      commandID,
		IdempotencyKey: idempotencyKey,
		Actor:          actor(meta.Actor),
		Reason:         "agent-self-deploy-plan-gate",
		RequestId:      requestID,
		RequestContext: &governancev1.RequestContext{Source: callerID},
	}
}

func actor(item value.Actor) *governancev1.Actor {
	if strings.TrimSpace(item.Type) == "" && strings.TrimSpace(item.ID) == "" {
		return nil
	}
	return &governancev1.Actor{Type: strings.TrimSpace(item.Type), Id: strings.TrimSpace(item.ID)}
}

func projectContext(plan entity.SelfDeployPlan) *governancev1.ProjectContextRef {
	return &governancev1.ProjectContextRef{
		ProjectRef:       optionalString(plan.ProjectRef),
		RepositoryRef:    optionalString(plan.RepositoryRef),
		ReleasePolicyRef: optionalString(plan.GovernanceContext.ReleasePolicyRef),
	}
}

func selfDeployPlanGateResult(response *governancev1.SelfDeployPlanGateResponse) (agentservice.SelfDeployPlanGatePreparationResult, error) {
	if response == nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	context := value.GovernanceContextRef{
		RiskAssessmentRef: governanceRef("risk_assessment", response.GetRiskAssessment().GetId()),
		GateRequestRef:    governanceRef("gate_request", response.GetGateRequest().GetId()),
	}
	if decision := response.GetGateDecision(); decision != nil {
		context.GateDecisionRef = governanceRef("gate_decision", decision.GetId())
	}
	status, ok := selfDeployPlanGateStatus(response.GetStatus())
	if !ok {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	if context.RiskAssessmentRef == "" {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	if status == agentservice.SelfDeployPlanGateStatusPending && context.GateRequestRef == "" {
		return agentservice.SelfDeployPlanGatePreparationResult{}, errs.ErrDependencyUnavailable
	}
	return agentservice.SelfDeployPlanGatePreparationResult{
		Status:            status,
		GovernanceContext: context,
		SafeSummary:       strings.TrimSpace(response.GetRiskAssessment().GetExplanation()),
	}, nil
}

func (preparer *SelfDeployGatePreparer) lookupExistingSelfDeployPlanGate(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput) (agentservice.SelfDeployPlanGatePreparationResult, bool, error) {
	target := selfDeployPlanTarget(input.Plan)
	if target.GetRef() == "" || strings.TrimSpace(input.Plan.PlanFingerprint) == "" {
		return agentservice.SelfDeployPlanGatePreparationResult{}, false, nil
	}
	assessment, ok, err := preparer.lookupSelfDeployRiskAssessment(ctx, input, target)
	if err != nil || !ok {
		return agentservice.SelfDeployPlanGatePreparationResult{}, ok, err
	}
	gate, ok, err := preparer.lookupSelfDeployGateRequest(ctx, input, target, assessment.GetId())
	if err != nil || !ok {
		return agentservice.SelfDeployPlanGatePreparationResult{}, ok, err
	}
	return agentservice.SelfDeployPlanGatePreparationResult{
		Status: agentservice.SelfDeployPlanGateStatusPending,
		GovernanceContext: value.GovernanceContextRef{
			RiskAssessmentRef: governanceRef("risk_assessment", assessment.GetId()),
			GateRequestRef:    governanceRef("gate_request", gate.GetId()),
		},
		SafeSummary: strings.TrimSpace(assessment.GetExplanation()),
	}, true, nil
}

func (preparer *SelfDeployGatePreparer) lookupSelfDeployRiskAssessment(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput, target *governancev1.TargetRef) (*governancev1.RiskAssessment, bool, error) {
	status := governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE
	response, err := preparer.client.ListRiskAssessments(ctx, &governancev1.ListRiskAssessmentsRequest{
		Target:         target,
		ProjectContext: projectContext(input.Plan),
		Status:         &status,
		Page:           &governancev1.PageRequest{PageSize: 10},
		Meta:           queryMeta(input.Meta),
	})
	if err != nil {
		return nil, false, grpcclient.MapReadError(err, "governance-manager self-deploy risk assessment lookup failed")
	}
	var selected *governancev1.RiskAssessment
	for _, item := range response.GetRiskAssessments() {
		if !selfDeployRiskAssessmentMatchesPlan(item, target.GetRef(), input.Plan.PlanFingerprint) {
			continue
		}
		if selected != nil {
			return nil, false, errs.ErrConflict
		}
		selected = item
	}
	if selected == nil {
		return nil, false, nil
	}
	return selected, true, nil
}

func (preparer *SelfDeployGatePreparer) lookupSelfDeployGateRequest(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput, target *governancev1.TargetRef, assessmentID string) (*governancev1.GateRequest, bool, error) {
	assessmentID = strings.TrimSpace(assessmentID)
	if assessmentID == "" {
		return nil, false, nil
	}
	response, err := preparer.client.ListGateRequests(ctx, &governancev1.ListGateRequestsRequest{
		RiskAssessmentId: &assessmentID,
		Page:             &governancev1.PageRequest{PageSize: 10},
		Meta:             queryMeta(input.Meta),
	})
	if err != nil {
		return nil, false, grpcclient.MapReadError(err, "governance-manager self-deploy gate request lookup failed")
	}
	var selected *governancev1.GateRequest
	for _, item := range response.GetGateRequests() {
		if !selfDeployGateRequestMatchesAssessment(item, target.GetRef(), assessmentID) {
			continue
		}
		if selected != nil {
			return nil, false, errs.ErrConflict
		}
		selected = item
	}
	if selected == nil {
		return nil, false, nil
	}
	return selected, true, nil
}

func queryMeta(meta value.CommandMeta) *governancev1.QueryMeta {
	return &governancev1.QueryMeta{
		Actor:          actor(meta.Actor),
		RequestId:      firstNonEmpty(strings.TrimSpace(meta.IdempotencyKey), "self-deploy-plan-gate-read"),
		RequestContext: &governancev1.RequestContext{Source: callerID},
	}
}

func selfDeployPlanTarget(plan entity.SelfDeployPlan) *governancev1.TargetRef {
	return &governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN,
		Ref:  selfDeployPlanRef(plan.ID),
	}
}

func selfDeployRiskAssessmentMatchesPlan(item *governancev1.RiskAssessment, planRef string, fingerprint string) bool {
	if item == nil || strings.TrimSpace(item.GetId()) == "" {
		return false
	}
	if item.GetTarget().GetType() != governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN ||
		strings.TrimSpace(item.GetTarget().GetRef()) != strings.TrimSpace(planRef) {
		return false
	}
	for _, evidence := range item.GetEvidenceRefs() {
		if evidence.GetKind() == governancev1.EvidenceKind_EVIDENCE_KIND_SELF_DEPLOY_PLAN &&
			strings.TrimSpace(evidence.GetRef()) == strings.TrimSpace(planRef) &&
			strings.TrimSpace(evidence.GetDigest()) == strings.TrimSpace(fingerprint) {
			return true
		}
	}
	return false
}

func selfDeployGateRequestMatchesAssessment(item *governancev1.GateRequest, planRef string, assessmentID string) bool {
	if item == nil || strings.TrimSpace(item.GetId()) == "" {
		return false
	}
	if item.GetTarget().GetType() != governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN ||
		strings.TrimSpace(item.GetTarget().GetRef()) != strings.TrimSpace(planRef) ||
		strings.TrimSpace(item.GetRiskAssessmentId()) != strings.TrimSpace(assessmentID) {
		return false
	}
	switch item.GetStatus() {
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION:
		return true
	default:
		return false
	}
}

func selfDeployPlanGateStatus(status governancev1.SelfDeployPlanGateStatus) (agentservice.SelfDeployPlanGateStatus, bool) {
	switch status {
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_PENDING:
		return agentservice.SelfDeployPlanGateStatusPending, true
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_APPROVED:
		return agentservice.SelfDeployPlanGateStatusApproved, true
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_REJECTED:
		return agentservice.SelfDeployPlanGateStatusRejected, true
	case governancev1.SelfDeployPlanGateStatus_SELF_DEPLOY_PLAN_GATE_STATUS_BLOCKED:
		return agentservice.SelfDeployPlanGateStatusBlocked, true
	default:
		return "", false
	}
}

func selfDeployPlanRef(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return "agent:self-deploy-plan:" + id.String()
}

func governanceRef(kind string, id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	return "governance:" + kind + "/" + trimmed
}

func governanceRiskProfileRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	for _, prefix := range []string{"governance:risk_profile/", "risk_profile/", "governance:risk_profile:", "risk_profile:"} {
		if suffix, ok := strings.CutPrefix(trimmed, prefix); ok {
			return governanceRiskProfileUUIDRef(suffix, "governance:risk_profile:")
		}
	}
	return governanceRiskProfileUUIDRef(trimmed, "")
}

func governanceRiskProfileUUIDRef(value string, prefix string) string {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || id == uuid.Nil {
		return ""
	}
	return prefix + id.String()
}

func selfDeployPathCategories(values []enum.SelfDeployPathCategory) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func selfDeployRuntimeJobTypes(values []enum.SelfDeployRuntimeJobType) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func optionalUUIDString(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
