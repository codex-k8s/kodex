package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http/generated"
)

const defaultPageSize = 25

type OwnerInboxRespondBody = generated.OwnerInboxRespondRequest

func ListOwnerInboxItemsRequest(req *http.Request) (*interactionsv1.ListOwnerInboxItemsRequest, *SafeError) {
	query := req.URL.Query()
	meta, safeErr := queryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	scope, safeErr := scopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	requestKinds, safeErr := requestKindsFromQuery(queryValues(query, "request_kind"))
	if safeErr != nil {
		return nil, safeErr
	}
	statuses, safeErr := requestStatusesFromQuery(queryValues(query, "status"))
	if safeErr != nil {
		return nil, safeErr
	}
	sourceOwnerKind, safeErr := sourceOwnerKindFromQuery(query.Get("source_owner_kind"))
	if safeErr != nil {
		return nil, safeErr
	}
	correlationRef, safeErr := optionalProtoRef(query.Get("correlation_kind"), query.Get("correlation_ref"), "correlation ref is invalid", newExternalRef)
	if safeErr != nil {
		return nil, safeErr
	}
	assigneeRef, safeErr := optionalProtoRef(query.Get("assignee_kind"), query.Get("assignee_ref"), "assignee ref is invalid", newActorRef)
	if safeErr != nil {
		return nil, safeErr
	}
	page, safeErr := pageFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.ListOwnerInboxItemsRequest{
		Meta:               meta,
		Scope:              scope,
		RequestKinds:       requestKinds,
		Statuses:           statuses,
		SourceOwnerKind:    sourceOwnerKind,
		SourceOwnerRef:     optionalString(query.Get("source_owner_ref")),
		AssigneeRef:        assigneeRef,
		ActorRef:           optionalString(query.Get("actor_ref")),
		CorrelationRef:     correlationRef,
		CorrelationId:      optionalString(query.Get("correlation_id")),
		IncludeDiagnostics: parseBool(query.Get("include_diagnostics")),
		Page:               page,
	}, nil
}

func GetOwnerInboxItemRequest(req *http.Request) (*interactionsv1.GetOwnerInboxItemRequest, *SafeError) {
	meta, safeErr := queryMeta(req)
	if safeErr != nil {
		return nil, safeErr
	}
	scope, safeErr := scopeFromQuery(req)
	if safeErr != nil {
		return nil, safeErr
	}
	requestID := strings.TrimSpace(req.PathValue("request_id"))
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request id is invalid", false)
	}
	assigneeRef, safeErr := optionalProtoRef(req.URL.Query().Get("assignee_kind"), req.URL.Query().Get("assignee_ref"), "assignee ref is invalid", newActorRef)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.GetOwnerInboxItemRequest{
		Meta:               meta,
		RequestId:          requestID,
		Scope:              scope,
		AssigneeRef:        assigneeRef,
		IncludeDiagnostics: parseBool(req.URL.Query().Get("include_diagnostics")),
	}, nil
}

func RecordInteractionResponseRequest(req *http.Request, body OwnerInboxRespondBody) (*interactionsv1.RecordInteractionResponseRequest, *SafeError) {
	meta, actor, safeErr := commandMeta(req, body)
	if safeErr != nil {
		return nil, safeErr
	}
	requestID := strings.TrimSpace(req.PathValue("request_id"))
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request id is invalid", false)
	}
	action, safeErr := responseActionProto(string(body.Action))
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.RecordInteractionResponseRequest{
		Meta:                meta,
		RequestId:           requestID,
		ResponseAction:      action,
		RespondedByActorRef: actorRefString(actor),
		ResponseSummary:     body.ResponseSummary,
		ResponseObject:      objectRefProto(body.ResponseObject),
		SourceKind:          interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_WEB_CONSOLE,
		SourceRef:           optionalString("staff-gateway/" + requestIDFromContext(req.Context())),
		OwnerDecisionRef:    body.OwnerDecisionRef,
	}, nil
}

func ListOwnerInboxItemsResponse(response *interactionsv1.ListOwnerInboxItemsResponse, requestID string) (generated.OwnerInboxListResponse, *SafeError) {
	if response == nil {
		return generated.OwnerInboxListResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty response", true)
	}
	items := make([]generated.OwnerInboxItem, 0, len(response.GetItems()))
	for _, item := range response.GetItems() {
		casted, safeErr := ownerInboxItem(item)
		if safeErr != nil {
			return generated.OwnerInboxListResponse{}, safeErr
		}
		items = append(items, casted)
	}
	return generated.OwnerInboxListResponse{
		RequestId:     requestID,
		CorrelationId: optionalString(requestID),
		Items:         items,
		Page:          pageInfo(response.GetPage()),
	}, nil
}

