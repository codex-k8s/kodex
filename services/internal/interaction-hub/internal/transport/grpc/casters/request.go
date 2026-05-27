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
	return commandPayloadInput(input, (*interactionsv1.RequestFeedbackRequest).GetMeta, (*interactionsv1.RequestFeedbackRequest).GetRequest, InteractionRequestDraft, feedbackInputFromDraft)
}

func RequestApprovalInput(input *interactionsv1.RequestApprovalRequest) (interactionservice.RequestApprovalInput, error) {
	return commandPayloadInput(input, (*interactionsv1.RequestApprovalRequest).GetMeta, (*interactionsv1.RequestApprovalRequest).GetRequest, InteractionRequestDraft, approvalInputFromDraft)
}

func RequestHumanGateInput(input *interactionsv1.RequestHumanGateRequest) (interactionservice.RequestHumanGateInput, error) {
	return commandPayloadInput(input, (*interactionsv1.RequestHumanGateRequest).GetMeta, (*interactionsv1.RequestHumanGateRequest).GetRequest, InteractionRequestDraft, humanGateInputFromDraft)
}

func feedbackInputFromDraft(meta value.CommandMeta, draft interactionservice.InteractionRequestDraftInput) interactionservice.RequestFeedbackInput {
	return interactionservice.RequestFeedbackInput{Meta: meta, Request: draft}
}

func approvalInputFromDraft(meta value.CommandMeta, draft interactionservice.InteractionRequestDraftInput) interactionservice.RequestApprovalInput {
	return interactionservice.RequestApprovalInput{Meta: meta, Request: draft}
}

func humanGateInputFromDraft(meta value.CommandMeta, draft interactionservice.InteractionRequestDraftInput) interactionservice.RequestHumanGateInput {
	return interactionservice.RequestHumanGateInput{Meta: meta, Request: draft}
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
	return commandIDInput(input, (*interactionsv1.CancelInteractionRequestRequest).GetMeta, (*interactionsv1.CancelInteractionRequestRequest).GetRequestId, cancelRequestInput)
}

func cancelRequestInput(meta value.CommandMeta, requestID uuid.UUID) interactionservice.CancelInteractionRequestInput {
	return interactionservice.CancelInteractionRequestInput{Meta: meta, RequestID: requestID}
}

func commandIDInput[
	Request any,
	Output any,
](
	input *Request,
	metaInput func(*Request) *interactionsv1.CommandMeta,
	idInput func(*Request) string,
	build func(value.CommandMeta, uuid.UUID) Output,
) (Output, error) {
	decodeID := func(request *Request) (uuid.UUID, error) {
		return ParseUUID(idInput(request))
	}
	return decodeCommandEnvelope(input, metaInput, decodeID, build)
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
	return interactionRequestReadInput(QueryMeta(input.GetMeta()), requestID), nil
}

func interactionRequestReadInput(meta value.QueryMeta, requestID uuid.UUID) interactionservice.GetInteractionRequestInput {
	return interactionservice.GetInteractionRequestInput{Meta: meta, RequestID: requestID}
}

type queryIDAdapter[Request comparable, Output any] struct {
	metaInput func(Request) *interactionsv1.QueryMeta
	idInput   func(Request) string
	build     func(value.QueryMeta, uuid.UUID) Output
}

