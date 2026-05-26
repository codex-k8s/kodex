package mcptransport

import (
	"context"
	"strings"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type governanceEnumPair[Enum comparable] struct {
	name  string
	value Enum
}

const (
	governanceRiskEvaluateDescription   = "Evaluate risk through governance-manager from safe refs and summaries."
	governanceRiskReevaluateDescription = "Reevaluate an existing risk assessment through governance-manager."
	governanceRiskGetDescription        = "Read a safe risk assessment summary through governance-manager."
	governanceRiskListDescription       = "List safe risk assessment summaries through governance-manager."

	governanceGateRequestDescription        = "Request a governance gate through governance-manager without storing decision state in MCP."
	governanceGateGetDescription            = "Read a safe governance gate request summary through governance-manager."
	governanceGateListDescription           = "List safe governance gate request summaries through governance-manager."
	governanceGateSubmitDecisionDescription = "Submit a governance gate decision through governance-manager."
	governanceGateCancelDescription         = "Cancel an open governance gate request through governance-manager."
	governanceGateExpireDescription         = "Expire an open governance gate request through governance-manager."
)

var governanceToolDescriptions = map[string]string{
	ToolGovernanceRiskEvaluate:   governanceRiskEvaluateDescription,
	ToolGovernanceRiskReevaluate: governanceRiskReevaluateDescription,
	ToolGovernanceRiskGet:        governanceRiskGetDescription,
	ToolGovernanceRiskList:       governanceRiskListDescription,

	ToolGovernanceGateRequest:        governanceGateRequestDescription,
	ToolGovernanceGateGet:            governanceGateGetDescription,
	ToolGovernanceGateList:           governanceGateListDescription,
	ToolGovernanceGateSubmitDecision: governanceGateSubmitDecisionDescription,
	ToolGovernanceGateCancel:         governanceGateCancelDescription,
	ToolGovernanceGateExpire:         governanceGateExpireDescription,
}

var governanceTargetTypes = map[string]governancev1.GovernanceTargetType{
	"transition":        governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION,
	"pull_request":      governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
	"release_candidate": governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE,
	"runtime_job":       governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB,
	"policy_change":     governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE,
	"document":          governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT,
	"merge":             governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE,
	"postdeploy":        governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY,
	"rollback":          governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK,
}

var governanceTargetTypeNames = map[governancev1.GovernanceTargetType]string{
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_TRANSITION:        "transition",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST:      "pull_request",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE: "release_candidate",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RUNTIME_JOB:       "runtime_job",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POLICY_CHANGE:     "policy_change",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_DOCUMENT:          "document",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_MERGE:             "merge",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_POSTDEPLOY:        "postdeploy",
	governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_ROLLBACK:          "rollback",
}

var governanceEvidenceKindPairs = []governanceEnumPair[governancev1.EvidenceKind]{
	{name: "provider_comment", value: governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_COMMENT},
	{name: "provider_review", value: governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW},
	{name: "provider_check", value: governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_CHECK},
	{name: "runtime_summary", value: governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY},
	{name: "document", value: governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT},
	{name: "risk_factor", value: governancev1.EvidenceKind_EVIDENCE_KIND_RISK_FACTOR},
	{name: "review_signal", value: governancev1.EvidenceKind_EVIDENCE_KIND_REVIEW_SIGNAL},
	{name: "interaction_callback", value: governancev1.EvidenceKind_EVIDENCE_KIND_INTERACTION_CALLBACK},
	{name: "object_ref", value: governancev1.EvidenceKind_EVIDENCE_KIND_OBJECT_REF},
	{name: "custom", value: governancev1.EvidenceKind_EVIDENCE_KIND_CUSTOM},
}

var governanceEvidenceKinds = governanceEnumValues(governanceEvidenceKindPairs)
var governanceEvidenceKindNames = governanceEnumNames(governanceEvidenceKindPairs)

var governanceRiskClasses = map[string]governancev1.RiskClass{
	"r0": governancev1.RiskClass_RISK_CLASS_R0,
	"r1": governancev1.RiskClass_RISK_CLASS_R1,
	"r2": governancev1.RiskClass_RISK_CLASS_R2,
	"r3": governancev1.RiskClass_RISK_CLASS_R3,
}

var governanceRiskClassNames = map[governancev1.RiskClass]string{
	governancev1.RiskClass_RISK_CLASS_R0: "r0",
	governancev1.RiskClass_RISK_CLASS_R1: "r1",
	governancev1.RiskClass_RISK_CLASS_R2: "r2",
	governancev1.RiskClass_RISK_CLASS_R3: "r3",
}

var governanceRiskAssessmentStatuses = map[string]governancev1.RiskAssessmentStatus{
	"draft":      governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_DRAFT,
	"active":     governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE,
	"superseded": governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_SUPERSEDED,
	"closed":     governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_CLOSED,
}

var governanceRiskAssessmentStatusNames = map[governancev1.RiskAssessmentStatus]string{
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_DRAFT:      "draft",
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE:     "active",
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_SUPERSEDED: "superseded",
	governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_CLOSED:     "closed",
}

var governanceRiskFactorSourceTypeNames = map[governancev1.RiskFactorSourceType]string{
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY:         "policy",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE:   "changed_file",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SERVICE:        "service",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_API:            "api",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_DATABASE:       "database",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SECRET:         "secret",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE:        "release",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RUNTIME:        "runtime",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_REVIEW_SIGNAL:  "review_signal",
	governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_HUMAN_DECISION: "human_decision",
}

var governanceGateKindNames = map[governancev1.GateKind]string{
	governancev1.GateKind_GATE_KIND_PRODUCT:      "product",
	governancev1.GateKind_GATE_KIND_ARCHITECTURE: "architecture",
	governancev1.GateKind_GATE_KIND_TECHNICAL:    "technical",
	governancev1.GateKind_GATE_KIND_QA:           "qa",
	governancev1.GateKind_GATE_KIND_RELEASE:      "release",
	governancev1.GateKind_GATE_KIND_POSTDEPLOY:   "postdeploy",
	governancev1.GateKind_GATE_KIND_EMERGENCY:    "emergency",
	governancev1.GateKind_GATE_KIND_CUSTOM:       "custom",
}

var governanceGateStatuses = map[string]governancev1.GateRequestStatus{
	"requested":         governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
	"delivering":        governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING,
	"awaiting_decision": governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION,
	"resolved":          governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED,
	"expired":           governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED,
	"cancelled":         governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED,
}

var governanceGateStatusNames = map[governancev1.GateRequestStatus]string{
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED:         "requested",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_DELIVERING:        "delivering",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION: "awaiting_decision",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED:          "resolved",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED:           "expired",
	governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED:         "cancelled",
}

var governanceGateOutcomes = map[string]governancev1.GateOutcome{
	"approve":                 governancev1.GateOutcome_GATE_OUTCOME_APPROVE,
	"approve_with_conditions": governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS,
	"revise":                  governancev1.GateOutcome_GATE_OUTCOME_REVISE,
	"reject":                  governancev1.GateOutcome_GATE_OUTCOME_REJECT,
	"hold":                    governancev1.GateOutcome_GATE_OUTCOME_HOLD,
	"rollback":                governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK,
	"escalate":                governancev1.GateOutcome_GATE_OUTCOME_ESCALATE,
}

var governanceGateOutcomeNames = map[governancev1.GateOutcome]string{
	governancev1.GateOutcome_GATE_OUTCOME_APPROVE:                 "approve",
	governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS: "approve_with_conditions",
	governancev1.GateOutcome_GATE_OUTCOME_REVISE:                  "revise",
	governancev1.GateOutcome_GATE_OUTCOME_REJECT:                  "reject",
	governancev1.GateOutcome_GATE_OUTCOME_HOLD:                    "hold",
	governancev1.GateOutcome_GATE_OUTCOME_ROLLBACK:                "rollback",
	governancev1.GateOutcome_GATE_OUTCOME_ESCALATE:                "escalate",
}

// GovernanceToolsHandler routes gate lifecycle tools to governance-manager.
type GovernanceToolsHandler struct {
	client GovernanceManagerClient
}

// NewGovernanceToolsHandler creates a governance tool handler.
func NewGovernanceToolsHandler(client GovernanceManagerClient) *GovernanceToolsHandler {
	return &GovernanceToolsHandler{client: client}
}

func (handler *GovernanceToolsHandler) EvaluateRisk(ctx context.Context, _ *mcpsdk.CallToolRequest, input EvaluateGovernanceRiskInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	return routeOwnerTool(ctx, input, evaluateRiskRequest, handler.client.EvaluateRisk, governanceRiskAssessmentOutput, ToolGovernanceRiskEvaluate)
}

func (handler *GovernanceToolsHandler) ReevaluateRisk(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReevaluateGovernanceRiskInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	return routeOwnerTool(ctx, input, reevaluateRiskRequest, handler.client.ReevaluateRisk, governanceRiskAssessmentOutput, ToolGovernanceRiskReevaluate)
}

func (handler *GovernanceToolsHandler) GetRiskAssessment(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceRiskAssessmentInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentOutput, error) {
	return routeOwnerTool(ctx, input, getRiskAssessmentRequest, handler.client.GetRiskAssessment, governanceRiskAssessmentOutput, ToolGovernanceRiskGet)
}

func (handler *GovernanceToolsHandler) ListRiskAssessments(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceRiskAssessmentsInput) (*mcpsdk.CallToolResult, GovernanceRiskAssessmentListOutput, error) {
	return routeOwnerTool(ctx, input, listRiskAssessmentsRequest, handler.client.ListRiskAssessments, governanceRiskAssessmentListOutput, ToolGovernanceRiskList)
}

func (handler *GovernanceToolsHandler) RequestGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input RequestGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, requestGateRequest, handler.client.RequestGate, governanceGateOutput, ToolGovernanceGateRequest)
}

