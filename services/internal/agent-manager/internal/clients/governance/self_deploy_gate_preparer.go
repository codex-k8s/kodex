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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	ListGateDecisions(context.Context, *governancev1.ListGateDecisionsRequest, ...grpc.CallOption) (*governancev1.ListGateDecisionsResponse, error)
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
			return agentservice.SelfDeployPlanGatePreparationResult{}, selfDeployGateLookupError(agentservice.SelfDeployGateRecoveryCodeExistingGateLookupFailed, lookupErr)
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
		return agentservice.SelfDeployPlanGatePreparationResult{}, selfDeployGateLookupError(agentservice.SelfDeployGateRecoveryCodeExistingGateLookupFailed, lookupErr)
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
		if err != nil {
			return agentservice.SelfDeployPlanGatePreparationResult{}, false, err
		}
		return agentservice.SelfDeployPlanGatePreparationResult{}, false, nil
	}
	gate, ok, err := preparer.lookupSelfDeployGateRequest(ctx, input, target, assessment.GetId())
	if err != nil || !ok {
		if err != nil {
			return agentservice.SelfDeployPlanGatePreparationResult{}, false, err
		}
		return agentservice.SelfDeployPlanGatePreparationResult{}, false, agentservice.NewSelfDeployGateRecoveryError(agentservice.SelfDeployGateRecoveryCodeExistingGateNotFound, errs.ErrNotFound)
	}
	decision, ok, err := preparer.lookupSelfDeployGateDecision(ctx, input, gate.GetId(), gate.GetStatus())
	if err != nil {
		return agentservice.SelfDeployPlanGatePreparationResult{}, false, err
	}
	context := value.GovernanceContextRef{
		RiskAssessmentRef: governanceRef("risk_assessment", assessment.GetId()),
		GateRequestRef:    governanceRef("gate_request", gate.GetId()),
	}
	status := agentservice.SelfDeployPlanGateStatusPending
	summary := strings.TrimSpace(assessment.GetExplanation())
	if ok {
		context.GateDecisionRef = governanceRef("gate_decision", decision.GetId())
		mapped, mappedOK := selfDeployGateDecisionStatus(decision.GetOutcome())
		if !mappedOK {
			return agentservice.SelfDeployPlanGatePreparationResult{}, false, agentservice.NewSelfDeployGateRecoveryError(agentservice.SelfDeployGateRecoveryCodeExistingGateDecisionMismatch, errs.ErrInvalidArgument)
		}
		status = mapped
		if reason := strings.TrimSpace(decision.GetReason()); reason != "" {
			summary = reason
		}
	}
	return agentservice.SelfDeployPlanGatePreparationResult{
		Status:            status,
		GovernanceContext: context,
		SafeSummary:       summary,
	}, true, nil
}

func (preparer *SelfDeployGatePreparer) lookupSelfDeployRiskAssessment(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput, target *governancev1.TargetRef) (*governancev1.RiskAssessment, bool, error) {
	context := projectContext(input.Plan)
	response, err := preparer.listSelfDeployRiskAssessments(ctx, input, target, context)
	if err != nil {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(
			selfDeployGateReadErrorCode(
				err,
				agentservice.SelfDeployGateRecoveryCodeExistingRiskLookupFailed,
				agentservice.SelfDeployGateRecoveryCodeExistingRiskAccessDenied,
			),
			grpcclient.MapReadError(err, "governance-manager self-deploy risk assessment lookup failed"),
		)
	}
	selected, code, err := selectSelfDeployRiskAssessment(response.GetRiskAssessments(), target.GetRef(), input.Plan.PlanFingerprint)
	if err != nil {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, err)
	}
	if selected != nil {
		return selected, true, nil
	}
	if code == agentservice.SelfDeployGateRecoveryCodeExistingRiskFingerprintMismatch {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, errs.ErrConflict)
	}
	if projectContextHasRefs(context) {
		response, err = preparer.listSelfDeployRiskAssessments(ctx, input, target, nil)
		if err != nil {
			return nil, false, agentservice.NewSelfDeployGateRecoveryError(
				selfDeployGateReadErrorCode(
					err,
					agentservice.SelfDeployGateRecoveryCodeExistingRiskLookupFailed,
					agentservice.SelfDeployGateRecoveryCodeExistingRiskAccessDenied,
				),
				grpcclient.MapReadError(err, "governance-manager self-deploy risk assessment lookup failed"),
			)
		}
		selected, code, err = selectSelfDeployRiskAssessment(response.GetRiskAssessments(), target.GetRef(), input.Plan.PlanFingerprint)
		if err != nil {
			return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, err)
		}
		if selected != nil {
			return selected, true, nil
		}
		if code == agentservice.SelfDeployGateRecoveryCodeExistingRiskFingerprintMismatch {
			return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, errs.ErrConflict)
		}
	}
	return nil, false, nil
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
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(
			selfDeployGateReadErrorCode(
				err,
				agentservice.SelfDeployGateRecoveryCodeExistingGateRequestLookupFailed,
				agentservice.SelfDeployGateRecoveryCodeExistingGateRequestAccessDenied,
			),
			grpcclient.MapReadError(err, "governance-manager self-deploy gate request lookup failed"),
		)
	}
	selected, code, err := selectSelfDeployGateRequest(response.GetGateRequests(), target.GetRef(), assessmentID)
	if err != nil {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, err)
	}
	if selected == nil {
		if code == "" {
			code = agentservice.SelfDeployGateRecoveryCodeExistingGateNotFound
		}
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, errs.ErrNotFound)
	}
	return selected, true, nil
}