func queryIDInput[Request comparable, Output any](input Request, adapter queryIDAdapter[Request, Output]) (Output, error) {
	var zero Output
	var empty Request
	if input == empty {
		return zero, errs.ErrInvalidArgument
	}
	id, err := ParseUUID(adapter.idInput(input))
	if err != nil {
		return zero, err
	}
	return adapter.build(QueryMeta(adapter.metaInput(input)), id), nil
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

func ListOwnerInboxItemsInput(input *interactionsv1.ListOwnerInboxItemsRequest) (interactionservice.ListOwnerInboxItemsInput, error) {
	if input == nil {
		return interactionservice.ListOwnerInboxItemsInput{}, errs.ErrInvalidArgument
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.ListOwnerInboxItemsInput{}, err
	}
	result := interactionservice.ListOwnerInboxItemsInput{
		Meta:               QueryMeta(input.GetMeta()),
		Scope:              scope,
		RequestKinds:       RequestKinds(input.GetRequestKinds()),
		Statuses:           RequestStatuses(input.GetStatuses()),
		SourceOwnerRef:     strings.TrimSpace(input.GetSourceOwnerRef()),
		AssigneeRef:        ActorRef(input.GetAssigneeRef()),
		ActorRef:           strings.TrimSpace(input.GetActorRef()),
		CorrelationRef:     ExternalRef(input.GetCorrelationRef()),
		CorrelationID:      strings.TrimSpace(input.GetCorrelationId()),
		IncludeDiagnostics: input.GetIncludeDiagnostics(),
		Page:               PageRequest(input.GetPage()),
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
	return &interactionsv1.ListInteractionRequestsResponse{Requests: castSlice(requests, InteractionRequest), Page: PageResponse(page)}
}

func ListOwnerInboxItemsResponse(items []entity.OwnerInboxItem, page value.PageResult) *interactionsv1.ListOwnerInboxItemsResponse {
	return &interactionsv1.ListOwnerInboxItemsResponse{Items: castSlice(items, OwnerInboxItem), Page: PageResponse(page)}
}

func OwnerInboxItem(item entity.OwnerInboxItem) *interactionsv1.OwnerInboxItem {
	response := &interactionsv1.OwnerInboxItem{
		RequestId:         item.Request.ID.String(),
		RequestKind:       RequestKindProto(item.Request.RequestKind),
		RequestStatus:     RequestStatusProto(item.Request.Status),
		Scope:             &interactionsv1.ScopeRef{Type: ScopeTypeProto(item.Request.Scope.Type), Ref: item.Request.Scope.Ref},
		Requester:         SourceOwnerRefProto(item.Request.SourceOwner),
		DecisionOwner:     DecisionOwnerRefProto(item.Request.DecisionOwner),
		AssigneeRefs:      ActorRefsProto(item.Request.TargetRefs),
		ContextRefs:       ExternalRefsProto(item.Request.ContextRefs),
		Title:             item.Title,
		Summary:           item.Summary,
		DeadlineAt:        OptionalTimeProto(item.Request.DeadlineAt),
		ReminderPolicyRef: OptionalString(item.Request.ReminderPolicyRef),
		DeliverySummary:   OwnerInboxDeliverySummary(item.DeliverySummary),
		CreatedAt:         TimeProto(item.Request.CreatedAt),
		UpdatedAt:         TimeProto(item.Request.UpdatedAt),
		ResolvedAt:        OptionalTimeProto(item.Request.ResolvedAt),
		Version:           item.Request.Version,
	}
	if item.LatestCallback != nil {
		response.LatestCallback = OwnerInboxCallbackSummary(*item.LatestCallback)
	}
	if item.LatestResponse != nil {
		response.LatestResponse = OwnerInboxResponseSummary(*item.LatestResponse)
	}
	return response
}

func OwnerInboxDeliverySummary(summary entity.OwnerInboxDeliverySummary) *interactionsv1.OwnerInboxDeliverySummary {
	return &interactionsv1.OwnerInboxDeliverySummary{
		AttemptCount:            summary.AttemptCount,
		LatestDeliveryAttemptId: OptionalUUIDProto(summary.LatestAttemptID),
		LatestDeliveryId:        OptionalString(summary.LatestDeliveryID),
		LatestStatus:            DeliveryAttemptStatusProto(summary.LatestStatus),
		LatestErrorCode:         OptionalString(summary.LatestErrorCode),
		LatestErrorClass:        DeliveryErrorClassProto(summary.LatestErrorClass),
		NextRetryAt:             OptionalTimeProto(summary.NextRetryAt),
		LatestUpdatedAt:         OptionalTimeProto(summary.LatestUpdatedAt),
		RouteId:                 OptionalUUIDProto(summary.RouteID),
		ChannelMessageRef:       OptionalString(summary.ChannelMessageRef),
	}
}

func OwnerInboxCallbackSummary(callback entity.ChannelCallback) *interactionsv1.OwnerInboxCallbackSummary {
	return &interactionsv1.OwnerInboxCallbackSummary{
		CallbackRef:      callback.ID.String(),
		CallbackId:       callback.CallbackID,
		DeliveryId:       OptionalString(callback.DeliveryID),
		SignatureStatus:  CallbackSignatureStatusProto(callback.SignatureStatus),
		ProcessingStatus: CallbackProcessingStatusProto(callback.ProcessingStatus),
		ActorRef:         OptionalString(callback.ActorRef),
		Action:           OptionalString(callback.Action),
		ErrorCode:        OptionalString(callback.ErrorCode),
		ReceivedAt:       TimeProto(callback.ReceivedAt),
		GatewayRef:       OptionalString(callback.GatewayRef),
		CorrelationId:    OptionalString(callback.CorrelationID),
	}
}

func OwnerInboxResponseSummary(response entity.InteractionResponse) *interactionsv1.OwnerInboxResponseSummary {
	return &interactionsv1.OwnerInboxResponseSummary{
		ResponseId:             response.ID.String(),
		ResponseAction:         ResponseActionProto(response.ResponseAction),
		RespondedByActorRef:    response.RespondedByActorRef,
		SourceKind:             ResponseSourceKindProto(response.SourceKind),
		SourceRef:              OptionalString(response.SourceRef),
		OwnerDecisionRef:       OptionalString(response.OwnerDecisionRef),
		CreatedAt:              TimeProto(response.CreatedAt),
		ResponseSummary:        OptionalString(response.ResponseSummary),
		ResponseSummaryDigest:  OptionalString(response.ResponseSummaryDigest),
		ResponseObject:         ObjectRefProto(response.ResponseObject),
		InteractionResponseRef: OptionalString(response.ID.String()),
	}
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
	if refPairEmpty(string(input.Kind), input.Ref) {
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
	if refPairEmpty(string(input.Kind), input.Ref) {
		return nil
	}
	ref := &interactionsv1.IngressRef{}
	ref.Kind = IngressKindProto(input.Kind)
	ref.Ref = OptionalString(input.Ref)
	return ref
}

func refPairEmpty(kind string, ref string) bool {
	return kind == "" && ref == ""
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
	return collectRefs(input, (*interactionsv1.ActorRef)(nil), (*interactionsv1.ActorRef).GetRefKind, (*interactionsv1.ActorRef).GetRef, actorRefValue)
}

func ActorRefsProto(input []value.ActorRef) []*interactionsv1.ActorRef {
	return castSlice(input, actorRefItemProto)
}

func RequestKinds(input []interactionsv1.InteractionRequestKind) []enum.InteractionRequestKind {
	return castSlice(input, RequestKind)
}

func RequestStatuses(input []interactionsv1.InteractionRequestStatus) []enum.InteractionRequestStatus {
	return castSlice(input, RequestStatus)
}

func castSlice[Source any, Target any](input []Source, cast func(Source) Target) []Target {
	result := make([]Target, 0, len(input))
	for _, item := range input {
		result = append(result, cast(item))
	}
	return result
}

func collectRefs[Source comparable, Target any](input []Source, zero Source, kind func(Source) string, ref func(Source) string, build func(string, string) Target) []Target {
	result := make([]Target, 0, len(input))
	for _, item := range input {
		if item == zero {
			continue
		}
		result = append(result, build(strings.TrimSpace(kind(item)), strings.TrimSpace(ref(item))))
	}
	return result
}

func ExternalRef(input *interactionsv1.ExternalRef) value.ExternalRef {
	if input == nil {
		return value.ExternalRef{}
	}
	kind := strings.TrimSpace(input.GetRefKind())
	ref := strings.TrimSpace(input.GetRef())
	return externalRefValue(kind, ref)
}

func ExternalRefs(input []*interactionsv1.ExternalRef) []value.ExternalRef {
	return collectRefs(input, (*interactionsv1.ExternalRef)(nil), (*interactionsv1.ExternalRef).GetRefKind, (*interactionsv1.ExternalRef).GetRef, externalRefValue)
}

func ExternalRefsProto(input []value.ExternalRef) []*interactionsv1.ExternalRef {
	return castSlice(input, externalRefItemProto)
}

func actorRefValue(kind string, ref string) value.ActorRef {
	return value.ActorRef{Kind: kind, Ref: ref}
}

func actorRefItemProto(ref value.ActorRef) *interactionsv1.ActorRef {
	return &interactionsv1.ActorRef{RefKind: ref.Kind, Ref: ref.Ref}
}

func externalRefValue(kind string, ref string) value.ExternalRef {
	return value.ExternalRef{Kind: kind, Ref: ref}
}

func externalRefItemProto(ref value.ExternalRef) *interactionsv1.ExternalRef {
	output := &interactionsv1.ExternalRef{}
	output.RefKind = ref.Kind
	output.Ref = ref.Ref
	return output
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
