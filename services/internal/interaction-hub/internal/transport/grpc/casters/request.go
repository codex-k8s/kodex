package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func RequestFeedbackInput(input *interactionsv1.RequestFeedbackRequest) (interactionservice.RequestFeedbackInput, error) {
	if input == nil {
		return interactionservice.RequestFeedbackInput{}, errs.ErrInvalidArgument
	}
	meta, draft, err := interactionRequestCommandParts(input.GetMeta(), input.GetRequest())
	if err != nil {
		return interactionservice.RequestFeedbackInput{}, err
	}
	return interactionservice.RequestFeedbackInput{Meta: meta, Request: draft}, nil
}

func RequestApprovalInput(input *interactionsv1.RequestApprovalRequest) (interactionservice.RequestApprovalInput, error) {
	if input == nil {
		return interactionservice.RequestApprovalInput{}, errs.ErrInvalidArgument
	}
	meta, draft, err := interactionRequestCommandParts(input.GetMeta(), input.GetRequest())
	if err != nil {
		return interactionservice.RequestApprovalInput{}, err
	}
	return interactionservice.RequestApprovalInput{Meta: meta, Request: draft}, nil
}

func RequestHumanGateInput(input *interactionsv1.RequestHumanGateRequest) (interactionservice.RequestHumanGateInput, error) {
	if input == nil {
		return interactionservice.RequestHumanGateInput{}, errs.ErrInvalidArgument
	}
	meta, draft, err := interactionRequestCommandParts(input.GetMeta(), input.GetRequest())
	if err != nil {
		return interactionservice.RequestHumanGateInput{}, err
	}
	return interactionservice.RequestHumanGateInput{Meta: meta, Request: draft}, nil
}

func interactionRequestCommandParts(metaInput *interactionsv1.CommandMeta, requestInput *interactionsv1.InteractionRequestDraft) (value.CommandMeta, interactionservice.InteractionRequestDraftInput, error) {
	meta, err := CommandMeta(metaInput)
	if err != nil {
		return value.CommandMeta{}, interactionservice.InteractionRequestDraftInput{}, err
	}
	draft, err := InteractionRequestDraft(requestInput)
	if err != nil {
		return value.CommandMeta{}, interactionservice.InteractionRequestDraftInput{}, err
	}
	return meta, draft, nil
}

func RecordInteractionResponseInput(input *interactionsv1.RecordInteractionResponseRequest) (interactionservice.RecordInteractionResponseInput, error) {
	if input == nil {
		return interactionservice.RecordInteractionResponseInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.RecordInteractionResponseInput{}, err
	}
	requestID, err := ParseUUID(input.GetRequestId())
	if err != nil {
		return interactionservice.RecordInteractionResponseInput{}, err
	}
	return interactionservice.RecordInteractionResponseInput{
		Meta:                meta,
		RequestID:           requestID,
		ResponseAction:      ResponseAction(input.GetResponseAction()),
		RespondedByActorRef: input.GetRespondedByActorRef(),
		ResponseSummary:     input.GetResponseSummary(),
		ResponseObject:      ObjectRef(input.GetResponseObject()),
		SourceKind:          ResponseSourceKind(input.GetSourceKind()),
		SourceRef:           input.GetSourceRef(),
		OwnerDecisionRef:    input.GetOwnerDecisionRef(),
	}, nil
}

func CancelInteractionRequestInput(input *interactionsv1.CancelInteractionRequestRequest) (interactionservice.CancelInteractionRequestInput, error) {
	if input == nil {
		return interactionservice.CancelInteractionRequestInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.CancelInteractionRequestInput{}, err
	}
	requestID, err := ParseUUID(input.GetRequestId())
	if err != nil {
		return interactionservice.CancelInteractionRequestInput{}, err
	}
	return interactionservice.CancelInteractionRequestInput{Meta: meta, RequestID: requestID}, nil
}

func ExpireInteractionRequestsInput(input *interactionsv1.ExpireInteractionRequestsRequest) (interactionservice.ExpireInteractionRequestsInput, error) {
	if input == nil {
		return interactionservice.ExpireInteractionRequestsInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.ExpireInteractionRequestsInput{}, err
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.ExpireInteractionRequestsInput{}, err
	}
	deadlineBefore, err := OptionalTime(input.GetDeadlineBefore())
	if err != nil {
		return interactionservice.ExpireInteractionRequestsInput{}, err
	}
	return interactionservice.ExpireInteractionRequestsInput{Meta: meta, Scope: scope, DeadlineBefore: deadlineBefore, Limit: input.GetLimit()}, nil
}