func (handler *GovernanceToolsHandler) GetGateRequest(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, getGateRequest, handler.client.GetGateRequest, governanceGateOutput, ToolGovernanceGateGet)
}

func (handler *GovernanceToolsHandler) ListGateRequests(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListGovernanceGatesInput) (*mcpsdk.CallToolResult, GovernanceGateListOutput, error) {
	return routeOwnerTool(ctx, input, listGateRequests, handler.client.ListGateRequests, governanceGateListOutput, ToolGovernanceGateList)
}

func (handler *GovernanceToolsHandler) SubmitGateDecision(ctx context.Context, _ *mcpsdk.CallToolRequest, input SubmitGovernanceGateDecisionInput) (*mcpsdk.CallToolResult, GovernanceGateDecisionOutput, error) {
	return routeOwnerTool(ctx, input, submitGateDecisionRequest, handler.client.SubmitGateDecision, governanceGateDecisionOutput, ToolGovernanceGateSubmitDecision)
}

func (handler *GovernanceToolsHandler) CancelGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input CancelGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, cancelGateRequest, handler.client.CancelGate, governanceGateOutput, ToolGovernanceGateCancel)
}

func (handler *GovernanceToolsHandler) ExpireGate(ctx context.Context, _ *mcpsdk.CallToolRequest, input ExpireGovernanceGateInput) (*mcpsdk.CallToolResult, GovernanceGateOutput, error) {
	return routeOwnerTool(ctx, input, expireGateRequest, handler.client.ExpireGate, governanceGateOutput, ToolGovernanceGateExpire)
}