func (preparer *SelfDeployGatePreparer) lookupSelfDeployGateDecision(ctx context.Context, input agentservice.SelfDeployPlanGatePreparationInput, gateRequestID string, status governancev1.GateRequestStatus) (*governancev1.GateDecision, bool, error) {
	if status != governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED {
		return nil, false, nil
	}
	gateRequestID = strings.TrimSpace(gateRequestID)
	if gateRequestID == "" {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(agentservice.SelfDeployGateRecoveryCodeExistingGateDecisionMismatch, errs.ErrNotFound)
	}
	response, err := preparer.client.ListGateDecisions(ctx, &governancev1.ListGateDecisionsRequest{
		GateRequestId: &gateRequestID,
		Page:          &governancev1.PageRequest{PageSize: 10},
		Meta:          queryMeta(input.Meta),
	})
	if err != nil {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(
			selfDeployGateReadErrorCode(
				err,
				agentservice.SelfDeployGateRecoveryCodeExistingGateDecisionLookupFailed,
				agentservice.SelfDeployGateRecoveryCodeExistingGateDecisionAccessDenied,
			),
			grpcclient.MapReadError(err, "governance-manager self-deploy gate decision lookup failed"),
		)
	}
	decision, code, err := selectSelfDeployGateDecision(response.GetGateDecisions(), gateRequestID)
	if err != nil {
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, err)
	}
	if decision == nil {
		if code == "" {
			code = agentservice.SelfDeployGateRecoveryCodeExistingGateDecisionNotFound
		}
		return nil, false, agentservice.NewSelfDeployGateRecoveryError(code, errs.ErrNotFound)
	}
	return decision, true, nil
}

func (preparer *SelfDeployGatePreparer) listSelfDeployRiskAssessments(
	ctx context.Context,
	input agentservice.SelfDeployPlanGatePreparationInput,
	target *governancev1.TargetRef,
	context *governancev1.ProjectContextRef,
) (*governancev1.ListRiskAssessmentsResponse, error) {
	status := governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE
	return preparer.client.ListRiskAssessments(ctx, &governancev1.ListRiskAssessmentsRequest{
		Target:         target,
		ProjectContext: context,
		Status:         &status,
		Page:           &governancev1.PageRequest{PageSize: 10},
		Meta:           queryMeta(input.Meta),
	})
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

func selectSelfDeployRiskAssessment(items []*governancev1.RiskAssessment, planRef string, fingerprint string) (*governancev1.RiskAssessment, string, error) {
	var selected *governancev1.RiskAssessment
	fingerprintMismatch := false
	for _, item := range items {
		if !selfDeployRiskAssessmentMatchesTarget(item, planRef) {
			continue
		}
		if !selfDeployRiskAssessmentMatchesPlanFingerprint(item, planRef, fingerprint) {
			fingerprintMismatch = true
			continue
		}
		if selected != nil {
			return nil, agentservice.SelfDeployGateRecoveryCodeExistingRiskConflict, errs.ErrConflict
		}
		selected = item
	}
	if selected != nil {
		return selected, "", nil
	}
	if fingerprintMismatch {
		return nil, agentservice.SelfDeployGateRecoveryCodeExistingRiskFingerprintMismatch, nil
	}
	return nil, "", nil
}

func selfDeployRiskAssessmentMatchesTarget(item *governancev1.RiskAssessment, planRef string) bool {
	if item == nil || strings.TrimSpace(item.GetId()) == "" {
		return false
	}
	if item.GetTarget().GetType() != governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN ||
		strings.TrimSpace(item.GetTarget().GetRef()) != strings.TrimSpace(planRef) {
		return false
	}
	return true
}

func selfDeployRiskAssessmentMatchesPlanFingerprint(item *governancev1.RiskAssessment, planRef string, fingerprint string) bool {
	for _, evidence := range item.GetEvidenceRefs() {
		if evidence.GetKind() == governancev1.EvidenceKind_EVIDENCE_KIND_SELF_DEPLOY_PLAN &&
			strings.TrimSpace(evidence.GetRef()) == strings.TrimSpace(planRef) &&
			strings.TrimSpace(evidence.GetDigest()) == strings.TrimSpace(fingerprint) {
			return true
		}
	}
	return false
}

func selectSelfDeployGateRequest(items []*governancev1.GateRequest, planRef string, assessmentID string) (*governancev1.GateRequest, string, error) {
	var selected *governancev1.GateRequest
	mismatch := false
	for _, item := range items {
		if !selfDeployGateRequestMatchesAssessmentAndTarget(item, planRef, assessmentID) {
			mismatch = mismatch || selfDeployGateRequestMatchesAssessmentID(item, assessmentID)
			continue
		}
		if !selfDeployGateRequestStatusRecoverable(item.GetStatus()) {
			mismatch = true
			continue
		}
		if selected != nil {
			return nil, agentservice.SelfDeployGateRecoveryCodeExistingGateConflict, errs.ErrConflict
		}
		selected = item
	}
	if selected != nil {
		return selected, "", nil
	}
	if mismatch {
		return nil, agentservice.SelfDeployGateRecoveryCodeExistingGateMismatch, nil
	}
	return nil, "", nil
}

func selectSelfDeployGateDecision(items []*governancev1.GateDecision, gateRequestID string) (*governancev1.GateDecision, string, error) {
	var selected *governancev1.GateDecision
	mismatch := false
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.GetId()) == "" {
			continue
		}
		if strings.TrimSpace(item.GetGateRequestId()) != strings.TrimSpace(gateRequestID) {
			mismatch = true
			continue
		}
		if item.GetOutcome() == governancev1.GateOutcome_GATE_OUTCOME_UNSPECIFIED {
			mismatch = true
			continue
		}
		if selected != nil {
			return nil, agentservice.SelfDeployGateRecoveryCodeExistingGateConflict, errs.ErrConflict
		}
		selected = item
	}
	if selected != nil {
		return selected, "", nil
	}
	if mismatch {
		return nil, agentservice.SelfDeployGateRecoveryCodeExistingGateDecisionMismatch, nil
	}
	return nil, "", nil
}