func GetInteractionRequestInput(input *interactionsv1.GetInteractionRequestRequest) (interactionservice.GetInteractionRequestInput, error) {
	if input == nil {
		return interactionservice.GetInteractionRequestInput{}, errs.ErrInvalidArgument
	}
	requestID, err := ParseUUID(input.GetRequestId())
	if err != nil {
		return interactionservice.GetInteractionRequestInput{}, err
	}
	return interactionservice.GetInteractionRequestInput{Meta: QueryMeta(input.GetMeta()), RequestID: requestID}, nil
}

func ListInteractionRequestsInput(input *interactionsv1.ListInteractionRequestsRequest) (interactionservice.ListInteractionRequestsInput, error) {
	if input == nil {
		return interactionservice.ListInteractionRequestsInput{}, errs.ErrInvalidArgument
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.ListInteractionRequestsInput{}, err
	}
	deadlineBefore, err := OptionalTime(input.GetDeadlineBefore())
	if err != nil {
		return interactionservice.ListInteractionRequestsInput{}, err
	}
	result := interactionservice.ListInteractionRequestsInput{
		Meta:           QueryMeta(input.GetMeta()),
		Scope:          scope,
		SourceOwnerRef: strings.TrimSpace(input.GetSourceOwnerRef()),
		DeadlineBefore: deadlineBefore,
		Page:           PageRequest(input.GetPage()),
	}
	if input.RequestKind != nil {
		result.RequestKind = RequestKind(input.GetRequestKind())
	}
	if input.Status != nil {
		result.Status = RequestStatus(input.GetStatus())
	}
	if input.SourceOwnerKind != nil {
		result.SourceOwnerKind = SourceOwnerKind(input.GetSourceOwnerKind())
	}
	return result, nil
}

func InteractionRequestDraft(input *interactionsv1.InteractionRequestDraft) (interactionservice.InteractionRequestDraftInput, error) {
	if input == nil {
		return interactionservice.InteractionRequestDraftInput{}, errs.ErrInvalidArgument
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.InteractionRequestDraftInput{}, err
	}
	threadID, err := OptionalUUID(input.GetThreadId())
	if err != nil {
		return interactionservice.InteractionRequestDraftInput{}, err
	}
	deadline, err := OptionalTime(input.GetDeadlineAt())
	if err != nil {
		return interactionservice.InteractionRequestDraftInput{}, err
	}
	return interactionservice.InteractionRequestDraftInput{
		Scope:             scope,
		ThreadID:          threadID,
		SourceOwner:       SourceOwnerRef(input.GetSourceOwner()),
		Ingress:           IngressRef(input.GetIngress()),
		DecisionOwner:     DecisionOwnerRef(input.GetDecisionOwner()),
		TargetRefs:        ActorRefs(input.GetTargetRefs()),
		ContextRefs:       ExternalRefs(input.GetContextRefs()),
		PromptSummary:     input.GetPromptSummary(),
		PromptObject:      ObjectRef(input.GetPromptObject()),
		AllowedActions:    InteractionActions(input.GetAllowedActions()),
		RiskClass:         RiskClass(input.GetRiskClass()),
		DeadlineAt:        deadline,
		ReminderPolicyRef: strings.TrimSpace(input.GetReminderPolicyRef()),
	}, nil
}

func InteractionRequestResponse(request entity.InteractionRequest) *interactionsv1.InteractionRequestResponse {
	return &interactionsv1.InteractionRequestResponse{Request: InteractionRequest(request)}
}

func InteractionResponseResponse(request entity.InteractionRequest, response entity.InteractionResponse) *interactionsv1.InteractionResponseResponse {
	return &interactionsv1.InteractionResponseResponse{Request: InteractionRequest(request), Response: InteractionResponse(response)}
}