func evaluateRiskRequest(input EvaluateGovernanceRiskInput) (*governancev1.EvaluateRiskRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	evaluationSummary, err := governanceRiskEvaluationSummary(input.EvaluationSummary)
	if err != nil {
		return nil, err
	}
	return &governancev1.EvaluateRiskRequest{
		Target:            target,
		ProjectContext:    governanceProjectContext(input.ProjectContext, true),
		ProviderContext:   governanceProviderContext(input.ProviderContext),
		AgentContext:      governanceAgentContext(input.AgentContext),
		RuntimeContext:    governanceRuntimeContext(input.RuntimeContext),
		EvidenceRefs:      evidenceRefs,
		RiskProfileRef:    optionalString(input.RiskProfileRef),
		Meta:              meta,
		EvaluationSummary: evaluationSummary,
	}, nil
}

func reevaluateRiskRequest(input ReevaluateGovernanceRiskInput) (*governancev1.ReevaluateRiskRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := requiredTrimmed(input.RiskAssessmentID, "risk_assessment_id")
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.NewEvidenceRefs)
	if err != nil {
		return nil, err
	}
	reevaluationReason, err := requiredTrimmed(input.ReevaluationReason, "reevaluation_reason")
	if err != nil {
		return nil, err
	}
	evaluationSummary, err := governanceRiskEvaluationSummary(input.EvaluationSummary)
	if err != nil {
		return nil, err
	}
	return &governancev1.ReevaluateRiskRequest{
		RiskAssessmentId:   riskAssessmentID,
		NewEvidenceRefs:    evidenceRefs,
		ReevaluationReason: reevaluationReason,
		Meta:               meta,
		EvaluationSummary:  evaluationSummary,
		RiskProfileRef:     optionalString(input.RiskProfileRef),
	}, nil
}

func getRiskAssessmentRequest(input GetGovernanceRiskAssessmentInput) (*governancev1.GetRiskAssessmentRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := requiredTrimmed(input.RiskAssessmentID, "risk_assessment_id")
	if err != nil {
		return nil, err
	}
	return &governancev1.GetRiskAssessmentRequest{
		RiskAssessmentId:     riskAssessmentID,
		IncludeFactors:       input.IncludeFactors,
		IncludeReviewSignals: input.IncludeReviewSignals,
		Meta:                 meta,
	}, nil
}

func listRiskAssessmentsRequest(input ListGovernanceRiskAssessmentsInput) (*governancev1.ListRiskAssessmentsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, false, "target")
	if err != nil {
		return nil, err
	}
	projectContext := governanceProjectContext(input.ProjectContext, false)
	if target == nil && !governanceProjectContextHasListScope(input.ProjectContext) {
		return nil, invalidInput("target or project_context.project_ref/repository_ref is required")
	}
	riskClass, err := optionalGovernanceRiskClass(input.EffectiveRiskClass)
	if err != nil {
		return nil, err
	}
	status, err := optionalGovernanceRiskAssessmentStatus(input.Status)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListRiskAssessmentsRequest{
		Target:             target,
		ProjectContext:     projectContext,
		EffectiveRiskClass: riskClass,
		Status:             status,
		Page:               governancePageRequest(input.Page),
		Meta:               meta,
	}, nil
}

func requestGateRequest(input RequestGovernanceGateInput) (*governancev1.RequestGateRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	evidenceRefs, err := governanceEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	evidenceSummary, err := requiredTrimmed(input.EvidenceSummary, "evidence_summary")
	if err != nil {
		return nil, err
	}
	return &governancev1.RequestGateRequest{
		RiskAssessmentId:       optionalString(input.RiskAssessmentID),
		GatePolicyId:           optionalString(input.GatePolicyID),
		Target:                 target,
		InteractionDeliveryRef: governanceInteractionDeliveryRef(input.InteractionDeliveryRef),
		EvidenceRefs:           evidenceRefs,
		EvidenceSummary:        evidenceSummary,
		DeadlineAt:             optionalString(input.DeadlineAt),
		Meta:                   meta,
	}, nil
}

func getGateRequest(input GetGovernanceGateInput) (*governancev1.GetGateRequestRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	gateRequestID, err := requiredTrimmed(input.GateRequestID, "gate_request_id")
	if err != nil {
		return nil, err
	}
	return &governancev1.GetGateRequestRequest{
		GateRequestId:   gateRequestID,
		IncludeDecision: input.IncludeDecision,
		Meta:            meta,
	}, nil
}

func listGateRequests(input ListGovernanceGatesInput) (*governancev1.ListGateRequestsRequest, error) {
	meta, err := governanceQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := governanceTarget(input.Target, false, "target")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.RiskAssessmentID) == "" && target == nil {
		return nil, invalidInput("risk_assessment_id or target is required")
	}
	status, err := optionalGovernanceGateStatus(input.Status)
	if err != nil {
		return nil, err
	}
	return &governancev1.ListGateRequestsRequest{
		RiskAssessmentId: optionalString(input.RiskAssessmentID),
		Target:           target,
		Status:           status,
		Page:             governancePageRequest(input.Page),
		Meta:             meta,
	}, nil
}

func submitGateDecisionRequest(input SubmitGovernanceGateDecisionInput) (*governancev1.SubmitGateDecisionRequest, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	gateRequestID, err := requiredTrimmed(input.GateRequestID, "gate_request_id")
	if err != nil {
		return nil, err
	}
	decisionActorRef, err := requiredTrimmed(input.DecisionActorRef, "decision_actor_ref")
	if err != nil {
		return nil, err
	}
	decisionPolicyRef, err := requiredTrimmed(input.DecisionPolicyRef, "decision_policy_ref")
	if err != nil {
		return nil, err
	}
	outcome, err := governanceGateOutcome(input.Outcome)
	if err != nil {
		return nil, err
	}
	reason, err := requiredTrimmed(input.Reason, "reason")
	if err != nil {
		return nil, err
	}
	return &governancev1.SubmitGateDecisionRequest{
		GateRequestId:          gateRequestID,
		DecisionActorRef:       decisionActorRef,
		DecisionPolicyRef:      decisionPolicyRef,
		Outcome:                outcome,
		Reason:                 reason,
		ConditionsSummary:      optionalString(input.ConditionsSummary),
		InteractionDeliveryRef: governanceInteractionDeliveryRef(input.InteractionDeliveryRef),
		Meta:                   meta,
	}, nil
}