func OwnerInboxItemResponse(response *interactionsv1.OwnerInboxItemResponse, requestID string) (generated.OwnerInboxItemResponse, *SafeError) {
	if response == nil {
		return generated.OwnerInboxItemResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty response", true)
	}
	item, safeErr := ownerInboxItem(response.GetItem())
	if safeErr != nil {
		return generated.OwnerInboxItemResponse{}, safeErr
	}
	return generated.OwnerInboxItemResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Item: item}, nil
}

func OwnerInboxRespondResponse(response *interactionsv1.InteractionResponseResponse, requestID string) (generated.OwnerInboxRespondResponse, *SafeError) {
	if response == nil || response.GetRequest() == nil || response.GetResponse() == nil {
		return generated.OwnerInboxRespondResponse{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty response", true)
	}
	item, safeErr := responseItem(response.GetRequest(), response.GetResponse())
	if safeErr != nil {
		return generated.OwnerInboxRespondResponse{}, safeErr
	}
	summary, safeErr := responseSummary(response.GetResponse())
	if safeErr != nil {
		return generated.OwnerInboxRespondResponse{}, safeErr
	}
	return generated.OwnerInboxRespondResponse{RequestId: requestID, CorrelationId: optionalString(requestID), Item: item, Response: summary}, nil
}

func queryMeta(req *http.Request) (*interactionsv1.QueryMeta, *SafeError) {
	actor, safeErr := actorFromHeaders(req)
	if safeErr != nil {
		return nil, safeErr
	}
	return &interactionsv1.QueryMeta{
		Actor:     actor,
		RequestId: requestIDFromContext(req.Context()),
		RequestContext: &interactionsv1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   optionalString(traceID(req)),
			SessionId: optionalString(req.Header.Get(headerSessionID)),
		},
	}, nil
}

func commandMeta(req *http.Request, body OwnerInboxRespondBody) (*interactionsv1.CommandMeta, *interactionsv1.Actor, *SafeError) {
	actor, safeErr := actorFromHeaders(req)
	if safeErr != nil {
		return nil, nil, safeErr
	}
	if trimmed(body.CommandId) == "" && trimmed(body.IdempotencyKey) == "" {
		return nil, nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "command id or idempotency key is required", false)
	}
	if body.ExpectedVersion <= 0 {
		return nil, nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "expected version is required", false)
	}
	reason := trimmed(body.Reason)
	if reason == "" {
		reason = "staff-gateway owner inbox response"
	}
	meta := &interactionsv1.CommandMeta{
		CommandId:       trimOptional(body.CommandId),
		IdempotencyKey:  trimOptional(body.IdempotencyKey),
		ExpectedVersion: &body.ExpectedVersion,
		Actor:           actor,
		Reason:          reason,
		RequestId:       requestIDFromContext(req.Context()),
		RequestContext: &interactionsv1.RequestContext{
			Source:    "staff-gateway",
			TraceId:   optionalString(traceID(req)),
			SessionId: optionalString(req.Header.Get(headerSessionID)),
		},
	}
	return meta, actor, nil
}

func actorFromHeaders(req *http.Request) (*interactionsv1.Actor, *SafeError) {
	actorType := strings.TrimSpace(req.Header.Get(headerActorType))
	actorID := strings.TrimSpace(req.Header.Get(headerActorID))
	if actorID == "" || len(actorID) > 256 || !validActorType(actorType) {
		return nil, NewSafeError(http.StatusUnauthorized, CodeUnauthenticated, "actor context is required", false)
	}
	return &interactionsv1.Actor{Type: actorType, Id: actorID}, nil
}

func validActorType(value string) bool {
	switch value {
	case "user", "service", "agent", "external_account":
		return true
	default:
		return false
	}
}

func actorRefString(actor *interactionsv1.Actor) string {
	return actor.GetType() + "/" + actor.GetId()
}

func traceID(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get(headerTraceID)); value != "" {
		return value
	}
	return requestIDFromContext(req.Context())
}