func ExpireInteractionRequestsResponse(result interactionservice.ExpireInteractionRequestsResult) *interactionsv1.ExpireInteractionRequestsResponse {
	ids := make([]string, 0, len(result.ExpiredRequestIDs))
	for _, id := range result.ExpiredRequestIDs {
		ids = append(ids, id.String())
	}
	return &interactionsv1.ExpireInteractionRequestsResponse{ExpiredRequestIds: ids, ExpiredCount: int32(len(ids))}
}

func ListInteractionRequestsResponse(requests []entity.InteractionRequest, page value.PageResult) *interactionsv1.ListInteractionRequestsResponse {
	items := make([]*interactionsv1.InteractionRequest, 0, len(requests))
	for _, request := range requests {
		items = append(items, InteractionRequest(request))
	}
	return &interactionsv1.ListInteractionRequestsResponse{Requests: items, Page: PageResponse(page)}
}

func InteractionRequest(request entity.InteractionRequest) *interactionsv1.InteractionRequest {
	return &interactionsv1.InteractionRequest{
		Id:                request.ID.String(),
		RequestKind:       RequestKindProto(request.RequestKind),
		Scope:             &interactionsv1.ScopeRef{Type: ScopeTypeProto(request.Scope.Type), Ref: request.Scope.Ref},
		ThreadId:          OptionalUUIDProto(request.ThreadID),
		SourceOwner:       SourceOwnerRefProto(request.SourceOwner),
		Ingress:           IngressRefProto(request.Ingress),
		DecisionOwner:     DecisionOwnerRefProto(request.DecisionOwner),
		TargetRefs:        ActorRefsProto(request.TargetRefs),
		ContextRefs:       ExternalRefsProto(request.ContextRefs),
		PromptSummary:     request.PromptSummary,
		PromptObject:      ObjectRefProto(request.PromptObject),
		AllowedActions:    InteractionActionsProto(request.AllowedActions),
		RiskClass:         RiskClassProto(request.RiskClass),
		Status:            RequestStatusProto(request.Status),
		DeadlineAt:        OptionalTimeProto(request.DeadlineAt),
		ReminderPolicyRef: OptionalString(request.ReminderPolicyRef),
		Version:           request.Version,
		CreatedAt:         TimeProto(request.CreatedAt),
		UpdatedAt:         TimeProto(request.UpdatedAt),
		ResolvedAt:        OptionalTimeProto(request.ResolvedAt),
	}
}

func InteractionResponse(response entity.InteractionResponse) *interactionsv1.InteractionResponse {
	return &interactionsv1.InteractionResponse{
		Id:                  response.ID.String(),
		RequestId:           response.RequestID.String(),
		ResponseAction:      ResponseActionProto(response.ResponseAction),
		RespondedByActorRef: response.RespondedByActorRef,
		ResponseSummary:     OptionalString(response.ResponseSummary),
		ResponseObject:      ObjectRefProto(response.ResponseObject),
		SourceKind:          ResponseSourceKindProto(response.SourceKind),
		SourceRef:           OptionalString(response.SourceRef),
		OwnerDecisionRef:    OptionalString(response.OwnerDecisionRef),
		CreatedAt:           TimeProto(response.CreatedAt),
	}
}

func SourceOwnerRef(input *interactionsv1.SourceOwnerRef) value.SourceOwnerRef {
	if input == nil {
		return value.SourceOwnerRef{}
	}
	return value.SourceOwnerRef{Kind: SourceOwnerKind(input.GetKind()), Ref: strings.TrimSpace(input.GetRef())}
}

func SourceOwnerRefProto(input value.SourceOwnerRef) *interactionsv1.SourceOwnerRef {
	if input.Kind == "" && input.Ref == "" {
		return nil
	}
	return &interactionsv1.SourceOwnerRef{Kind: SourceOwnerKindProto(input.Kind), Ref: OptionalString(input.Ref)}
}

func IngressRef(input *interactionsv1.IngressRef) value.IngressRef {
	if input == nil {
		return value.IngressRef{}
	}
	return value.IngressRef{Kind: IngressKind(input.GetKind()), Ref: strings.TrimSpace(input.GetRef())}
}

func IngressRefProto(input value.IngressRef) *interactionsv1.IngressRef {
	if input.Kind == "" && input.Ref == "" {
		return nil
	}
	return &interactionsv1.IngressRef{Kind: IngressKindProto(input.Kind), Ref: OptionalString(input.Ref)}
}