func cancelGateRequest(input CancelGovernanceGateInput) (*governancev1.CancelGateRequest, error) {
	return terminalGovernanceGateRequest(input, func(gateRequestID string, reason string, meta *governancev1.CommandMeta, ref *governancev1.InteractionDeliveryRef) *governancev1.CancelGateRequest {
		return &governancev1.CancelGateRequest{
			GateRequestId:          gateRequestID,
			Reason:                 reason,
			InteractionDeliveryRef: ref,
			Meta:                   meta,
		}
	})
}

func expireGateRequest(input ExpireGovernanceGateInput) (*governancev1.ExpireGateRequest, error) {
	gateRequestID, reason, meta, ref, err := terminalGovernanceGateInput(input)
	if err != nil {
		return nil, err
	}
	request := &governancev1.ExpireGateRequest{Meta: meta}
	request.GateRequestId = gateRequestID
	request.Reason = reason
	request.InteractionDeliveryRef = ref
	return request, nil
}

func terminalGovernanceGateRequest[Request any](
	input CancelGovernanceGateInput,
	build func(string, string, *governancev1.CommandMeta, *governancev1.InteractionDeliveryRef) Request,
) (Request, error) {
	var zero Request
	gateRequestID, reason, meta, ref, err := terminalGovernanceGateInput(input)
	if err != nil {
		return zero, err
	}
	return build(gateRequestID, reason, meta, ref), nil
}

func terminalGovernanceGateInput(input CancelGovernanceGateInput) (string, string, *governancev1.CommandMeta, *governancev1.InteractionDeliveryRef, error) {
	meta, err := governanceCommandMeta(input.Meta)
	if err != nil {
		return "", "", nil, nil, err
	}
	gateRequestID, err := requiredTrimmed(input.GateRequestID, "gate_request_id")
	if err != nil {
		return "", "", nil, nil, err
	}
	reason, err := requiredTrimmed(input.Reason, "reason")
	if err != nil {
		return "", "", nil, nil, err
	}
	return gateRequestID, reason, meta, governanceInteractionDeliveryRef(input.InteractionDeliveryRef), nil
}

func governanceCommandMeta(input GovernanceCommandMetaInput) (*governancev1.CommandMeta, error) {
	actorValue, contextValue, requestID, err := governanceMetaBase(input.Actor, input.RequestContext, input.RequestID)
	if err != nil {
		return nil, err
	}
	if !governanceHasReplayKey(input) {
		return nil, invalidInput("command_id or idempotency_key is required")
	}
	meta := &governancev1.CommandMeta{Actor: actorValue, RequestId: requestID, RequestContext: contextValue}
	meta.CommandId = optionalString(input.CommandID)
	meta.IdempotencyKey = optionalString(input.IdempotencyKey)
	meta.ExpectedVersion = input.ExpectedVersion
	meta.Reason = strings.TrimSpace(input.Reason)
	return meta, nil
}

func governanceQueryMeta(input GovernanceQueryMetaInput) (*governancev1.QueryMeta, error) {
	actorValue, contextValue, requestID, err := governanceMetaBase(input.Actor, input.RequestContext, input.RequestID)
	if err != nil {
		return nil, err
	}
	return &governancev1.QueryMeta{Actor: actorValue, RequestId: requestID, RequestContext: contextValue}, nil
}

func governanceMetaBase(actorInput GovernanceActorInput, contextInput GovernanceRequestContextInput, requestIDInput string) (*governancev1.Actor, *governancev1.RequestContext, string, error) {
	actorValue, err := governanceActor(actorInput)
	if err != nil {
		return nil, nil, "", err
	}
	contextValue, err := governanceRequestContext(contextInput)
	if err != nil {
		return nil, nil, "", err
	}
	requestID := strings.TrimSpace(requestIDInput)
	if requestID == "" {
		return nil, nil, "", invalidInput("request_id is required")
	}
	return actorValue, contextValue, requestID, nil
}

func governanceHasReplayKey(input GovernanceCommandMetaInput) bool {
	return strings.TrimSpace(input.CommandID) != "" || strings.TrimSpace(input.IdempotencyKey) != ""
}

func governanceActor(input GovernanceActorInput) (*governancev1.Actor, error) {
	actorType, actorID, err := actorFields(input.Type, input.ID)
	if err != nil {
		return nil, err
	}
	return &governancev1.Actor{Type: actorType, Id: actorID}, nil
}

func governanceRequestContext(input GovernanceRequestContextInput) (*governancev1.RequestContext, error) {
	source, err := safeRequestSource(input.Source)
	if err != nil {
		return nil, err
	}
	contextValue := &governancev1.RequestContext{}
	contextValue.Source = source
	contextValue.TraceId = optionalString(input.TraceID)
	contextValue.SessionId = optionalString(input.SessionID)
	contextValue.ClientIpHash = optionalString(input.ClientIPHash)
	return contextValue, nil
}

func governanceTarget(input GovernanceTargetInput, required bool, field string) (*governancev1.TargetRef, error) {
	empty := strings.TrimSpace(input.Type) == "" && strings.TrimSpace(input.Ref) == ""
	if empty && !required {
		return nil, nil
	}
	if strings.TrimSpace(input.Ref) == "" {
		return nil, invalidInput(field + ".ref is required")
	}
	targetType, err := governanceTargetType(input.Type, field+".type")
	if err != nil {
		return nil, err
	}
	return &governancev1.TargetRef{Type: targetType, Ref: strings.TrimSpace(input.Ref)}, nil
}

func governanceInteractionDeliveryRef(input GovernanceInteractionDeliveryRefInput) *governancev1.InteractionDeliveryRef {
	return &governancev1.InteractionDeliveryRef{
		RequestRef:  optionalString(input.RequestRef),
		DeliveryRef: optionalString(input.DeliveryRef),
		CallbackRef: optionalString(input.CallbackRef),
		DecisionRef: optionalString(input.DecisionRef),
	}
}

