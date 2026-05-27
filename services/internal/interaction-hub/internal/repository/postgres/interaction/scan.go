package interaction

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func scanThread(row postgreslib.RowScanner) (entity.ConversationThread, error) {
	var thread entity.ConversationThread
	var latestMessageID pgtype.UUID
	var closedAt pgtype.Timestamptz
	var scopeType, threadKind, sourceKind, status string
	err := row.Scan(
		&thread.ID,
		&scopeType,
		&thread.Scope.Ref,
		&threadKind,
		&thread.PrimaryActorRef,
		&sourceKind,
		&thread.SourceRef,
		&status,
		&latestMessageID,
		&thread.CorrelationID,
		&thread.RetentionClass,
		&thread.Version,
		&thread.CreatedAt,
		&thread.UpdatedAt,
		&closedAt,
	)
	thread.Scope.Type = enum.ScopeType(scopeType)
	thread.ThreadKind = enum.ConversationThreadKind(threadKind)
	thread.SourceKind = enum.ConversationSourceKind(sourceKind)
	thread.Status = enum.ConversationThreadStatus(status)
	thread.LatestMessageID = postgreslib.UUIDPtrFromPG(latestMessageID)
	thread.ClosedAt = postgreslib.TimePtrFromPG(closedAt)
	return thread, err
}

func scanMessage(row postgreslib.RowScanner) (entity.ConversationMessage, error) {
	var message entity.ConversationMessage
	var messageKind string
	var objectSize pgtype.Int8
	var safeMetadata []byte
	err := row.Scan(
		&message.ID,
		&message.ThreadID,
		&messageKind,
		&message.AuthorRef,
		&message.BodySummary,
		&message.BodyObject.URI,
		&message.BodyObject.Digest,
		&objectSize,
		&message.BodyDigest,
		&message.Locale,
		&safeMetadata,
		&message.CreatedAt,
	)
	message.MessageKind = enum.ConversationMessageKind(messageKind)
	if objectSize.Valid {
		value := objectSize.Int64
		message.BodyObject.SizeBytes = &value
	}
	if err != nil {
		return message, err
	}
	if len(safeMetadata) == 0 {
		message.SafeMetadata = map[string]string{}
		return message, nil
	}
	if err := json.Unmarshal(safeMetadata, &message.SafeMetadata); err != nil {
		return message, err
	}
	if message.SafeMetadata == nil {
		message.SafeMetadata = map[string]string{}
	}
	return message, nil
}

func scanRequest(row postgreslib.RowScanner) (entity.InteractionRequest, error) {
	var request entity.InteractionRequest
	var requestKind, scopeType, sourceOwnerKind, ingressKind, decisionOwnerKind, riskClass, status string
	var threadID pgtype.UUID
	var promptObjectSize pgtype.Int8
	var targetRefs, contextRefs, allowedActions []byte
	var deadlineAt, resolvedAt pgtype.Timestamptz
	err := row.Scan(
		&request.ID,
		&requestKind,
		&scopeType,
		&request.Scope.Ref,
		&threadID,
		&sourceOwnerKind,
		&request.SourceOwner.Ref,
		&ingressKind,
		&request.Ingress.Ref,
		&decisionOwnerKind,
		&request.DecisionOwner.OwnerRequestRef,
		&request.DecisionOwner.OwnerDecisionRef,
		&targetRefs,
		&contextRefs,
		&request.PromptSummary,
		&request.PromptObject.URI,
		&request.PromptObject.Digest,
		&promptObjectSize,
		&allowedActions,
		&riskClass,
		&status,
		&deadlineAt,
		&request.ReminderPolicyRef,
		&request.Version,
		&request.CreatedAt,
		&request.UpdatedAt,
		&resolvedAt,
	)
	request.RequestKind = enum.InteractionRequestKind(requestKind)
	request.Scope.Type = enum.ScopeType(scopeType)
	request.ThreadID = postgreslib.UUIDPtrFromPG(threadID)
	request.SourceOwner.Kind = enum.SourceOwnerKind(sourceOwnerKind)
	request.Ingress.Kind = enum.IngressKind(ingressKind)
	request.DecisionOwner.Kind = enum.DecisionOwnerKind(decisionOwnerKind)
	if promptObjectSize.Valid {
		value := promptObjectSize.Int64
		request.PromptObject.SizeBytes = &value
	}
	request.RiskClass = enum.InteractionRiskClass(riskClass)
	request.Status = enum.InteractionRequestStatus(status)
	request.DeadlineAt = postgreslib.TimePtrFromPG(deadlineAt)
	request.ResolvedAt = postgreslib.TimePtrFromPG(resolvedAt)
	if err != nil {
		return request, err
	}
	if request.TargetRefs, err = unmarshalArray[value.ActorRef](targetRefs); err != nil {
		return request, err
	}
	if request.ContextRefs, err = unmarshalArray[value.ExternalRef](contextRefs); err != nil {
		return request, err
	}
	if request.AllowedActions, err = unmarshalArray[value.InteractionAction](allowedActions); err != nil {
		return request, err
	}
	return request, nil
}