func selfDeployGateRequestMatchesAssessmentAndTarget(item *governancev1.GateRequest, planRef string, assessmentID string) bool {
	if item == nil || strings.TrimSpace(item.GetId()) == "" {
		return false
	}
	if item.GetTarget().GetType() != governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN ||
		strings.TrimSpace(item.GetTarget().GetRef()) != strings.TrimSpace(planRef) ||
		strings.TrimSpace(item.GetRiskAssessmentId()) != strings.TrimSpace(assessmentID) {
		return false
	}
	return true
}

func selfDeployGateRequestMatchesAssessmentID(item *governancev1.GateRequest, assessmentID string) bool {
	return item != nil && strings.TrimSpace(item.GetId()) != "" && strings.TrimSpace(item.GetRiskAssessmentId()) == strings.TrimSpace(assessmentID)
}

func selfDeployGateRequestStatusRecoverable(status governancev1.GateRequestStatus) bool {
	switch status {
	case governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION,
		governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED:
		return true
	default:
		return false
	}
}

func selfDeployGateLookupError(defaultCode string, err error) error {
	if err == nil {
		return nil
	}
	if code := agentservice.SelfDeployGateRecoveryErrorCode(err); code != "" {
		return err
	}
	return agentservice.NewSelfDeployGateRecoveryError(defaultCode, err)
}

func selfDeployGateReadErrorCode(err error, lookupCode string, accessDeniedCode string) string {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated:
		return accessDeniedCode
	default:
		return lookupCode
	}
}

func projectContextHasRefs(context *governancev1.ProjectContextRef) bool {
	if context == nil {
		return false
	}
	return strings.TrimSpace(context.GetProjectRef()) != "" ||
		strings.TrimSpace(context.GetRepositoryRef()) != "" ||
		strings.TrimSpace(context.GetServiceRef()) != "" ||
		strings.TrimSpace(context.GetBranchRulesRef()) != "" ||
		strings.TrimSpace(context.GetReleasePolicyRef()) != "" ||
		strings.TrimSpace(context.GetReleaseLineRef()) != ""
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

func selfDeployGateDecisionStatus(outcome governancev1.GateOutcome) (agentservice.SelfDeployPlanGateStatus, bool) {
	switch outcome {
	case governancev1.GateOutcome_GATE_OUTCOME_APPROVE,
		governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS:
		return agentservice.SelfDeployPlanGateStatusApproved, true
	case governancev1.GateOutcome_GATE_OUTCOME_REVISE,
		governancev1.GateOutcome_GATE_OUTCOME_REJECT:
		return agentservice.SelfDeployPlanGateStatusRejected, true
	case governancev1.GateOutcome_GATE_OUTCOME_HOLD,
		governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK,
		governancev1.GateOutcome_GATE_OUTCOME_ESCALATE:
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