func governanceEvidenceRefs(inputs []GovernanceEvidenceRefInput) ([]*governancev1.EvidenceRef, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	result := make([]*governancev1.EvidenceRef, 0, len(inputs))
	for _, input := range inputs {
		kind, err := governanceEvidenceKind(input.Kind, "evidence_refs.kind")
		if err != nil {
			return nil, invalidInput("evidence_refs contains invalid kind")
		}
		ref, err := requiredTrimmed(input.Ref, "evidence_refs.ref")
		if err != nil {
			return nil, err
		}
		summary, err := requiredTrimmed(input.Summary, "evidence_refs.summary")
		if err != nil {
			return nil, err
		}
		result = append(result, &governancev1.EvidenceRef{
			Kind:           kind,
			Ref:            ref,
			Summary:        summary,
			Digest:         optionalString(input.Digest),
			RetentionClass: optionalString(input.RetentionClass),
		})
	}
	return result, nil
}

func governanceProjectContext(input GovernanceProjectContextRefInput, includeEmpty bool) *governancev1.ProjectContextRef {
	if !includeEmpty && governanceProjectContextEmpty(input) {
		return nil
	}
	return &governancev1.ProjectContextRef{
		ProjectRef:       optionalString(input.ProjectRef),
		RepositoryRef:    optionalString(input.RepositoryRef),
		ServiceRef:       optionalString(input.ServiceRef),
		BranchRulesRef:   optionalString(input.BranchRulesRef),
		ReleasePolicyRef: optionalString(input.ReleasePolicyRef),
		ReleaseLineRef:   optionalString(input.ReleaseLineRef),
	}
}

func governanceProjectContextEmpty(input GovernanceProjectContextRefInput) bool {
	return strings.TrimSpace(input.ProjectRef) == "" &&
		strings.TrimSpace(input.RepositoryRef) == "" &&
		strings.TrimSpace(input.ServiceRef) == "" &&
		strings.TrimSpace(input.BranchRulesRef) == "" &&
		strings.TrimSpace(input.ReleasePolicyRef) == "" &&
		strings.TrimSpace(input.ReleaseLineRef) == ""
}

func governanceProjectContextHasListScope(input GovernanceProjectContextRefInput) bool {
	return strings.TrimSpace(input.ProjectRef) != "" || strings.TrimSpace(input.RepositoryRef) != ""
}

func governanceProviderContext(input GovernanceProviderContextRefInput) *governancev1.ProviderContextRef {
	refs := optionalRefValues(
		input.WorkItemRef,
		input.PullRequestRef,
		input.CommentRef,
		input.ReviewSignalRef,
		input.ProviderOperationRef,
		input.ChangedFilesSummaryRef,
	)
	return &governancev1.ProviderContextRef{
		WorkItemRef:            refs[0],
		PullRequestRef:         refs[1],
		CommentRef:             refs[2],
		ReviewSignalRef:        refs[3],
		ProviderOperationRef:   refs[4],
		ChangedFilesSummaryRef: refs[5],
	}
}

func governanceAgentContext(input GovernanceAgentContextRefInput) *governancev1.AgentContextRef {
	refs := optionalRefValues(input.SessionRef, input.RunRef, input.StageRef, input.AcceptanceRef, input.RoleRef)
	return &governancev1.AgentContextRef{
		SessionRef:    refs[0],
		RunRef:        refs[1],
		StageRef:      refs[2],
		AcceptanceRef: refs[3],
		RoleRef:       refs[4],
	}
}

func governanceRuntimeContext(input GovernanceRuntimeContextRefInput) *governancev1.RuntimeContextRef {
	return &governancev1.RuntimeContextRef{
		SlotRef:        optionalString(input.SlotRef),
		JobRef:         optionalString(input.JobRef),
		EnvironmentRef: optionalString(input.EnvironmentRef),
		ArtifactRef:    optionalString(input.ArtifactRef),
		SummaryRef:     optionalString(input.SummaryRef),
	}
}

func governanceRiskEvaluationSummary(input GovernanceRiskEvaluationSummaryInput) (*governancev1.RiskEvaluationSummary, error) {
	factors, err := governanceRiskEvaluationFactors(input.Factors)
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskEvaluationSummary{
		ChangedFilesSummaryRef: optionalString(input.ChangedFilesSummaryRef),
		Summary:                strings.TrimSpace(input.Summary),
		Factors:                factors,
	}, nil
}

func governanceRiskEvaluationFactors(inputs []GovernanceRiskEvaluationFactorInput) ([]*governancev1.RiskEvaluationFactor, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	result := make([]*governancev1.RiskEvaluationFactor, 0, len(inputs))
	for _, input := range inputs {
		sourceType, err := governanceRiskFactorSourceType(input.SourceType, "evaluation_summary.factors.source_type")
		if err != nil {
			return nil, err
		}
		ref, err := requiredTrimmed(input.Ref, "evaluation_summary.factors.ref")
		if err != nil {
			return nil, err
		}
		summary, err := requiredTrimmed(input.Summary, "evaluation_summary.factors.summary")
		if err != nil {
			return nil, err
		}
		result = append(result, &governancev1.RiskEvaluationFactor{
			SourceType: sourceType,
			Ref:        ref,
			Summary:    summary,
			Tags:       trimmedStrings(input.Tags),
		})
	}
	return result, nil
}

func trimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func optionalRefValues(values ...string) []*string {
	result := make([]*string, len(values))
	for index, value := range values {
		result[index] = optionalString(value)
	}
	return result
}

func governancePageRequest(input GovernancePageInput) *governancev1.PageRequest {
	return &governancev1.PageRequest{
		PageSize:  input.PageSize,
		PageToken: optionalString(input.PageToken),
	}
}