func scanOwnerInboxItem(row postgreslib.RowScanner) (entity.OwnerInboxItem, error) {
	var item entity.OwnerInboxItem
	var requestKind, scopeType, sourceOwnerKind, ingressKind, decisionOwnerKind, riskClass, status string
	var threadID, latestAttemptID, latestRouteID pgtype.UUID
	var promptObjectSize pgtype.Int8
	var targetRefs, contextRefs, allowedActions []byte
	var deadlineAt, resolvedAt, latestNextRetryAt, latestUpdatedAt pgtype.Timestamptz
	var latestDeliveryStatus, latestDeliveryErrorClass string
	var responseID, callbackID pgtype.UUID
	var responseAction, responseActor, responseSourceKind, responseSourceRef, responseOwnerDecisionRef pgtype.Text
	var responseCreatedAt pgtype.Timestamptz
	var callbackCallbackID, callbackDeliveryID, callbackActorRef, callbackAction pgtype.Text
	var callbackSignatureStatus, callbackProcessingStatus, callbackErrorCode pgtype.Text
	var callbackReceivedAt pgtype.Timestamptz
	var callbackGatewayRef, callbackCorrelationID pgtype.Text
	err := row.Scan(
		&item.Request.ID,
		&requestKind,
		&scopeType,
		&item.Request.Scope.Ref,
		&threadID,
		&sourceOwnerKind,
		&item.Request.SourceOwner.Ref,
		&ingressKind,
		&item.Request.Ingress.Ref,
		&decisionOwnerKind,
		&item.Request.DecisionOwner.OwnerRequestRef,
		&item.Request.DecisionOwner.OwnerDecisionRef,
		&targetRefs,
		&contextRefs,
		&item.Request.PromptSummary,
		&item.Request.PromptObject.URI,
		&item.Request.PromptObject.Digest,
		&promptObjectSize,
		&allowedActions,
		&riskClass,
		&status,
		&deadlineAt,
		&item.Request.ReminderPolicyRef,
		&item.Request.Version,
		&item.Request.CreatedAt,
		&item.Request.UpdatedAt,
		&resolvedAt,
		&item.DeliverySummary.AttemptCount,
		&latestAttemptID,
		&item.DeliverySummary.LatestDeliveryID,
		&latestDeliveryStatus,
		&item.DeliverySummary.LatestErrorCode,
		&latestDeliveryErrorClass,
		&latestNextRetryAt,
		&latestUpdatedAt,
		&latestRouteID,
		&item.DeliverySummary.ChannelMessageRef,
		&responseID,
		&responseAction,
		&responseActor,
		&responseSourceKind,
		&responseSourceRef,
		&responseOwnerDecisionRef,
		&responseCreatedAt,
		&callbackID,
		&callbackCallbackID,
		&callbackDeliveryID,
		&callbackActorRef,
		&callbackAction,
		&callbackSignatureStatus,
		&callbackProcessingStatus,
		&callbackErrorCode,
		&callbackReceivedAt,
		&callbackGatewayRef,
		&callbackCorrelationID,
	)
	item.Request.RequestKind = enum.InteractionRequestKind(requestKind)
	item.Request.Scope.Type = enum.ScopeType(scopeType)
	item.Request.ThreadID = postgreslib.UUIDPtrFromPG(threadID)
	item.Request.SourceOwner.Kind = enum.SourceOwnerKind(sourceOwnerKind)
	item.Request.Ingress.Kind = enum.IngressKind(ingressKind)
	item.Request.DecisionOwner.Kind = enum.DecisionOwnerKind(decisionOwnerKind)
	if promptObjectSize.Valid {
		value := promptObjectSize.Int64
		item.Request.PromptObject.SizeBytes = &value
	}
	item.Request.RiskClass = enum.InteractionRiskClass(riskClass)
	item.Request.Status = enum.InteractionRequestStatus(status)
	item.Request.DeadlineAt = postgreslib.TimePtrFromPG(deadlineAt)
	item.Request.ResolvedAt = postgreslib.TimePtrFromPG(resolvedAt)
	item.DeliverySummary.LatestAttemptID = postgreslib.UUIDPtrFromPG(latestAttemptID)
	item.DeliverySummary.LatestStatus = enum.DeliveryAttemptStatus(latestDeliveryStatus)
	item.DeliverySummary.LatestErrorClass = enum.DeliveryErrorClass(latestDeliveryErrorClass)
	item.DeliverySummary.NextRetryAt = postgreslib.TimePtrFromPG(latestNextRetryAt)
	item.DeliverySummary.LatestUpdatedAt = postgreslib.TimePtrFromPG(latestUpdatedAt)
	item.DeliverySummary.RouteID = postgreslib.UUIDPtrFromPG(latestRouteID)
	if err != nil {
		return item, err
	}
	if item.Request.TargetRefs, err = unmarshalArray[value.ActorRef](targetRefs); err != nil {
		return item, err
	}
	if item.Request.ContextRefs, err = unmarshalArray[value.ExternalRef](contextRefs); err != nil {
		return item, err
	}
	if item.Request.AllowedActions, err = unmarshalArray[value.InteractionAction](allowedActions); err != nil {
		return item, err
	}
	if responseID.Valid {
		item.LatestResponse = &entity.InteractionResponse{
			ID:                  uuid.UUID(responseID.Bytes),
			RequestID:           item.Request.ID,
			ResponseAction:      enum.InteractionResponseAction(textFromPG(responseAction)),
			RespondedByActorRef: textFromPG(responseActor),
			SourceKind:          enum.InteractionResponseSourceKind(textFromPG(responseSourceKind)),
			SourceRef:           textFromPG(responseSourceRef),
			OwnerDecisionRef:    textFromPG(responseOwnerDecisionRef),
			CreatedAt:           *postgreslib.TimePtrFromPG(responseCreatedAt),
		}
	}
	if callbackID.Valid {
		item.LatestCallback = &entity.ChannelCallback{
			ID:               uuid.UUID(callbackID.Bytes),
			CallbackID:       textFromPG(callbackCallbackID),
			DeliveryID:       textFromPG(callbackDeliveryID),
			RequestID:        &item.Request.ID,
			ActorRef:         textFromPG(callbackActorRef),
			Action:           textFromPG(callbackAction),
			SignatureStatus:  enum.CallbackSignatureStatus(textFromPG(callbackSignatureStatus)),
			ProcessingStatus: enum.CallbackProcessingStatus(textFromPG(callbackProcessingStatus)),
			ErrorCode:        textFromPG(callbackErrorCode),
			ReceivedAt:       *postgreslib.TimePtrFromPG(callbackReceivedAt),
			GatewayRef:       textFromPG(callbackGatewayRef),
			CorrelationID:    textFromPG(callbackCorrelationID),
		}
	}
	return item, nil
}