func scopeFromQuery(req *http.Request) (*interactionsv1.ScopeRef, *SafeError) {
	scopeType, safeErr := scopeTypeProto(req.URL.Query().Get("scope_type"))
	if safeErr != nil {
		return nil, safeErr
	}
	scopeRef := strings.TrimSpace(req.URL.Query().Get("scope_ref"))
	if scopeRef == "" {
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "scope ref is required", false)
	}
	return &interactionsv1.ScopeRef{Type: scopeType, Ref: scopeRef}, nil
}

func scopeTypeProto(value string) (interactionsv1.InteractionScopeType, *SafeError) {
	switch strings.TrimSpace(value) {
	case "platform":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PLATFORM, nil
	case "organization":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_ORGANIZATION, nil
	case "project":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT, nil
	case "repository":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_REPOSITORY, nil
	case "service":
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_SERVICE, nil
	default:
		return interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "scope type is invalid", false)
	}
}

func requestKindsFromQuery(values []string) ([]interactionsv1.InteractionRequestKind, *SafeError) {
	items := splitQueryValues(values)
	result := make([]interactionsv1.InteractionRequestKind, 0, len(items))
	for _, item := range items {
		switch item {
		case "feedback":
			result = append(result, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_FEEDBACK)
		case "approval":
			result = append(result, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_APPROVAL)
		case "human_gate":
			result = append(result, interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE)
		default:
			return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request kind is invalid", false)
		}
	}
	return result, nil
}

func requestStatusesFromQuery(values []string) ([]interactionsv1.InteractionRequestStatus, *SafeError) {
	items := splitQueryValues(values)
	result := make([]interactionsv1.InteractionRequestStatus, 0, len(items))
	for _, item := range items {
		status, ok := map[string]interactionsv1.InteractionRequestStatus{
			"created":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CREATED,
			"routed":    interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ROUTED,
			"waiting":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING,
			"answered":  interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED,
			"expired":   interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_EXPIRED,
			"cancelled": interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_CANCELLED,
			"failed":    interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_FAILED,
		}[item]
		if !ok {
			return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request status is invalid", false)
		}
		result = append(result, status)
	}
	return result, nil
}

func sourceOwnerKindFromQuery(value string) (*interactionsv1.SourceOwnerKind, *SafeError) {
	switch strings.TrimSpace(value) {
	case "":
		return nil, nil
	case "agent_manager":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER
		return &item, nil
	case "slot_agent":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SLOT_AGENT
		return &item, nil
	case "governance_manager":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_GOVERNANCE_MANAGER
		return &item, nil
	case "provider_hub":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_PROVIDER_HUB
		return &item, nil
	case "operations_hub":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_OPERATIONS_HUB
		return &item, nil
	case "user":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_USER
		return &item, nil
	case "system":
		item := interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_SYSTEM
		return &item, nil
	default:
		return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "source owner kind is invalid", false)
	}
}

func responseActionProto(value string) (interactionsv1.InteractionResponseAction, *SafeError) {
	switch strings.TrimSpace(value) {
	case "answer":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ANSWER, nil
	case "approve":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_APPROVE, nil
	case "reject":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REJECT, nil
	case "defer":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_DEFER, nil
	case "acknowledge":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ACKNOWLEDGE, nil
	case "custom":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_CUSTOM, nil
	case "request_changes":
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES, nil
	default:
		return interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_UNSPECIFIED, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "response action is invalid", false)
	}
}

func optionalProtoRef[T any](kind string, ref string, invalidMessage string, build func(string, string) *T) (*T, *SafeError) {
	kind, ref, safeErr := optionalRefParts(kind, ref, invalidMessage)
	if safeErr != nil || kind == "" {
		return nil, safeErr
	}
	return build(kind, ref), nil
}

func newActorRef(kind string, ref string) *interactionsv1.ActorRef {
	return &interactionsv1.ActorRef{RefKind: kind, Ref: ref}
}

func newExternalRef(kind string, ref string) *interactionsv1.ExternalRef {
	return &interactionsv1.ExternalRef{RefKind: kind, Ref: ref}
}

func optionalRefParts(kind string, ref string, invalidMessage string) (string, string, *SafeError) {
	kind = strings.TrimSpace(kind)
	ref = strings.TrimSpace(ref)
	if kind == "" && ref == "" {
		return "", "", nil
	}
	if kind == "" || ref == "" {
		return "", "", NewSafeError(http.StatusBadRequest, CodeInvalidRequest, invalidMessage, false)
	}
	return kind, ref, nil
}