func governanceRiskAssessmentOutput(response *governancev1.RiskAssessmentResponse) GovernanceRiskAssessmentOutput {
	if response == nil {
		return GovernanceRiskAssessmentOutput{}
	}
	factors := response.GetRiskFactors()
	return GovernanceRiskAssessmentOutput{
		RiskAssessment:    governanceRiskAssessmentSummary(response.GetRiskAssessment(), factors),
		RiskFactors:       governanceRiskFactorSummaries(factors),
		ReviewSignalCount: len(response.GetReviewSignals()),
	}
}

func governanceRiskAssessmentListOutput(response *governancev1.ListRiskAssessmentsResponse) GovernanceRiskAssessmentListOutput {
	if response == nil {
		return GovernanceRiskAssessmentListOutput{}
	}
	return GovernanceRiskAssessmentListOutput{
		RiskAssessments: governanceRiskAssessmentSummaries(response.GetRiskAssessments()),
		Page:            governancePageSummary(response.GetPage()),
	}
}

func governanceRiskAssessmentSummaries(assessments []*governancev1.RiskAssessment) []GovernanceRiskAssessmentSummary {
	return summarizeItems(assessments, func(assessment *governancev1.RiskAssessment) GovernanceRiskAssessmentSummary {
		return governanceRiskAssessmentSummary(assessment, nil)
	})
}

func governanceRiskAssessmentSummary(assessment *governancev1.RiskAssessment, factors []*governancev1.RiskFactor) GovernanceRiskAssessmentSummary {
	if assessment == nil {
		return GovernanceRiskAssessmentSummary{}
	}
	requiredGates := governanceRequiredGateSummaries(assessment.GetRequiredGates())
	matchedRuleRefs := governanceMatchedRuleRefs(factors)
	return GovernanceRiskAssessmentSummary{
		ID:                 assessment.GetId(),
		Target:             governanceTargetSummary(assessment.GetTarget()),
		ProjectContext:     governanceProjectContextSummary(assessment.GetProjectContext()),
		ProviderContext:    governanceProviderContextSummary(assessment.GetProviderContext()),
		AgentContext:       governanceAgentContextSummary(assessment.GetAgentContext()),
		RuntimeContext:     governanceRuntimeContextSummary(assessment.GetRuntimeContext()),
		InitialRiskClass:   governanceRiskClassName(assessment.GetInitialRiskClass()),
		EffectiveRiskClass: governanceRiskClassName(assessment.GetEffectiveRiskClass()),
		Status:             governanceRiskAssessmentStatusName(assessment.GetStatus()),
		Summary:            assessment.GetExplanation(),
		RequiredGates:      requiredGates,
		RequiredGateCount:  len(requiredGates),
		RequiredGateRefs:   governanceRequiredGateRefs(requiredGates),
		MatchedRuleRefs:    matchedRuleRefs,
		MatchedRuleCount:   len(matchedRuleRefs),
		RiskFactorCount:    len(factors),
		RiskProfileID:      assessment.GetRiskProfileId(),
		RiskProfileVersion: assessment.GetRiskProfileVersion(),
		EvaluationSummary:  governanceEvaluationSummarySummary(assessment.GetEvaluationSummary()),
		EvidenceRefs:       governanceEvidenceSummaries(assessment.GetEvidenceRefs()),
		Version:            assessment.GetVersion(),
		CreatedAt:          assessment.GetCreatedAt(),
		UpdatedAt:          assessment.GetUpdatedAt(),
	}
}

func governanceRequiredGateSummaries(gates []*governancev1.RequiredGate) []GovernanceRequiredGateSummary {
	return summarizeItems(gates, func(gate *governancev1.RequiredGate) GovernanceRequiredGateSummary {
		if gate == nil {
			return GovernanceRequiredGateSummary{}
		}
		return GovernanceRequiredGateSummary{
			GatePolicyID: gate.GetGatePolicyId(),
			GateKind:     governanceGateKindName(gate.GetGateKind()),
			MinRiskClass: governanceRiskClassName(gate.GetMinRiskClass()),
			Reason:       gate.GetReason(),
		}
	})
}