func scanResponse(row postgreslib.RowScanner) (entity.InteractionResponse, error) {
	var response entity.InteractionResponse
	var responseAction, sourceKind string
	var responseObjectSize pgtype.Int8
	err := row.Scan(
		&response.ID,
		&response.RequestID,
		&responseAction,
		&response.RespondedByActorRef,
		&response.ResponseSummary,
		&response.ResponseObject.URI,
		&response.ResponseObject.Digest,
		&responseObjectSize,
		&sourceKind,
		&response.SourceRef,
		&response.OwnerDecisionRef,
		&response.CreatedAt,
	)
	response.ResponseAction = enum.InteractionResponseAction(responseAction)
	response.SourceKind = enum.InteractionResponseSourceKind(sourceKind)
	if responseObjectSize.Valid {
		value := responseObjectSize.Int64
		response.ResponseObject.SizeBytes = &value
	}
	return response, err
}

func scanNotification(row postgreslib.RowScanner) (entity.Notification, error) {
	var notification entity.Notification
	var scopeType, notificationKind, priority, status, sourceOwnerKind, ingressKind string
	var requestID, subscriptionID pgtype.UUID
	var recipientRefs, contextRefs, channelHintRefs []byte
	var expiresAt pgtype.Timestamptz
	err := row.Scan(
		&notification.ID,
		&scopeType,
		&notification.Scope.Ref,
		&notificationKind,
		&requestID,
		&subscriptionID,
		&recipientRefs,
		&notification.MessageTemplateRef,
		&notification.MessageSummary,
		&priority,
		&status,
		&notification.CreatedAt,
		&notification.UpdatedAt,
		&expiresAt,
		&sourceOwnerKind,
		&notification.SourceOwner.Ref,
		&ingressKind,
		&notification.Ingress.Ref,
		&contextRefs,
		&channelHintRefs,
		&notification.NotificationPolicyRef,
		&notification.MessageTitle,
		&notification.BodyPreview,
	)
	notification.Scope.Type = enum.ScopeType(scopeType)
	notification.NotificationKind = enum.NotificationKind(notificationKind)
	notification.RequestID = postgreslib.UUIDPtrFromPG(requestID)
	notification.SubscriptionID = postgreslib.UUIDPtrFromPG(subscriptionID)
	notification.Priority = enum.NotificationPriority(priority)
	notification.Status = enum.NotificationStatus(status)
	notification.ExpiresAt = postgreslib.TimePtrFromPG(expiresAt)
	notification.SourceOwner.Kind = enum.SourceOwnerKind(sourceOwnerKind)
	notification.Ingress.Kind = enum.IngressKind(ingressKind)
	if err != nil {
		return notification, err
	}
	if notification.RecipientRefs, err = unmarshalArray[value.ActorRef](recipientRefs); err != nil {
		return notification, err
	}
	if notification.ContextRefs, err = unmarshalArray[value.ExternalRef](contextRefs); err != nil {
		return notification, err
	}
	if notification.ChannelHintRefs, err = unmarshalArray[value.ExternalRef](channelHintRefs); err != nil {
		return notification, err
	}
	return notification, nil
}