func pageFromQuery(req *http.Request) (*interactionsv1.PageRequest, *SafeError) {
	query := req.URL.Query()
	pageSize := defaultPageSize
	if raw := strings.TrimSpace(query.Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return nil, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "page size is invalid", false)
		}
		pageSize = parsed
	}
	return &interactionsv1.PageRequest{PageSize: int32(pageSize), PageToken: optionalString(query.Get("page_token"))}, nil
}

func ownerInboxItem(item *interactionsv1.OwnerInboxItem) (generated.OwnerInboxItem, *SafeError) {
	if item == nil {
		return generated.OwnerInboxItem{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned empty owner inbox item", true)
	}
	if _, err := uuid.Parse(item.GetRequestId()); err != nil {
		return generated.OwnerInboxItem{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned invalid owner inbox item", true)
	}
	createdAt, safeErr := requiredTime(item.GetCreatedAt())
	if safeErr != nil {
		return generated.OwnerInboxItem{}, safeErr
	}
	updatedAt, safeErr := requiredTime(item.GetUpdatedAt())
	if safeErr != nil {
		return generated.OwnerInboxItem{}, safeErr
	}
	deliverySummary, safeErr := deliverySummary(item.GetDeliverySummary())
	if safeErr != nil {
		return generated.OwnerInboxItem{}, safeErr
	}
	output := generated.OwnerInboxItem{
		RequestId:         item.GetRequestId(),
		RequestKind:       generated.RequestKind(enumName(item.GetRequestKind().String(), "INTERACTION_REQUEST_KIND_")),
		RequestStatus:     generated.RequestStatus(enumName(item.GetRequestStatus().String(), "INTERACTION_REQUEST_STATUS_")),
		Scope:             scopeRef(item.GetScope()),
		Requester:         sourceOwnerRef(item.GetRequester()),
		DecisionOwner:     decisionOwnerRef(item.GetDecisionOwner()),
		AssigneeRefs:      actorRefs(item.GetAssigneeRefs()),
		ContextRefs:       externalRefs(item.GetContextRefs()),
		Title:             item.GetTitle(),
		Summary:           item.GetSummary(),
		DeadlineAt:        optionalTime(item.GetDeadlineAt()),
		ReminderPolicyRef: optionalString(item.GetReminderPolicyRef()),
		DeliverySummary:   deliverySummary,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ResolvedAt:        optionalTime(item.GetResolvedAt()),
		Version:           item.GetVersion(),
		AllowedActions:    interactionActions(item.GetAllowedActions()),
	}
	if item.GetLatestCallback() != nil {
		casted, safeErr := callbackSummary(item.GetLatestCallback())
		if safeErr != nil {
			return generated.OwnerInboxItem{}, safeErr
		}
		output.LatestCallback = &casted
	}
	if item.GetLatestResponse() != nil {
		casted, safeErr := responseSummaryFromInbox(item.GetLatestResponse())
		if safeErr != nil {
			return generated.OwnerInboxItem{}, safeErr
		}
		output.LatestResponse = &casted
	}
	return output, nil
}

func responseItem(request *interactionsv1.InteractionRequest, response *interactionsv1.InteractionResponse) (generated.OwnerInboxItem, *SafeError) {
	item := &interactionsv1.OwnerInboxItem{
		RequestId:       request.GetId(),
		RequestKind:     request.GetRequestKind(),
		RequestStatus:   request.GetStatus(),
		Scope:           request.GetScope(),
		Requester:       request.GetSourceOwner(),
		DecisionOwner:   request.GetDecisionOwner(),
		AssigneeRefs:    request.GetTargetRefs(),
		ContextRefs:     request.GetContextRefs(),
		Title:           request.GetPromptSummary(),
		Summary:         request.GetPromptSummary(),
		DeadlineAt:      request.DeadlineAt,
		DeliverySummary: &interactionsv1.OwnerInboxDeliverySummary{},
		CreatedAt:       request.GetCreatedAt(),
		UpdatedAt:       request.GetUpdatedAt(),
		ResolvedAt:      request.ResolvedAt,
		Version:         request.GetVersion(),
		LatestResponse:  protoResponseSummary(response),
	}
	return ownerInboxItem(item)
}

func deliverySummary(summary *interactionsv1.OwnerInboxDeliverySummary) (generated.OwnerInboxDeliverySummary, *SafeError) {
	if summary == nil {
		return generated.OwnerInboxDeliverySummary{
			LatestStatus:     generated.DeliveryAttemptStatusUnspecified,
			LatestErrorClass: generated.DeliveryErrorClassUnspecified,
		}, nil
	}
	return generated.OwnerInboxDeliverySummary{
		AttemptCount:            summary.GetAttemptCount(),
		LatestDeliveryAttemptId: optionalString(summary.GetLatestDeliveryAttemptId()),
		LatestDeliveryId:        optionalString(summary.GetLatestDeliveryId()),
		LatestStatus:            generated.DeliveryAttemptStatus(enumName(summary.GetLatestStatus().String(), "DELIVERY_ATTEMPT_STATUS_")),
		LatestErrorCode:         optionalString(summary.GetLatestErrorCode()),
		LatestErrorClass:        generated.DeliveryErrorClass(enumName(summary.GetLatestErrorClass().String(), "DELIVERY_ERROR_CLASS_")),
		NextRetryAt:             optionalTime(summary.GetNextRetryAt()),
		LatestUpdatedAt:         optionalTime(summary.GetLatestUpdatedAt()),
		RouteId:                 optionalString(summary.GetRouteId()),
		ChannelMessageRef:       optionalString(summary.GetChannelMessageRef()),
	}, nil
}

func callbackSummary(summary *interactionsv1.OwnerInboxCallbackSummary) (generated.OwnerInboxCallbackSummary, *SafeError) {
	receivedAt, safeErr := requiredTime(summary.GetReceivedAt())
	if safeErr != nil {
		return generated.OwnerInboxCallbackSummary{}, safeErr
	}
	return generated.OwnerInboxCallbackSummary{
		CallbackRef:      summary.GetCallbackRef(),
		CallbackId:       summary.GetCallbackId(),
		DeliveryId:       optionalString(summary.GetDeliveryId()),
		SignatureStatus:  generated.CallbackSignatureStatus(enumName(summary.GetSignatureStatus().String(), "CALLBACK_SIGNATURE_STATUS_")),
		ProcessingStatus: generated.CallbackProcessingStatus(enumName(summary.GetProcessingStatus().String(), "CALLBACK_PROCESSING_STATUS_")),
		ActorRef:         optionalString(summary.GetActorRef()),
		Action:           optionalString(summary.GetAction()),
		ErrorCode:        optionalString(summary.GetErrorCode()),
		ReceivedAt:       receivedAt,
		GatewayRef:       optionalString(summary.GetGatewayRef()),
		CorrelationId:    optionalString(summary.GetCorrelationId()),
	}, nil
}

func responseSummaryFromInbox(summary *interactionsv1.OwnerInboxResponseSummary) (generated.OwnerInboxResponseSummary, *SafeError) {
	createdAt, safeErr := requiredTime(summary.GetCreatedAt())
	if safeErr != nil {
		return generated.OwnerInboxResponseSummary{}, safeErr
	}
	return generated.OwnerInboxResponseSummary{
		ResponseId:             summary.GetResponseId(),
		ResponseAction:         generated.ResponseAction(enumName(summary.GetResponseAction().String(), "INTERACTION_RESPONSE_ACTION_")),
		RespondedByActorRef:    summary.GetRespondedByActorRef(),
		SourceKind:             generated.ResponseSourceKind(enumName(summary.GetSourceKind().String(), "INTERACTION_RESPONSE_SOURCE_KIND_")),
		SourceRef:              optionalString(summary.GetSourceRef()),
		OwnerDecisionRef:       optionalString(summary.GetOwnerDecisionRef()),
		CreatedAt:              createdAt,
		ResponseSummary:        optionalString(summary.GetResponseSummary()),
		ResponseSummaryDigest:  optionalString(summary.GetResponseSummaryDigest()),
		ResponseObject:         objectRef(summary.GetResponseObject()),
		InteractionResponseRef: optionalString(summary.GetInteractionResponseRef()),
	}, nil
}

func responseSummary(response *interactionsv1.InteractionResponse) (generated.OwnerInboxResponseSummary, *SafeError) {
	return responseSummaryFromInbox(protoResponseSummary(response))
}

func protoResponseSummary(response *interactionsv1.InteractionResponse) *interactionsv1.OwnerInboxResponseSummary {
	return &interactionsv1.OwnerInboxResponseSummary{
		ResponseId:             response.GetId(),
		ResponseAction:         response.GetResponseAction(),
		RespondedByActorRef:    response.GetRespondedByActorRef(),
		SourceKind:             response.GetSourceKind(),
		SourceRef:              response.SourceRef,
		OwnerDecisionRef:       response.OwnerDecisionRef,
		CreatedAt:              response.GetCreatedAt(),
		ResponseSummary:        response.ResponseSummary,
		ResponseObject:         response.GetResponseObject(),
		InteractionResponseRef: optionalString(response.GetId()),
	}
}

func scopeRef(input *interactionsv1.ScopeRef) generated.ScopeRef {
	return generated.ScopeRef{Type: generated.ScopeType(enumName(input.GetType().String(), "INTERACTION_SCOPE_TYPE_")), Ref: input.GetRef()}
}

func sourceOwnerRef(input *interactionsv1.SourceOwnerRef) generated.SourceOwnerRef {
	return generated.SourceOwnerRef{Kind: generated.SourceOwnerKind(enumName(input.GetKind().String(), "SOURCE_OWNER_KIND_")), Ref: optionalString(input.GetRef())}
}

func decisionOwnerRef(input *interactionsv1.DecisionOwnerRef) *generated.DecisionOwnerRef {
	if input == nil || input.GetOwnerKind() == interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_UNSPECIFIED {
		return nil
	}
	return &generated.DecisionOwnerRef{
		OwnerKind:        generated.DecisionOwnerKind(enumName(input.GetOwnerKind().String(), "DECISION_OWNER_KIND_")),
		OwnerRequestRef:  input.GetOwnerRequestRef(),
		OwnerDecisionRef: optionalString(input.GetOwnerDecisionRef()),
	}
}

func actorRefs(input []*interactionsv1.ActorRef) []generated.ActorRef {
	result := make([]generated.ActorRef, 0, len(input))
	for index := range input {
		if input[index] != nil {
			result = append(result, generated.ActorRef{RefKind: input[index].GetRefKind(), Ref: input[index].GetRef()})
		}
	}
	return result
}

func externalRefs(input []*interactionsv1.ExternalRef) []generated.ExternalRef {
	return collectOwnerRefs(input, func(item *interactionsv1.ExternalRef) (generated.ExternalRef, bool) {
		if item == nil {
			return generated.ExternalRef{}, false
		}
		return generated.ExternalRef{RefKind: item.GetRefKind(), Ref: item.GetRef()}, true
	})
}

func collectOwnerRefs[Input any, Output any](input []Input, cast func(Input) (Output, bool)) []Output {
	result := make([]Output, 0, len(input))
	for index := range input {
		casted, ok := cast(input[index])
		if ok {
			result = append(result, casted)
		}
	}
	return result
}

func interactionActions(input []*interactionsv1.InteractionAction) []generated.InteractionAction {
	result := make([]generated.InteractionAction, 0, len(input))
	for _, item := range input {
		if item == nil {
			continue
		}
		result = append(result, generated.InteractionAction{
			ActionKey:        item.GetActionKey(),
			LabelTemplateRef: optionalString(item.GetLabelTemplateRef()),
			IsTerminal:       item.GetIsTerminal(),
		})
	}
	return result
}

func objectRefProto(input *generated.ObjectRef) *interactionsv1.ObjectRef {
	if input == nil {
		return nil
	}
	return &interactionsv1.ObjectRef{
		ObjectUri:       strings.TrimSpace(input.ObjectUri),
		ObjectDigest:    strings.TrimSpace(input.ObjectDigest),
		ObjectSizeBytes: input.ObjectSizeBytes,
	}
}

func objectRef(input *interactionsv1.ObjectRef) *generated.ObjectRef {
	if input == nil {
		return nil
	}
	return &generated.ObjectRef{
		ObjectUri:       input.GetObjectUri(),
		ObjectDigest:    input.GetObjectDigest(),
		ObjectSizeBytes: input.ObjectSizeBytes,
	}
}

func pageInfo(input *interactionsv1.PageResponse) generated.PageInfo {
	if input == nil {
		return generated.PageInfo{}
	}
	return generated.PageInfo{NextPageToken: optionalString(input.GetNextPageToken())}
}

func requiredTime(value string) (time.Time, *SafeError) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, NewSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub returned invalid timestamp", true)
	}
	return parsed.UTC(), nil
}

func optionalTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	result := parsed.UTC()
	return &result
}

func enumName(value string, prefix string) string {
	trimmed := strings.TrimPrefix(value, prefix)
	if trimmed == "" {
		return "unspecified"
	}
	return strings.ToLower(trimmed)
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalString(*value)
}

func trimmed(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func splitQueryValues(values []string) []string {
	var result []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func queryValues(values map[string][]string, key string) []string {
	return values[key]
}