func governanceRequiredGateRefs(gates []GovernanceRequiredGateSummary) []string {
	if len(gates) == 0 {
		return nil
	}
	result := make([]string, 0, len(gates))
	seen := make(map[string]struct{}, len(gates))
	for _, gate := range gates {
		ref := strings.TrimSpace(gate.GatePolicyID)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func governanceRiskFactorSummaries(factors []*governancev1.RiskFactor) []GovernanceRiskFactorSummary {
	return summarizeItems(factors, func(factor *governancev1.RiskFactor) GovernanceRiskFactorSummary {
		if factor == nil {
			return GovernanceRiskFactorSummary{}
		}
		return GovernanceRiskFactorSummary{
			ID:               factor.GetId(),
			RiskAssessmentID: factor.GetRiskAssessmentId(),
			SourceType:       governanceRiskFactorSourceTypeName(factor.GetSourceType()),
			SourceRef:        factor.GetSourceRef(),
			RiskClass:        governanceRiskClassName(factor.GetRiskClass()),
			Summary:          factor.GetSummary(),
			CreatedAt:        factor.GetCreatedAt(),
		}
	})
}

func governanceMatchedRuleRefs(factors []*governancev1.RiskFactor) []string {
	if len(factors) == 0 {
		return nil
	}
	result := make([]string, 0, len(factors))
	seen := make(map[string]struct{}, len(factors))
	for _, factor := range factors {
		if factor.GetSourceType() != governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY {
			continue
		}
		ref := strings.TrimSpace(factor.GetSourceRef())
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func governanceEvaluationSummarySummary(summary *governancev1.RiskEvaluationSummary) GovernanceEvaluationSummarySummary {
	if summary == nil {
		return GovernanceEvaluationSummarySummary{}
	}
	return GovernanceEvaluationSummarySummary{
		ChangedFilesSummaryRef: summary.GetChangedFilesSummaryRef(),
		Summary:                summary.GetSummary(),
		FactorCount:            len(summary.GetFactors()),
	}
}

func governanceGateOutput(response *governancev1.GateRequestResponse) GovernanceGateOutput {
	if response == nil {
		return GovernanceGateOutput{}
	}
	output := GovernanceGateOutput{GateRequest: governanceGateRequestSummary(response.GetGateRequest())}
	if response.GetGateDecision() != nil {
		decision := governanceGateDecisionSummary(response.GetGateDecision())
		output.GateDecision = &decision
	}
	return output
}

func governanceGateDecisionOutput(response *governancev1.GateDecisionResponse) GovernanceGateDecisionOutput {
	if response == nil {
		return GovernanceGateDecisionOutput{}
	}
	return GovernanceGateDecisionOutput{
		GateDecision: governanceGateDecisionSummary(response.GetGateDecision()),
		GateRequest:  governanceGateRequestSummary(response.GetGateRequest()),
	}
}

func governanceGateListOutput(response *governancev1.ListGateRequestsResponse) GovernanceGateListOutput {
	if response == nil {
		return GovernanceGateListOutput{}
	}
	return GovernanceGateListOutput{
		GateRequests: governanceGateRequestSummaries(response.GetGateRequests()),
		Page:         governancePageSummary(response.GetPage()),
	}
}

func governanceGateRequestSummaries(requests []*governancev1.GateRequest) []GovernanceGateRequestSummary {
	return summarizeItems(requests, governanceGateRequestSummary)
}

func governanceGateRequestSummary(request *governancev1.GateRequest) GovernanceGateRequestSummary {
	if request == nil {
		return GovernanceGateRequestSummary{}
	}
	return GovernanceGateRequestSummary{
		ID:                     request.GetId(),
		RiskAssessmentID:       request.GetRiskAssessmentId(),
		GatePolicyID:           request.GetGatePolicyId(),
		Target:                 governanceTargetSummary(request.GetTarget()),
		InteractionDeliveryRef: governanceInteractionDeliverySummary(request.GetInteractionDeliveryRef()),
		EvidenceRefs:           governanceEvidenceSummaries(request.GetEvidenceRefs()),
		EvidenceSummary:        request.GetEvidenceSummary(),
		Status:                 governanceGateStatusName(request.GetStatus()),
		Version:                request.GetVersion(),
		CreatedAt:              request.GetCreatedAt(),
		UpdatedAt:              request.GetUpdatedAt(),
		TerminalActorRef:       request.GetTerminalActorRef(),
		TerminalReason:         request.GetTerminalReason(),
		TerminalAt:             request.GetTerminalAt(),
	}
}

func governanceGateDecisionSummary(decision *governancev1.GateDecision) GovernanceGateDecisionSummary {
	if decision == nil {
		return GovernanceGateDecisionSummary{}
	}
	return GovernanceGateDecisionSummary{
		ID:                decision.GetId(),
		GateRequestID:     decision.GetGateRequestId(),
		DecisionActorRef:  decision.GetDecisionActorRef(),
		DecisionPolicyRef: decision.GetDecisionPolicyRef(),
		Outcome:           governanceGateOutcomeName(decision.GetOutcome()),
		Reason:            decision.GetReason(),
		ConditionsSummary: decision.GetConditionsSummary(),
		SourceRef:         decision.GetSourceRef(),
		DecidedAt:         decision.GetDecidedAt(),
	}
}

func governanceProjectContextSummary(contextRef *governancev1.ProjectContextRef) GovernanceProjectContextSummary {
	if contextRef == nil {
		return GovernanceProjectContextSummary{}
	}
	return GovernanceProjectContextSummary{
		ProjectRef:       contextRef.GetProjectRef(),
		RepositoryRef:    contextRef.GetRepositoryRef(),
		ServiceRef:       contextRef.GetServiceRef(),
		BranchRulesRef:   contextRef.GetBranchRulesRef(),
		ReleasePolicyRef: contextRef.GetReleasePolicyRef(),
		ReleaseLineRef:   contextRef.GetReleaseLineRef(),
	}
}

func governanceProviderContextSummary(contextRef *governancev1.ProviderContextRef) GovernanceProviderContextSummary {
	if contextRef == nil {
		return GovernanceProviderContextSummary{}
	}
	summary := GovernanceProviderContextSummary{}
	summary.WorkItemRef = contextRef.GetWorkItemRef()
	summary.PullRequestRef = contextRef.GetPullRequestRef()
	summary.CommentRef = contextRef.GetCommentRef()
	summary.ReviewSignalRef = contextRef.GetReviewSignalRef()
	summary.ProviderOperationRef = contextRef.GetProviderOperationRef()
	summary.ChangedFilesSummaryRef = contextRef.GetChangedFilesSummaryRef()
	return summary
}

func governanceAgentContextSummary(contextRef *governancev1.AgentContextRef) GovernanceAgentContextSummary {
	if contextRef == nil {
		return GovernanceAgentContextSummary{}
	}
	return GovernanceAgentContextSummary{
		SessionRef:    contextRef.GetSessionRef(),
		RunRef:        contextRef.GetRunRef(),
		StageRef:      contextRef.GetStageRef(),
		AcceptanceRef: contextRef.GetAcceptanceRef(),
		RoleRef:       contextRef.GetRoleRef(),
	}
}

func governanceRuntimeContextSummary(contextRef *governancev1.RuntimeContextRef) GovernanceRuntimeContextSummary {
	if contextRef == nil {
		return GovernanceRuntimeContextSummary{}
	}
	summary := GovernanceRuntimeContextSummary{}
	summary.SlotRef = contextRef.GetSlotRef()
	summary.JobRef = contextRef.GetJobRef()
	summary.EnvironmentRef = contextRef.GetEnvironmentRef()
	summary.ArtifactRef = contextRef.GetArtifactRef()
	summary.SummaryRef = contextRef.GetSummaryRef()
	return summary
}

func governanceTargetSummary(target *governancev1.TargetRef) GovernanceTargetSummary {
	if target == nil {
		return GovernanceTargetSummary{}
	}
	return GovernanceTargetSummary{Type: governanceTargetTypeName(target.GetType()), Ref: target.GetRef()}
}

func governanceInteractionDeliverySummary(ref *governancev1.InteractionDeliveryRef) GovernanceInteractionDeliverySummary {
	if ref == nil {
		return GovernanceInteractionDeliverySummary{}
	}
	return GovernanceInteractionDeliverySummary{
		RequestRef:  ref.GetRequestRef(),
		DeliveryRef: ref.GetDeliveryRef(),
		CallbackRef: ref.GetCallbackRef(),
		DecisionRef: ref.GetDecisionRef(),
	}
}

func governanceEvidenceSummaries(evidenceRefs []*governancev1.EvidenceRef) []GovernanceEvidenceSummary {
	return summarizeItems(evidenceRefs, governanceEvidenceSummary)
}

func governanceEvidenceSummary(evidence *governancev1.EvidenceRef) GovernanceEvidenceSummary {
	if evidence == nil {
		return GovernanceEvidenceSummary{}
	}
	return GovernanceEvidenceSummary{
		Kind:           governanceEvidenceKindName(evidence.GetKind()),
		Ref:            evidence.GetRef(),
		Summary:        evidence.GetSummary(),
		Digest:         evidence.GetDigest(),
		RetentionClass: evidence.GetRetentionClass(),
	}
}

func governancePageSummary(page *governancev1.PageResponse) PageSummary {
	if page == nil {
		return PageSummary{}
	}
	return PageSummary{NextPageToken: page.GetNextPageToken()}
}

func governanceTargetType(value string, field string) (governancev1.GovernanceTargetType, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceTargetTypes, governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_UNSPECIFIED, field)
}

func governanceEvidenceKind(value string, field string) (governancev1.EvidenceKind, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceEvidenceKinds, governancev1.EvidenceKind_EVIDENCE_KIND_UNSPECIFIED, field)
}

func optionalGovernanceRiskClass(value string) (*governancev1.RiskClass, error) {
	return optionalGovernanceEnum(value, "effective_risk_class", governanceRiskClasses, governancev1.RiskClass_RISK_CLASS_UNSPECIFIED)
}

func optionalGovernanceRiskAssessmentStatus(value string) (*governancev1.RiskAssessmentStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceRiskAssessmentStatuses, governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_UNSPECIFIED)
}

func governanceRiskFactorSourceType(value string, field string) (governancev1.RiskFactorSourceType, error) {
	key := governanceEnumKey(value)
	if key == "" {
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_UNSPECIFIED, invalidInput(field + " is required")
	}
	switch key {
	case "policy":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY, nil
	case "changed_file":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE, nil
	case "service":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SERVICE, nil
	case "api":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_API, nil
	case "database":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_DATABASE, nil
	case "secret":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_SECRET, nil
	case "release":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE, nil
	case "runtime":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RUNTIME, nil
	case "review_signal":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_REVIEW_SIGNAL, nil
	case "human_decision":
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_HUMAN_DECISION, nil
	default:
		return governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_UNSPECIFIED, invalidInput(field + " is invalid")
	}
}

func optionalGovernanceGateStatus(value string) (*governancev1.GateRequestStatus, error) {
	return optionalGovernanceEnum(value, "status", governanceGateStatuses, governancev1.GateRequestStatus_GATE_REQUEST_STATUS_UNSPECIFIED)
}

func optionalGovernanceEnum[Enum comparable](value string, field string, values map[string]Enum, zero Enum) (*Enum, error) {
	key := governanceEnumKey(value)
	if key == "" {
		return nil, nil
	}
	enumValue, ok := values[key]
	if !ok || enumValue == zero {
		return nil, invalidInput(field + " is invalid")
	}
	return &enumValue, nil
}

func governanceGateOutcome(value string) (governancev1.GateOutcome, error) {
	return requiredEnumValue(governanceEnumKey(value), governanceGateOutcomes, governancev1.GateOutcome_GATE_OUTCOME_UNSPECIFIED, "outcome")
}

func governanceTargetTypeName(value governancev1.GovernanceTargetType) string {
	return enumName(value, governanceTargetTypeNames)
}

func governanceEvidenceKindName(value governancev1.EvidenceKind) string {
	return enumName(value, governanceEvidenceKindNames)
}

func governanceRiskClassName(value governancev1.RiskClass) string {
	return enumName(value, governanceRiskClassNames)
}

func governanceRiskAssessmentStatusName(value governancev1.RiskAssessmentStatus) string {
	return enumName(value, governanceRiskAssessmentStatusNames)
}

func governanceRiskFactorSourceTypeName(value governancev1.RiskFactorSourceType) string {
	return enumName(value, governanceRiskFactorSourceTypeNames)
}

func governanceGateKindName(value governancev1.GateKind) string {
	return enumName(value, governanceGateKindNames)
}

func governanceGateStatusName(value governancev1.GateRequestStatus) string {
	return enumName(value, governanceGateStatusNames)
}

func governanceGateOutcomeName(value governancev1.GateOutcome) string {
	return enumName(value, governanceGateOutcomeNames)
}

func governanceEnumValues[Enum comparable](pairs []governanceEnumPair[Enum]) map[string]Enum {
	result := make(map[string]Enum, len(pairs))
	for _, pair := range pairs {
		result[pair.name] = pair.value
	}
	return result
}

func governanceEnumNames[Enum comparable](pairs []governanceEnumPair[Enum]) map[Enum]string {
	result := make(map[Enum]string, len(pairs))
	for _, pair := range pairs {
		result[pair.value] = pair.name
	}
	return result
}

func governanceEnumKey(value string) string {
	key := normalizedKey(value)
	prefixes := []string{
		"governance_target_type_",
		"evidence_kind_",
		"risk_class_",
		"risk_assessment_status_",
		"risk_factor_source_type_",
		"gate_kind_",
		"gate_request_status_",
		"gate_outcome_",
	}
	for _, prefix := range prefixes {
		key = strings.TrimPrefix(key, prefix)
	}
	return key
}