func scanSubscription(row postgreslib.RowScanner) (entity.Subscription, error) {
	var subscription entity.Subscription
	var scopeType, status, sourceOwnerKind string
	var eventFilter, deliveryPreferences, channelHintRefs []byte
	err := row.Scan(
		&subscription.ID,
		&scopeType,
		&subscription.Scope.Ref,
		&subscription.SubscriberRef.Kind,
		&subscription.SubscriberRef.Ref,
		&eventFilter,
		&deliveryPreferences,
		&status,
		&subscription.Version,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
		&sourceOwnerKind,
		&subscription.SourceOwner.Ref,
		&channelHintRefs,
		&subscription.SubscriptionPolicyRef,
	)
	subscription.Scope.Type = enum.ScopeType(scopeType)
	subscription.Status = enum.SubscriptionStatus(status)
	subscription.SourceOwner.Kind = enum.SourceOwnerKind(sourceOwnerKind)
	if err != nil {
		return subscription, err
	}
	subscription.EventFilterJSON = jsonString(eventFilter)
	subscription.DeliveryPreferencesJSON = jsonString(deliveryPreferences)
	if subscription.ChannelHintRefs, err = unmarshalArray[value.ExternalRef](channelHintRefs); err != nil {
		return subscription, err
	}
	return subscription, nil
}

func scanDeliveryRoute(row postgreslib.RowScanner) (entity.DeliveryRoute, error) {
	var route entity.DeliveryRoute
	var scopeType, surfaceKind, status string
	err := row.Scan(
		&route.ID,
		&scopeType,
		&route.Scope.Ref,
		&surfaceKind,
		&route.ChannelCapabilityRef,
		&route.PackageInstallationRef,
		&route.RoutingPolicyRef,
		&status,
		&route.CreatedAt,
		&route.UpdatedAt,
		&route.PackageVersionRef,
		&route.CallbackRouteRef,
		&route.RuntimeRef,
	)
	route.Scope.Type = enum.ScopeType(scopeType)
	route.SurfaceKind = enum.DeliverySurfaceKind(surfaceKind)
	route.Status = enum.DeliveryRouteStatus(status)
	return route, err
}

func scanDeliveryAttempt(row postgreslib.RowScanner) (entity.DeliveryAttempt, error) {
	var attempt entity.DeliveryAttempt
	var requestID, notificationID pgtype.UUID
	var deliveryKind, status, errorClass string
	var nextRetryAt, sentAt pgtype.Timestamptz
	err := row.Scan(
		&attempt.ID,
		&requestID,
		&notificationID,
		&attempt.RouteID,
		&attempt.DeliveryID,
		&deliveryKind,
		&status,
		&attempt.ChannelMessageRef,
		&attempt.AttemptNumber,
		&nextRetryAt,
		&attempt.ErrorCode,
		&errorClass,
		&attempt.PayloadDigest,
		&attempt.ResultFingerprint,
		&attempt.ChannelCapabilityRef,
		&attempt.PackageInstallationRef,
		&attempt.PackageVersionRef,
		&attempt.DeliveryCommandRef,
		&attempt.CallbackRef,
		&attempt.CallbackRouteRef,
		&attempt.RuntimeRef,
		&attempt.RuntimeJobRef,
		&attempt.RoutingPolicyRef,
		&attempt.CreatedAt,
		&attempt.UpdatedAt,
		&sentAt,
	)
	attempt.Target = deliveryTargetFromPG(requestID, notificationID)
	attempt.DeliveryKind = enum.DeliveryKind(deliveryKind)
	attempt.Status = enum.DeliveryAttemptStatus(status)
	attempt.ErrorClass = enum.DeliveryErrorClass(errorClass)
	attempt.NextRetryAt = postgreslib.TimePtrFromPG(nextRetryAt)
	attempt.SentAt = postgreslib.TimePtrFromPG(sentAt)
	return attempt, err
}