func DecisionOwnerRef(input *interactionsv1.DecisionOwnerRef) value.DecisionOwnerRef {
	if input == nil {
		return value.DecisionOwnerRef{}
	}
	return value.DecisionOwnerRef{
		Kind:             DecisionOwnerKind(input.GetOwnerKind()),
		OwnerRequestRef:  strings.TrimSpace(input.GetOwnerRequestRef()),
		OwnerDecisionRef: strings.TrimSpace(input.GetOwnerDecisionRef()),
	}
}

func DecisionOwnerRefProto(input value.DecisionOwnerRef) *interactionsv1.DecisionOwnerRef {
	if input.Kind == "" && input.OwnerRequestRef == "" && input.OwnerDecisionRef == "" {
		return nil
	}
	return &interactionsv1.DecisionOwnerRef{
		OwnerKind:        DecisionOwnerKindProto(input.Kind),
		OwnerRequestRef:  input.OwnerRequestRef,
		OwnerDecisionRef: OptionalString(input.OwnerDecisionRef),
	}
}

func ActorRefs(input []*interactionsv1.ActorRef) []value.ActorRef {
	result := make([]value.ActorRef, 0, len(input))
	for _, ref := range input {
		if ref == nil {
			continue
		}
		result = append(result, value.ActorRef{Kind: strings.TrimSpace(ref.GetRefKind()), Ref: strings.TrimSpace(ref.GetRef())})
	}
	return result
}

func ActorRefsProto(input []value.ActorRef) []*interactionsv1.ActorRef {
	result := make([]*interactionsv1.ActorRef, 0, len(input))
	for _, ref := range input {
		result = append(result, &interactionsv1.ActorRef{RefKind: ref.Kind, Ref: ref.Ref})
	}
	return result
}

func ExternalRefs(input []*interactionsv1.ExternalRef) []value.ExternalRef {
	result := make([]value.ExternalRef, 0, len(input))
	for _, ref := range input {
		if ref == nil {
			continue
		}
		result = append(result, value.ExternalRef{Kind: strings.TrimSpace(ref.GetRefKind()), Ref: strings.TrimSpace(ref.GetRef())})
	}
	return result
}

func ExternalRefsProto(input []value.ExternalRef) []*interactionsv1.ExternalRef {
	result := make([]*interactionsv1.ExternalRef, 0, len(input))
	for _, ref := range input {
		result = append(result, &interactionsv1.ExternalRef{RefKind: ref.Kind, Ref: ref.Ref})
	}
	return result
}

func InteractionActions(input []*interactionsv1.InteractionAction) []value.InteractionAction {
	result := make([]value.InteractionAction, 0, len(input))
	for _, action := range input {
		if action == nil {
			continue
		}
		result = append(result, value.InteractionAction{
			ActionKey:        strings.TrimSpace(action.GetActionKey()),
			LabelTemplateRef: strings.TrimSpace(action.GetLabelTemplateRef()),
			Terminal:         action.GetIsTerminal(),
		})
	}
	return result
}

func InteractionActionsProto(input []value.InteractionAction) []*interactionsv1.InteractionAction {
	result := make([]*interactionsv1.InteractionAction, 0, len(input))
	for _, action := range input {
		result = append(result, &interactionsv1.InteractionAction{
			ActionKey:        action.ActionKey,
			LabelTemplateRef: OptionalString(action.LabelTemplateRef),
			IsTerminal:       action.Terminal,
		})
	}
	return result
}

func OptionalUUID(input string) (uuid.UUID, error) {
	if strings.TrimSpace(input) == "" {
		return uuid.Nil, nil
	}
	return ParseUUID(input)
}

func OptionalTime(input string) (*time.Time, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input))
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	value := parsed.UTC()
	return &value, nil
}

func SourceOwnerKind(input interactionsv1.SourceOwnerKind) enum.SourceOwnerKind {
	return domainEnumValue[enum.SourceOwnerKind](input, "SOURCE_OWNER_KIND_")
}

func SourceOwnerKindProto(input enum.SourceOwnerKind) interactionsv1.SourceOwnerKind {
	return protoEnumValue(input, interactionsv1.SourceOwnerKind_value, "SOURCE_OWNER_KIND_", interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_UNSPECIFIED)
}

