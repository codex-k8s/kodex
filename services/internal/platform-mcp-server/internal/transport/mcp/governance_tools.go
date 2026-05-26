package mcptransport

import (
	"context"
	"strings"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	governanceGateRequestDescription        = "Request a governance gate through governance-manager without storing decision state in MCP."
	governanceGateGetDescription            = "Read a safe governance gate request summary through governance-manager."
	governanceGateListDescription           = "List safe governance gate request summaries through governance-manager."
	governanceGateSubmitDecisionDescription = "Submit a governance gate decision through governance-manager."
	governanceGateCancelDescription         = "Cancel an open governance gate request through governance-manager."
	governanceGateExpireDescription         = "Expire an open governance gate request through governance-manager."
)

var governanceToolDescriptions = map[string]string{
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

var governanceEvidenceKinds = map[string]governancev1.EvidenceKind{
	"provider_comment":     governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_COMMENT,
	"provider_review":      governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW,
	"provider_check":       governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_CHECK,
	"runtime_summary":      governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY,
	"document":             governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT,
	"risk_factor":          governancev1.EvidenceKind_EVIDENCE_KIND_RISK_FACTOR,
	"review_signal":        governancev1.EvidenceKind_EVIDENCE_KIND_REVIEW_SIGNAL,
	"interaction_callback": governancev1.EvidenceKind_EVIDENCE_KIND_INTERACTION_CALLBACK,
	"object_ref":           governancev1.EvidenceKind_EVIDENCE_KIND_OBJECT_REF,
	"custom":               governancev1.EvidenceKind_EVIDENCE_KIND_CUSTOM,
}

var governanceEvidenceKindNames = map[governancev1.EvidenceKind]string{
	governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_COMMENT:     "provider_comment",
	governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW:      "provider_review",
	governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_CHECK:       "provider_check",
	governancev1.EvidenceKind_EVIDENCE_KIND_RUNTIME_SUMMARY:      "runtime_summary",
	governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT:             "document",
	governancev1.EvidenceKind_EVIDENCE_KIND_RISK_FACTOR:          "risk_factor",
	governancev1.EvidenceKind_EVIDENCE_KIND_REVIEW_SIGNAL:        "review_signal",
	governancev1.EvidenceKind_EVIDENCE_KIND_INTERACTION_CALLBACK: "interaction_callback",
	governancev1.EvidenceKind_EVIDENCE_KIND_OBJECT_REF:           "object_ref",
	governancev1.EvidenceKind_EVIDENCE_KIND_CUSTOM:               "custom",
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

func governancePageRequest(input GovernancePageInput) *governancev1.PageRequest {
	return &governancev1.PageRequest{
		PageSize:  input.PageSize,
		PageToken: optionalString(input.PageToken),
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

func optionalGovernanceGateStatus(value string) (*governancev1.GateRequestStatus, error) {
	key := governanceEnumKey(value)
	if key == "" {
		return nil, nil
	}
	statusValue, ok := governanceGateStatuses[key]
	if !ok || statusValue == governancev1.GateRequestStatus_GATE_REQUEST_STATUS_UNSPECIFIED {
		return nil, invalidInput("status is invalid")
	}
	return &statusValue, nil
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

func governanceGateStatusName(value governancev1.GateRequestStatus) string {
	return enumName(value, governanceGateStatusNames)
}

func governanceGateOutcomeName(value governancev1.GateOutcome) string {
	return enumName(value, governanceGateOutcomeNames)
}

func governanceEnumKey(value string) string {
	key := normalizedKey(value)
	prefixes := []string{
		"governance_target_type_",
		"evidence_kind_",
		"gate_request_status_",
		"gate_outcome_",
	}
	for _, prefix := range prefixes {
		key = strings.TrimPrefix(key, prefix)
	}
	return key
}