func scanChannelCallback(row postgreslib.RowScanner) (entity.ChannelCallback, error) {
	var callback entity.ChannelCallback
	var deliveryAttemptID, requestID, sourceRouteID pgtype.UUID
	var signatureStatus, processingStatus string
	var objectSize pgtype.Int8
	err := row.Scan(
		&callback.ID,
		&callback.CallbackID,
		&callback.DeliveryID,
		&deliveryAttemptID,
		&requestID,
		&sourceRouteID,
		&callback.ActorRef,
		&callback.Action,
		&callback.CallbackSummary,
		&callback.CallbackObject.URI,
		&callback.CallbackObject.Digest,
		&objectSize,
		&signatureStatus,
		&processingStatus,
		&callback.ErrorCode,
		&callback.ReceivedAt,
		&callback.CreatedAt,
		&callback.CallbackRouteRef,
		&callback.GatewayRef,
		&callback.CorrelationID,
		&callback.CallbackFingerprint,
	)
	callback.DeliveryAttemptID = uuidFromPG(deliveryAttemptID)
	callback.RequestID = uuidFromPG(requestID)
	callback.SourceRouteID = uuidFromPG(sourceRouteID)
	callback.SignatureStatus = enum.CallbackSignatureStatus(signatureStatus)
	callback.ProcessingStatus = enum.CallbackProcessingStatus(processingStatus)
	if objectSize.Valid {
		value := objectSize.Int64
		callback.CallbackObject.SizeBytes = &value
	}
	return callback, err
}

func deliveryTargetFromPG(requestID pgtype.UUID, notificationID pgtype.UUID) value.DeliveryTarget {
	if requestID.Valid {
		return value.DeliveryTarget{Kind: value.DeliveryTargetKindRequest, ID: uuid.UUID(requestID.Bytes)}
	}
	if notificationID.Valid {
		return value.DeliveryTarget{Kind: value.DeliveryTargetKindNotification, ID: uuid.UUID(notificationID.Bytes)}
	}
	return value.DeliveryTarget{}
}

func uuidFromPG(input pgtype.UUID) *uuid.UUID {
	if !input.Valid {
		return nil
	}
	value := uuid.UUID(input.Bytes)
	return &value
}

func textFromPG(input pgtype.Text) string {
	if !input.Valid {
		return ""
	}
	return input.String
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	var operation string
	var resultPayload []byte
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.ActorRef,
		&operation,
		&result.AggregateType,
		&result.AggregateID,
		&result.RequestFingerprint,
		&resultPayload,
		&result.CreatedAt,
	)
	if commandID.Valid {
		result.CommandID = uuid.UUID(commandID.Bytes)
	}
	result.Operation = enum.Operation(operation)
	result.ResultPayload = append(result.ResultPayload[:0], resultPayload...)
	return result, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	raw, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	event := entity.OutboxEvent{}
	event.ID = raw.Identity.RowID
	event.EventType = raw.Identity.TypeName
	event.SchemaVersion = raw.Identity.ContractVersion
	event.AggregateType = raw.Identity.SubjectKind
	event.AggregateID = raw.Identity.SubjectID
	event.Payload = raw.Body
	event.OccurredAt = raw.Identity.CreatedAt
	event.AttemptCount = raw.Delivery.Attempts
	event.PublishedAt = raw.Delivery.SentAt
	event.NextAttemptAt = raw.Delivery.RetryAt
	event.LockedUntil = raw.Delivery.LeaseUntil
	event.FailedPermanentlyAt = raw.Failure.DeadAt
	event.FailureKind = raw.Failure.FailureCode
	event.LastError = raw.Failure.ErrorText
	return event, nil
}

func unmarshalArray[T any](input []byte) ([]T, error) {
	if len(input) == 0 || string(input) == "null" {
		return []T{}, nil
	}
	var result []T
	if err := json.Unmarshal(input, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return []T{}, nil
	}
	return result, nil
}

func jsonString(input []byte) string {
	if len(input) == 0 || string(input) == "null" {
		return "{}"
	}
	return string(input)
}