func DecisionOwnerKind(input interactionsv1.DecisionOwnerKind) enum.DecisionOwnerKind {
	return domainEnumValue[enum.DecisionOwnerKind](input, "DECISION_OWNER_KIND_")
}

func DecisionOwnerKindProto(input enum.DecisionOwnerKind) interactionsv1.DecisionOwnerKind {
	return protoEnumValue(input, interactionsv1.DecisionOwnerKind_value, "DECISION_OWNER_KIND_", interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_UNSPECIFIED)
}

func IngressKind(input interactionsv1.IngressKind) enum.IngressKind {
	return domainEnumValue[enum.IngressKind](input, "INGRESS_KIND_")
}

func IngressKindProto(input enum.IngressKind) interactionsv1.IngressKind {
	return protoEnumValue(input, interactionsv1.IngressKind_value, "INGRESS_KIND_", interactionsv1.IngressKind_INGRESS_KIND_UNSPECIFIED)
}

func RequestKind(input interactionsv1.InteractionRequestKind) enum.InteractionRequestKind {
	return domainEnumValue[enum.InteractionRequestKind](input, "INTERACTION_REQUEST_KIND_")
}

func RequestKindProto(input enum.InteractionRequestKind) interactionsv1.InteractionRequestKind {
	return protoEnumValue(input, interactionsv1.InteractionRequestKind_value, "INTERACTION_REQUEST_KIND_", interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_UNSPECIFIED)
}

func RiskClass(input interactionsv1.InteractionRiskClass) enum.InteractionRiskClass {
	return domainEnumValue[enum.InteractionRiskClass](input, "INTERACTION_RISK_CLASS_")
}

func RiskClassProto(input enum.InteractionRiskClass) interactionsv1.InteractionRiskClass {
	return protoEnumValue(input, interactionsv1.InteractionRiskClass_value, "INTERACTION_RISK_CLASS_", interactionsv1.InteractionRiskClass_INTERACTION_RISK_CLASS_UNSPECIFIED)
}

func RequestStatus(input interactionsv1.InteractionRequestStatus) enum.InteractionRequestStatus {
	return domainEnumValue[enum.InteractionRequestStatus](input, "INTERACTION_REQUEST_STATUS_")
}

func RequestStatusProto(input enum.InteractionRequestStatus) interactionsv1.InteractionRequestStatus {
	return protoEnumValue(input, interactionsv1.InteractionRequestStatus_value, "INTERACTION_REQUEST_STATUS_", interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_UNSPECIFIED)
}

func ResponseAction(input interactionsv1.InteractionResponseAction) enum.InteractionResponseAction {
	return domainEnumValue[enum.InteractionResponseAction](input, "INTERACTION_RESPONSE_ACTION_")
}

func ResponseActionProto(input enum.InteractionResponseAction) interactionsv1.InteractionResponseAction {
	return protoEnumValue(input, interactionsv1.InteractionResponseAction_value, "INTERACTION_RESPONSE_ACTION_", interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_UNSPECIFIED)
}

func ResponseSourceKind(input interactionsv1.InteractionResponseSourceKind) enum.InteractionResponseSourceKind {
	return domainEnumValue[enum.InteractionResponseSourceKind](input, "INTERACTION_RESPONSE_SOURCE_KIND_")
}

func ResponseSourceKindProto(input enum.InteractionResponseSourceKind) interactionsv1.InteractionResponseSourceKind {
	return protoEnumValue(input, interactionsv1.InteractionResponseSourceKind_value, "INTERACTION_RESPONSE_SOURCE_KIND_", interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_UNSPECIFIED)
}

func domainEnumValue[Domain ~string](input interface{ String() string }, prefix string) Domain {
	name := strings.TrimPrefix(input.String(), prefix)
	if name == input.String() || name == "UNSPECIFIED" {
		return ""
	}
	return Domain(strings.ToLower(name))
}

func protoEnumValue[Domain ~string, Proto ~int32](input Domain, values map[string]int32, prefix string, fallback Proto) Proto {
	if input == "" {
		return fallback
	}
	key := prefix + strings.ToUpper(string(input))
	if value, ok := values[key]; ok {
		return Proto(value)
	}
	return fallback
}
