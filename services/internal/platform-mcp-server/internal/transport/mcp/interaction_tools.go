package mcptransport

import (
	"context"
	"strings"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var interactionToolDescriptions = map[string]string{
	ToolInteractionOwnerInboxList:    "Получить список входящих задач владельца через interaction-hub.",
	ToolInteractionOwnerInboxGet:     "Прочитать входящую задачу владельца через interaction-hub.",
	ToolInteractionOwnerInboxRespond: "Записать ответ владельца через interaction-hub без переноса решения в MCP.",
}

var interactionScopeTypes = map[string]interactionsv1.InteractionScopeType{
	"platform":     interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM,
	"organization": interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION,
	"project":      interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT,
	"repository":   interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY,
	"service":      interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE,
}

var interactionScopeTypeNames = map[interactionsv1.InteractionScopeType]string{
	interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM:     "platform",
	interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION: "organization",
	interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT:      "project",
	interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY:   "repository",
	interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE:      "service",
}

var interactionRequestKinds = map[string]interactionsv1.InteractionRequestKind{
	"feedback":   interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_FEEDBACK,
	"approval":   interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_APPROVAL,
	"human_gate": interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE,
}

var interactionRequestKindNames = map[interactionsv1.InteractionRequestKind]string{
	interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_FEEDBACK:   "feedback",
	interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_APPROVAL:   "approval",
	interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE: "human_gate",
}

var interactionRequestStatuses = map[string]interactionsv1.InteractionRequestStatus{
	"created":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CREATED,
	"routed":    interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ROUTED,
	"waiting":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING,
	"answered":  interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED,
	"expired":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_EXPIRED,
	"cancelled": interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CANCELLED,
	"canceled":  interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CANCELLED,
	"failed":    interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_FAILED,
}

var interactionRequestStatusNames = map[interactionsv1.InteractionRequestStatus]string{
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CREATED:   "created",
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ROUTED:    "routed",
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING:   "waiting",
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED:  "answered",
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_EXPIRED:   "expired",
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CANCELLED: "cancelled",
	interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_FAILED:    "failed",
}

var interactionSourceOwnerKinds = map[string]interactionsv1.SourceOwnerKind{
	"agent_manager":      interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER,
	"slot_agent":         interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SLOT_AGENT,
	"governance_manager": interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_GOVERNANCE_MANAGER,
	"provider_hub":       interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_PROVIDER_HUB,
	"operations_hub":     interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_OPERATIONS_HUB,
	"user":               interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_USER,
	"system":             interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SYSTEM,
}

var interactionSourceOwnerKindNames = map[interactionsv1.SourceOwnerKind]string{
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER:      "agent_manager",
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SLOT_AGENT:         "slot_agent",
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_GOVERNANCE_MANAGER: "governance_manager",
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_PROVIDER_HUB:       "provider_hub",
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_OPERATIONS_HUB:     "operations_hub",
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_USER:               "user",
	interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SYSTEM:             "system",
}

var interactionDecisionOwnerKindNames = map[interactionsv1.DecisionOwnerKind]string{
	interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_AGENT_MANAGER:      "agent_manager",
	interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_GOVERNANCE_MANAGER: "governance_manager",
	interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_PROVIDER_HUB:       "provider_hub",
	interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_OPERATIONS_HUB:     "operations_hub",
	interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_SYSTEM:             "system",
}

var interactionResponseActions = map[string]interactionsv1.InteractionResponseAction{
	"answer":      interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ANSWER,
	"approve":     interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_APPROVE,
	"reject":      interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REJECT,
	"defer":       interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_DEFER,
	"acknowledge": interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ACKNOWLEDGE,
	"custom":      interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_CUSTOM,
}

var interactionResponseActionNames = map[interactionsv1.InteractionResponseAction]string{
	interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ANSWER:      "answer",
	interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_APPROVE:     "approve",
	interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REJECT:      "reject",
	interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_DEFER:       "defer",
	interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ACKNOWLEDGE: "acknowledge",
	interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_CUSTOM:      "custom",
}

var interactionResponseSourceKinds = map[string]interactionsv1.InteractionResponseSourceKind{
	"web_console":      interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_WEB_CONSOLE,
	"mcp":              interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_MCP,
	"channel_callback": interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_CHANNEL_CALLBACK,
	"system":           interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_SYSTEM,
	"service":          interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_SERVICE,
}

var interactionResponseSourceKindNames = map[interactionsv1.InteractionResponseSourceKind]string{
	interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_WEB_CONSOLE:      "web_console",
	interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_MCP:              "mcp",
	interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_CHANNEL_CALLBACK: "channel_callback",
	interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_SYSTEM:           "system",
	interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_SERVICE:          "service",
}

var interactionDeliveryAttemptStatusNames = map[interactionsv1.DeliveryAttemptStatus]string{
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_QUEUED:    "queued",
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_SENT:      "sent",
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_ACCEPTED:  "accepted",
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DELIVERED: "delivered",
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_FAILED:    "failed",
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_CANCELLED: "cancelled",
	interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_EXPIRED:   "expired",
}

var interactionDeliveryErrorClassNames = map[interactionsv1.DeliveryErrorClass]string{
	interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_TEMPORARY:    "temporary",
	interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_PERMANENT:    "permanent",
	interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_AUTH:         "auth",
	interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_RATE_LIMITED: "rate_limited",
	interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_POLICY:       "policy",
}

var interactionCallbackSignatureStatusNames = map[interactionsv1.CallbackSignatureStatus]string{
	interactionsv1.CallbackSignatureStatus_CALLBACK_SIGNATURE_STATUS_VERIFIED:               "verified",
	interactionsv1.CallbackSignatureStatus_CALLBACK_SIGNATURE_STATUS_TRUSTED_INTERNAL:       "trusted_internal",
	interactionsv1.CallbackSignatureStatus_CALLBACK_SIGNATURE_STATUS_REJECTED_BEFORE_DOMAIN: "rejected_before_domain",
}

var interactionCallbackProcessingStatusNames = map[interactionsv1.CallbackProcessingStatus]string{
	interactionsv1.CallbackProcessingStatus_CALLBACK_PROCESSING_STATUS_ACCEPTED:  "accepted",
	interactionsv1.CallbackProcessingStatus_CALLBACK_PROCESSING_STATUS_DUPLICATE: "duplicate",
	interactionsv1.CallbackProcessingStatus_CALLBACK_PROCESSING_STATUS_REJECTED:  "rejected",
	interactionsv1.CallbackProcessingStatus_CALLBACK_PROCESSING_STATUS_FAILED:    "failed",
}

// InteractionToolsHandler routes interaction MCP tools to interaction-hub.
type InteractionToolsHandler struct {
	client InteractionHubClient
}

// NewInteractionToolsHandler creates the interaction tool boundary.
func NewInteractionToolsHandler(client InteractionHubClient) *InteractionToolsHandler {
	return &InteractionToolsHandler{client: client}
}

func (handler *InteractionToolsHandler) ListOwnerInboxItems(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListOwnerInboxInput) (*mcpsdk.CallToolResult, OwnerInboxListOutput, error) {
	return routeOwnerTool(ctx, input, listOwnerInboxRequest, handler.client.ListOwnerInboxItems, ownerInboxListOutput, ToolInteractionOwnerInboxList)
}

func (handler *InteractionToolsHandler) GetOwnerInboxItem(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetOwnerInboxInput) (*mcpsdk.CallToolResult, OwnerInboxOutput, error) {
	return routeOwnerTool(ctx, input, getOwnerInboxRequest, handler.client.GetOwnerInboxItem, ownerInboxOutput, ToolInteractionOwnerInboxGet)
}

func (handler *InteractionToolsHandler) RespondOwnerInboxItem(ctx context.Context, _ *mcpsdk.CallToolRequest, input RespondOwnerInboxInput) (*mcpsdk.CallToolResult, OwnerInboxResponseOutput, error) {
	return routeOwnerTool(ctx, input, respondOwnerInboxRequest, handler.client.RecordInteractionResponse, ownerInboxResponseOutput, ToolInteractionOwnerInboxRespond)
}

func listOwnerInboxRequest(input ListOwnerInboxInput) (*interactionsv1.ListOwnerInboxItemsRequest, error) {
	meta, err := interactionQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	scope, err := interactionScope(input.Scope)
	if err != nil {
		return nil, err
	}
	requestKinds, err := interactionRequestKindList(input.RequestKinds)
	if err != nil {
		return nil, err
	}
	statuses, err := interactionRequestStatusList(input.Statuses)
	if err != nil {
		return nil, err
	}
	sourceOwnerKind, err := optionalInteractionSourceOwnerKind(input.SourceOwnerKind)
	if err != nil {
		return nil, err
	}
	return &interactionsv1.ListOwnerInboxItemsRequest{
		Meta:               meta,
		Scope:              scope,
		RequestKinds:       requestKinds,
		Statuses:           statuses,
		SourceOwnerKind:    sourceOwnerKind,
		SourceOwnerRef:     optionalString(input.SourceOwnerRef),
		AssigneeRef:        interactionActorRef(input.AssigneeRef),
		ActorRef:           optionalString(input.ActorRef),
		CorrelationRef:     interactionExternalRef(input.CorrelationRef),
		CorrelationId:      optionalString(input.CorrelationID),
		IncludeDiagnostics: input.IncludeDiagnostics,
		Page:               interactionPageRequest(input.Page),
	}, nil
}

func getOwnerInboxRequest(input GetOwnerInboxInput) (*interactionsv1.GetOwnerInboxItemRequest, error) {
	meta, err := interactionQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	requestID, err := requiredTrimmed(input.RequestID, "request_id")
	if err != nil {
		return nil, err
	}
	scope, err := interactionScope(input.Scope)
	if err != nil {
		return nil, err
	}
	return &interactionsv1.GetOwnerInboxItemRequest{
		Meta:               meta,
		RequestId:          requestID,
		Scope:              scope,
		AssigneeRef:        interactionActorRef(input.AssigneeRef),
		IncludeDiagnostics: input.IncludeDiagnostics,
	}, nil
}

func respondOwnerInboxRequest(input RespondOwnerInboxInput) (*interactionsv1.RecordInteractionResponseRequest, error) {
	meta, err := interactionCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	requestID, err := requiredTrimmed(input.RequestID, "request_id")
	if err != nil {
		return nil, err
	}
	action, err := interactionResponseAction(input.ResponseAction)
	if err != nil {
		return nil, err
	}
	respondedBy, err := requiredTrimmed(input.RespondedByActorRef, "responded_by_actor_ref")
	if err != nil {
		return nil, err
	}
	sourceKind, err := interactionResponseSourceKind(input.SourceKind)
	if err != nil {
		return nil, err
	}
	responseObject, err := interactionObjectRef(input.ResponseObject)
	if err != nil {
		return nil, err
	}
	return &interactionsv1.RecordInteractionResponseRequest{
		Meta:                meta,
		RequestId:           requestID,
		ResponseAction:      action,
		RespondedByActorRef: respondedBy,
		ResponseSummary:     optionalString(input.ResponseSummary),
		ResponseObject:      responseObject,
		SourceKind:          sourceKind,
		SourceRef:           optionalString(input.SourceRef),
		OwnerDecisionRef:    optionalString(input.OwnerDecisionRef),
	}, nil
}

func interactionCommandMeta(input InteractionCommandMetaInput) (*interactionsv1.CommandMeta, error) {
	actorValue, contextValue, err := interactionMetaActorAndContext(input.Actor, input.RequestContext)
	if err != nil {
		return nil, err
	}
	commandID := optionalString(input.CommandID)
	idempotencyKey := optionalString(input.IdempotencyKey)
	if commandID == nil && idempotencyKey == nil {
		return nil, invalidInput("command_id or idempotency_key is required")
	}
	requestID, err := requiredTrimmed(input.RequestID, "request_id")
	if err != nil {
		return nil, err
	}
	meta := &interactionsv1.CommandMeta{Actor: actorValue, RequestContext: contextValue}
	meta.CommandId = commandID
	meta.IdempotencyKey = idempotencyKey
	meta.ExpectedVersion = input.ExpectedVersion
	meta.Reason = strings.TrimSpace(input.Reason)
	meta.RequestId = requestID
	return meta, nil
}

func interactionQueryMeta(input InteractionQueryMetaInput) (*interactionsv1.QueryMeta, error) {
	actorValue, contextValue, err := interactionMetaActorAndContext(input.Actor, input.RequestContext)
	if err != nil {
		return nil, err
	}
	requestID, err := requiredTrimmed(input.RequestID, "request_id")
	if err != nil {
		return nil, err
	}
	meta := &interactionsv1.QueryMeta{}
	meta.Actor = actorValue
	meta.RequestId = requestID
	meta.RequestContext = contextValue
	return meta, nil
}

func interactionMetaActorAndContext(actorInput InteractionActorInput, contextInput InteractionRequestContextInput) (*interactionsv1.Actor, *interactionsv1.RequestContext, error) {
	actorValue, err := interactionActor(actorInput)
	if err != nil {
		return nil, nil, err
	}
	contextValue, err := interactionRequestContext(contextInput)
	if err != nil {
		return nil, nil, err
	}
	return actorValue, contextValue, nil
}

func interactionActor(input InteractionActorInput) (*interactionsv1.Actor, error) {
	actorType, actorID, err := actorFields(input.Type, input.ID)
	if err != nil {
		return nil, err
	}
	return &interactionsv1.Actor{Type: actorType, Id: actorID}, nil
}

func interactionRequestContext(input InteractionRequestContextInput) (*interactionsv1.RequestContext, error) {
	contextValue := &interactionsv1.RequestContext{}
	source, traceID, sessionID, clientIPHash, err := safeRequestContext(input.Source, input.TraceID, input.SessionID, input.ClientIPHash)
	if err != nil {
		return nil, err
	}
	contextValue.Source = source
	contextValue.TraceId = traceID
	contextValue.SessionId = sessionID
	contextValue.ClientIpHash = clientIPHash
	return contextValue, nil
}

func interactionScope(input InteractionScopeInput) (*interactionsv1.ScopeRef, error) {
	scopeRef, err := requiredTrimmed(input.Ref, "scope.ref")
	if err != nil {
		return nil, err
	}
	scopeType, err := requiredEnumValue(interactionEnumKey(input.Type), interactionScopeTypes, interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_UNSPECIFIED, "scope.type")
	if err != nil {
		return nil, err
	}
	return &interactionsv1.ScopeRef{Type: scopeType, Ref: scopeRef}, nil
}

func interactionActorRef(input InteractionActorRefInput) *interactionsv1.ActorRef {
	refKind, ref, ok := optionalInteractionRef(input.RefKind, input.Ref)
	if !ok {
		return nil
	}
	return &interactionsv1.ActorRef{RefKind: refKind, Ref: ref}
}

func interactionExternalRef(input InteractionExternalRefInput) *interactionsv1.ExternalRef {
	refKind, ref, ok := optionalInteractionRef(input.RefKind, input.Ref)
	if !ok {
		return nil
	}
	return &interactionsv1.ExternalRef{RefKind: refKind, Ref: ref}
}

func optionalInteractionRef(refKindInput, refInput string) (string, string, bool) {
	refKind := strings.TrimSpace(refKindInput)
	ref := strings.TrimSpace(refInput)
	return refKind, ref, refKind != "" || ref != ""
}

func interactionObjectRef(input InteractionObjectInput) (*interactionsv1.ObjectRef, error) {
	if strings.TrimSpace(input.ObjectURI) == "" && strings.TrimSpace(input.ObjectDigest) == "" && input.ObjectSizeBytes == nil {
		return nil, nil
	}
	objectURI, err := requiredTrimmed(input.ObjectURI, "response_object.object_uri")
	if err != nil {
		return nil, err
	}
	objectDigest, err := requiredTrimmed(input.ObjectDigest, "response_object.object_digest")
	if err != nil {
		return nil, err
	}
	return &interactionsv1.ObjectRef{
		ObjectUri:       objectURI,
		ObjectDigest:    objectDigest,
		ObjectSizeBytes: input.ObjectSizeBytes,
	}, nil
}

func interactionPageRequest(input InteractionPageInput) *interactionsv1.PageRequest {
	return &interactionsv1.PageRequest{
		PageSize:  input.PageSize,
		PageToken: optionalString(input.PageToken),
	}
}

func interactionRequestKindList(inputs []string) ([]interactionsv1.InteractionRequestKind, error) {
	return interactionEnumList(inputs, "request_kinds", interactionRequestKinds, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_UNSPECIFIED)
}

func interactionRequestStatusList(inputs []string) ([]interactionsv1.InteractionRequestStatus, error) {
	return interactionEnumList(inputs, "statuses", interactionRequestStatuses, interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_UNSPECIFIED)
}

func interactionEnumList[Enum comparable](inputs []string, field string, values map[string]Enum, zero Enum) ([]Enum, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	result := make([]Enum, 0, len(inputs))
	for _, input := range inputs {
		value, err := requiredEnumValue(interactionEnumKey(input), values, zero, field)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func optionalInteractionSourceOwnerKind(value string) (*interactionsv1.SourceOwnerKind, error) {
	return optionalInteractionEnum(value, "source_owner_kind", interactionSourceOwnerKinds, interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_UNSPECIFIED)
}

func optionalInteractionEnum[Enum comparable](value string, field string, values map[string]Enum, zero Enum) (*Enum, error) {
	return optionalEnumValue(interactionEnumKey(value), values, zero, field)
}

func interactionResponseAction(value string) (interactionsv1.InteractionResponseAction, error) {
	return requiredEnumValue(interactionEnumKey(value), interactionResponseActions, interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_UNSPECIFIED, "response_action")
}

func interactionResponseSourceKind(value string) (interactionsv1.InteractionResponseSourceKind, error) {
	return requiredEnumValue(interactionEnumKey(value), interactionResponseSourceKinds, interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_UNSPECIFIED, "source_kind")
}

func ownerInboxOutput(response *interactionsv1.OwnerInboxItemResponse) OwnerInboxOutput {
	if response == nil {
		return OwnerInboxOutput{}
	}
	return OwnerInboxOutput{Item: ownerInboxItemSummary(response.GetItem())}
}

func ownerInboxListOutput(response *interactionsv1.ListOwnerInboxItemsResponse) OwnerInboxListOutput {
	if response == nil {
		return OwnerInboxListOutput{}
	}
	return OwnerInboxListOutput{
		Items: ownerInboxItemSummaries(response.GetItems()),
		Page:  interactionPageSummary(response.GetPage()),
	}
}

func ownerInboxResponseOutput(response *interactionsv1.InteractionResponseResponse) OwnerInboxResponseOutput {
	if response == nil {
		return OwnerInboxResponseOutput{}
	}
	return OwnerInboxResponseOutput{
		Request:  ownerInboxRequestSummary(response.GetRequest()),
		Response: interactionResponseSummary(response.GetResponse()),
	}
}

func ownerInboxItemSummaries(items []*interactionsv1.OwnerInboxItem) []OwnerInboxItemSummary {
	return summarizeItems(items, ownerInboxItemSummary)
}

func ownerInboxItemSummary(item *interactionsv1.OwnerInboxItem) OwnerInboxItemSummary {
	if item == nil {
		return OwnerInboxItemSummary{}
	}
	return OwnerInboxItemSummary{
		RequestID:         item.GetRequestId(),
		RequestKind:       interactionRequestKindName(item.GetRequestKind()),
		RequestStatus:     interactionRequestStatusName(item.GetRequestStatus()),
		Scope:             interactionScopeSummary(item.GetScope()),
		Requester:         interactionSourceOwnerSummary(item.GetRequester()),
		DecisionOwner:     interactionDecisionOwnerSummaryPtr(item.GetDecisionOwner()),
		AssigneeRefs:      interactionActorRefSummaries(item.GetAssigneeRefs()),
		ContextRefs:       interactionExternalRefSummaries(item.GetContextRefs()),
		Title:             item.GetTitle(),
		Summary:           item.GetSummary(),
		DeadlineAt:        item.GetDeadlineAt(),
		ReminderPolicyRef: item.GetReminderPolicyRef(),
		DeliverySummary:   ownerInboxDeliverySummary(item.GetDeliverySummary()),
		LatestCallback:    ownerInboxCallbackSummaryPtr(item.GetLatestCallback()),
		LatestResponse:    ownerInboxResponseSummaryPtr(item.GetLatestResponse()),
		CreatedAt:         item.GetCreatedAt(),
		UpdatedAt:         item.GetUpdatedAt(),
		ResolvedAt:        item.GetResolvedAt(),
		Version:           item.GetVersion(),
		AllowedActions:    interactionActionSummaries(item.GetAllowedActions()),
	}
}

func ownerInboxRequestSummary(request *interactionsv1.InteractionRequest) OwnerInboxRequestSummary {
	if request == nil {
		return OwnerInboxRequestSummary{}
	}
	return OwnerInboxRequestSummary{
		ID:            request.GetId(),
		RequestKind:   interactionRequestKindName(request.GetRequestKind()),
		Scope:         interactionScopeSummary(request.GetScope()),
		SourceOwner:   interactionSourceOwnerSummary(request.GetSourceOwner()),
		DecisionOwner: interactionDecisionOwnerSummaryPtr(request.GetDecisionOwner()),
		TargetRefs:    interactionActorRefSummaries(request.GetTargetRefs()),
		ContextRefs:   interactionExternalRefSummaries(request.GetContextRefs()),
		PromptSummary: request.GetPromptSummary(),
		Status:        interactionRequestStatusName(request.GetStatus()),
		Version:       request.GetVersion(),
		CreatedAt:     request.GetCreatedAt(),
		UpdatedAt:     request.GetUpdatedAt(),
		ResolvedAt:    request.GetResolvedAt(),
	}
}

func ownerInboxDeliverySummary(summary *interactionsv1.OwnerInboxDeliverySummary) OwnerInboxDeliverySummary {
	if summary == nil {
		return OwnerInboxDeliverySummary{}
	}
	return OwnerInboxDeliverySummary{
		AttemptCount:            summary.GetAttemptCount(),
		LatestDeliveryAttemptID: summary.GetLatestDeliveryAttemptId(),
		LatestDeliveryID:        summary.GetLatestDeliveryId(),
		LatestStatus:            interactionDeliveryAttemptStatusName(summary.GetLatestStatus()),
		LatestErrorCode:         summary.GetLatestErrorCode(),
		LatestErrorClass:        interactionDeliveryErrorClassName(summary.GetLatestErrorClass()),
		NextRetryAt:             summary.GetNextRetryAt(),
		LatestUpdatedAt:         summary.GetLatestUpdatedAt(),
		RouteID:                 summary.GetRouteId(),
		ChannelMessageRef:       summary.GetChannelMessageRef(),
	}
}

func ownerInboxCallbackSummaryPtr(summary *interactionsv1.OwnerInboxCallbackSummary) *OwnerInboxCallbackSummary {
	if summary == nil {
		return nil
	}
	result := OwnerInboxCallbackSummary{
		CallbackRef:      summary.GetCallbackRef(),
		CallbackID:       summary.GetCallbackId(),
		DeliveryID:       summary.GetDeliveryId(),
		SignatureStatus:  interactionCallbackSignatureStatusName(summary.GetSignatureStatus()),
		ProcessingStatus: interactionCallbackProcessingStatusName(summary.GetProcessingStatus()),
		ActorRef:         summary.GetActorRef(),
		Action:           summary.GetAction(),
		ErrorCode:        summary.GetErrorCode(),
		ReceivedAt:       summary.GetReceivedAt(),
		GatewayRef:       summary.GetGatewayRef(),
		CorrelationID:    summary.GetCorrelationId(),
	}
	return &result
}

func ownerInboxResponseSummaryPtr(summary *interactionsv1.OwnerInboxResponseSummary) *OwnerInboxResponseSummary {
	if summary == nil {
		return nil
	}
	objectSummary := interactionObjectSummaryPtr(summary.GetResponseObject())
	result := OwnerInboxResponseSummary{
		ResponseID:             summary.GetResponseId(),
		ResponseAction:         interactionResponseActionName(summary.GetResponseAction()),
		RespondedByActorRef:    summary.GetRespondedByActorRef(),
		SourceKind:             interactionResponseSourceKindName(summary.GetSourceKind()),
		SourceRef:              summary.GetSourceRef(),
		OwnerDecisionRef:       summary.GetOwnerDecisionRef(),
		CreatedAt:              summary.GetCreatedAt(),
		ResponseSummary:        summary.GetResponseSummary(),
		ResponseSummaryDigest:  summary.GetResponseSummaryDigest(),
		ResponseObject:         objectSummary,
		InteractionResponseRef: summary.GetInteractionResponseRef(),
	}
	return &result
}

func interactionResponseSummary(response *interactionsv1.InteractionResponse) OwnerInboxResponseSummary {
	if response == nil {
		return OwnerInboxResponseSummary{}
	}
	return OwnerInboxResponseSummary{
		ResponseID:          response.GetId(),
		RequestID:           response.GetRequestId(),
		ResponseAction:      interactionResponseActionName(response.GetResponseAction()),
		RespondedByActorRef: response.GetRespondedByActorRef(),
		ResponseSummary:     response.GetResponseSummary(),
		ResponseObject:      interactionObjectSummaryPtr(response.GetResponseObject()),
		SourceKind:          interactionResponseSourceKindName(response.GetSourceKind()),
		SourceRef:           response.GetSourceRef(),
		OwnerDecisionRef:    response.GetOwnerDecisionRef(),
		CreatedAt:           response.GetCreatedAt(),
	}
}

func interactionScopeSummary(scope *interactionsv1.ScopeRef) InteractionScopeSummary {
	if scope == nil {
		return InteractionScopeSummary{}
	}
	return InteractionScopeSummary{Type: interactionScopeTypeName(scope.GetType()), Ref: scope.GetRef()}
}

func interactionSourceOwnerSummary(source *interactionsv1.SourceOwnerRef) InteractionSourceOwnerSummary {
	if source == nil {
		return InteractionSourceOwnerSummary{}
	}
	return InteractionSourceOwnerSummary{Kind: interactionSourceOwnerKindName(source.GetKind()), Ref: source.GetRef()}
}

func interactionDecisionOwnerSummaryPtr(owner *interactionsv1.DecisionOwnerRef) *InteractionDecisionOwnerSummary {
	if owner == nil {
		return nil
	}
	result := InteractionDecisionOwnerSummary{
		OwnerKind:        interactionDecisionOwnerKindName(owner.GetOwnerKind()),
		OwnerRequestRef:  owner.GetOwnerRequestRef(),
		OwnerDecisionRef: owner.GetOwnerDecisionRef(),
	}
	return &result
}

func interactionActorRefSummaries(refs []*interactionsv1.ActorRef) []InteractionActorRefSummary {
	return summarizeItems(refs, interactionActorRefSummary)
}

func interactionActorRefSummary(ref *interactionsv1.ActorRef) InteractionActorRefSummary {
	if ref == nil {
		return InteractionActorRefSummary{}
	}
	return InteractionActorRefSummary{RefKind: ref.GetRefKind(), Ref: ref.GetRef()}
}

func interactionExternalRefSummaries(refs []*interactionsv1.ExternalRef) []InteractionExternalRefSummary {
	return summarizeItems(refs, interactionExternalRefSummary)
}

func interactionExternalRefSummary(ref *interactionsv1.ExternalRef) InteractionExternalRefSummary {
	if ref == nil {
		return InteractionExternalRefSummary{}
	}
	return InteractionExternalRefSummary{RefKind: ref.GetRefKind(), Ref: ref.GetRef()}
}

func interactionActionSummaries(actions []*interactionsv1.InteractionAction) []InteractionActionSummary {
	return summarizeItems(actions, interactionActionSummary)
}

func interactionActionSummary(action *interactionsv1.InteractionAction) InteractionActionSummary {
	if action == nil {
		return InteractionActionSummary{}
	}
	return InteractionActionSummary{
		ActionKey:        action.GetActionKey(),
		LabelTemplateRef: action.GetLabelTemplateRef(),
		IsTerminal:       action.GetIsTerminal(),
	}
}

func interactionObjectSummaryPtr(object *interactionsv1.ObjectRef) *InteractionObjectSummary {
	if object == nil {
		return nil
	}
	result := InteractionObjectSummary{
		ObjectURI:       object.GetObjectUri(),
		ObjectDigest:    object.GetObjectDigest(),
		ObjectSizeBytes: object.ObjectSizeBytes,
	}
	return &result
}

func interactionPageSummary(page *interactionsv1.PageResponse) PageSummary {
	if page == nil {
		return PageSummary{}
	}
	return PageSummary{NextPageToken: page.GetNextPageToken()}
}

func interactionScopeTypeName(value interactionsv1.InteractionScopeType) string {
	return enumName(value, interactionScopeTypeNames)
}

func interactionRequestKindName(value interactionsv1.InteractionRequestKind) string {
	return enumName(value, interactionRequestKindNames)
}

func interactionRequestStatusName(value interactionsv1.InteractionRequestStatus) string {
	return enumName(value, interactionRequestStatusNames)
}

func interactionSourceOwnerKindName(value interactionsv1.SourceOwnerKind) string {
	return enumName(value, interactionSourceOwnerKindNames)
}

func interactionDecisionOwnerKindName(value interactionsv1.DecisionOwnerKind) string {
	return enumName(value, interactionDecisionOwnerKindNames)
}

func interactionResponseActionName(value interactionsv1.InteractionResponseAction) string {
	return enumName(value, interactionResponseActionNames)
}

func interactionResponseSourceKindName(value interactionsv1.InteractionResponseSourceKind) string {
	return enumName(value, interactionResponseSourceKindNames)
}

func interactionDeliveryAttemptStatusName(value interactionsv1.DeliveryAttemptStatus) string {
	return enumName(value, interactionDeliveryAttemptStatusNames)
}

func interactionDeliveryErrorClassName(value interactionsv1.DeliveryErrorClass) string {
	return enumName(value, interactionDeliveryErrorClassNames)
}

func interactionCallbackSignatureStatusName(value interactionsv1.CallbackSignatureStatus) string {
	return enumName(value, interactionCallbackSignatureStatusNames)
}

func interactionCallbackProcessingStatusName(value interactionsv1.CallbackProcessingStatus) string {
	return enumName(value, interactionCallbackProcessingStatusNames)
}

func interactionEnumKey(value string) string {
	key := normalizedKey(value)
	prefixes := []string{
		"interaction_scope_type_",
		"interaction_request_kind_",
		"interaction_request_status_",
		"source_owner_kind_",
		"interaction_response_action_",
		"interaction_response_source_kind_",
	}
	for _, prefix := range prefixes {
		key = strings.TrimPrefix(key, prefix)
	}
	return key
}
