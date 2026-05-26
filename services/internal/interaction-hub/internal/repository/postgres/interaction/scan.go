package interaction

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
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

func deliveryTargetFromPG(requestID pgtype.UUID, notificationID pgtype.UUID) value.DeliveryTarget {
	if requestID.Valid {
		return value.DeliveryTarget{Kind: value.DeliveryTargetKindRequest, ID: uuid.UUID(requestID.Bytes)}
	}
	if notificationID.Valid {
		return value.DeliveryTarget{Kind: value.DeliveryTargetKindNotification, ID: uuid.UUID(notificationID.Bytes)}
	}
	return value.DeliveryTarget{}
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
	return entity.OutboxEvent{
		Event: outboxlib.Event{
			ID:            raw.Identity.RowID,
			EventType:     raw.Identity.TypeName,
			SchemaVersion: raw.Identity.ContractVersion,
			AggregateType: raw.Identity.SubjectKind,
			AggregateID:   raw.Identity.SubjectID,
			Payload:       raw.Body,
			OccurredAt:    raw.Identity.CreatedAt,
			AttemptCount:  raw.Delivery.Attempts,
		},
		PublishedAt:         raw.Delivery.SentAt,
		NextAttemptAt:       raw.Delivery.RetryAt,
		LockedUntil:         raw.Delivery.LeaseUntil,
		FailedPermanentlyAt: raw.Failure.DeadAt,
		FailureKind:         raw.Failure.FailureCode,
		LastError:           raw.Failure.ErrorText,
	}, nil
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
