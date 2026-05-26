package interaction

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

type pageQueryArgs struct {
	pgx.NamedArgs
	PageSize   int32
	NextOffset int32
}

func threadArgs(thread entity.ConversationThread) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                thread.ID,
		"scope_type":        string(thread.Scope.Type),
		"scope_ref":         thread.Scope.Ref,
		"thread_kind":       string(thread.ThreadKind),
		"primary_actor_ref": thread.PrimaryActorRef,
		"source_kind":       string(thread.SourceKind),
		"source_ref":        thread.SourceRef,
		"status":            string(thread.Status),
		"latest_message_id": postgreslib.NullableUUID(thread.LatestMessageID),
		"correlation_id":    thread.CorrelationID,
		"retention_class":   thread.RetentionClass,
		"version":           thread.Version,
		"created_at":        thread.CreatedAt,
		"updated_at":        thread.UpdatedAt,
		"closed_at":         postgreslib.NullableTime(thread.ClosedAt),
	}
}

func threadLatestMessageArgs(thread entity.ConversationThread, previousVersion int64) pgx.NamedArgs {
	args := threadArgs(thread)
	args["previous_version"] = previousVersion
	return args
}

func messageArgs(message entity.ConversationMessage) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                     message.ID,
		"thread_id":              message.ThreadID,
		"message_kind":           string(message.MessageKind),
		"author_ref":             message.AuthorRef,
		"body_summary":           message.BodySummary,
		"body_object_uri":        message.BodyObject.URI,
		"body_object_digest":     message.BodyObject.Digest,
		"body_object_size_bytes": nullableInt64(message.BodyObject.SizeBytes),
		"body_digest":            message.BodyDigest,
		"locale":                 message.Locale,
		"safe_metadata":          objectPayload(message.SafeMetadata),
		"created_at":             message.CreatedAt,
	}
}

func messageFilterArgs(filter query.ConversationMessageFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"thread_id": filter.ThreadID,
	})
}

func requestArgs(request entity.InteractionRequest) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                          request.ID,
		"request_kind":                string(request.RequestKind),
		"scope_type":                  string(request.Scope.Type),
		"scope_ref":                   request.Scope.Ref,
		"thread_id":                   postgreslib.NullableUUID(request.ThreadID),
		"source_owner_kind":           string(request.SourceOwner.Kind),
		"source_owner_ref":            request.SourceOwner.Ref,
		"ingress_kind":                string(request.Ingress.Kind),
		"ingress_ref":                 request.Ingress.Ref,
		"decision_owner_kind":         string(request.DecisionOwner.Kind),
		"decision_owner_request_ref":  request.DecisionOwner.OwnerRequestRef,
		"decision_owner_decision_ref": request.DecisionOwner.OwnerDecisionRef,
		"target_refs":                 arrayPayload(request.TargetRefs),
		"context_refs":                arrayPayload(request.ContextRefs),
		"prompt_summary":              request.PromptSummary,
		"prompt_object_uri":           request.PromptObject.URI,
		"prompt_object_digest":        request.PromptObject.Digest,
		"prompt_object_size_bytes":    nullableInt64(request.PromptObject.SizeBytes),
		"allowed_actions":             arrayPayload(request.AllowedActions),
		"risk_class":                  string(request.RiskClass),
		"status":                      string(request.Status),
		"deadline_at":                 postgreslib.NullableTime(request.DeadlineAt),
		"reminder_policy_ref":         request.ReminderPolicyRef,
		"version":                     request.Version,
		"created_at":                  request.CreatedAt,
		"updated_at":                  request.UpdatedAt,
		"resolved_at":                 postgreslib.NullableTime(request.ResolvedAt),
	}
}

func requestUpdateStatusArgs(request entity.InteractionRequest, previousVersion int64) pgx.NamedArgs {
	args := requestArgs(request)
	args["previous_version"] = previousVersion
	return args
}

func requestFilterArgs(filter query.InteractionRequestFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_type":        string(filter.Scope.Type),
		"scope_ref":         filter.Scope.Ref,
		"request_kind":      string(filter.RequestKind),
		"status":            string(filter.Status),
		"source_owner_kind": string(filter.SourceOwnerKind),
		"source_owner_ref":  filter.SourceOwnerRef,
		"deadline_before":   postgreslib.NullableTime(filter.DeadlineBefore),
	})
}

func expirableRequestArgs(scope value.ScopeRef, deadlineBefore time.Time, limit int32) pgx.NamedArgs {
	return pgx.NamedArgs{
		"scope_type":      string(scope.Type),
		"scope_ref":       scope.Ref,
		"deadline_before": deadlineBefore,
		"limit":           limit,
	}
}

func responseArgs(response entity.InteractionResponse) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                         response.ID,
		"request_id":                 response.RequestID,
		"response_action":            string(response.ResponseAction),
		"responded_by_actor_ref":     response.RespondedByActorRef,
		"response_summary":           response.ResponseSummary,
		"response_object_uri":        response.ResponseObject.URI,
		"response_object_digest":     response.ResponseObject.Digest,
		"response_object_size_bytes": nullableInt64(response.ResponseObject.SizeBytes),
		"source_kind":                string(response.SourceKind),
		"source_ref":                 response.SourceRef,
		"owner_decision_ref":         response.OwnerDecisionRef,
		"created_at":                 response.CreatedAt,
	}
}

func notificationArgs(notification entity.Notification) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                      notification.ID,
		"scope_type":              string(notification.Scope.Type),
		"scope_ref":               notification.Scope.Ref,
		"notification_kind":       string(notification.NotificationKind),
		"request_id":              postgreslib.NullableUUID(notification.RequestID),
		"subscription_id":         postgreslib.NullableUUID(notification.SubscriptionID),
		"recipient_refs":          arrayPayload(notification.RecipientRefs),
		"message_template_ref":    notification.MessageTemplateRef,
		"message_summary":         notification.MessageSummary,
		"priority":                string(notification.Priority),
		"status":                  string(notification.Status),
		"created_at":              notification.CreatedAt,
		"updated_at":              notification.UpdatedAt,
		"expires_at":              postgreslib.NullableTime(notification.ExpiresAt),
		"source_owner_kind":       string(notification.SourceOwner.Kind),
		"source_owner_ref":        notification.SourceOwner.Ref,
		"ingress_kind":            string(notification.Ingress.Kind),
		"ingress_ref":             notification.Ingress.Ref,
		"context_refs":            arrayPayload(notification.ContextRefs),
		"channel_hint_refs":       arrayPayload(notification.ChannelHintRefs),
		"notification_policy_ref": notification.NotificationPolicyRef,
		"message_title":           notification.MessageTitle,
		"body_preview":            notification.BodyPreview,
	}
}

func subscriptionArgs(subscription entity.Subscription) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                      subscription.ID,
		"scope_type":              string(subscription.Scope.Type),
		"scope_ref":               subscription.Scope.Ref,
		"subscriber_ref_kind":     subscription.SubscriberRef.Kind,
		"subscriber_ref":          subscription.SubscriberRef.Ref,
		"event_filter":            postgreslib.JSONPayload([]byte(subscription.EventFilterJSON)),
		"delivery_preferences":    postgreslib.JSONPayload([]byte(subscription.DeliveryPreferencesJSON)),
		"status":                  string(subscription.Status),
		"version":                 subscription.Version,
		"created_at":              subscription.CreatedAt,
		"updated_at":              subscription.UpdatedAt,
		"source_owner_kind":       string(subscription.SourceOwner.Kind),
		"source_owner_ref":        subscription.SourceOwner.Ref,
		"channel_hint_refs":       arrayPayload(subscription.ChannelHintRefs),
		"subscription_policy_ref": subscription.SubscriptionPolicyRef,
	}
}

func subscriptionUpdateArgs(subscription entity.Subscription, previousVersion int64) pgx.NamedArgs {
	args := subscriptionArgs(subscription)
	args["previous_version"] = previousVersion
	return args
}

func subscriptionFilterArgs(filter query.SubscriptionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_type":     string(filter.Scope.Type),
		"scope_ref":      filter.Scope.Ref,
		"subscriber_ref": filter.SubscriberRef,
		"status":         string(filter.Status),
	})
}

func deliveryAttemptArgs(attempt entity.DeliveryAttempt) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  attempt.ID,
		"request_id":          postgreslib.NullableUUID(deliveryTargetID(attempt.Target, value.DeliveryTargetKindRequest)),
		"notification_id":     postgreslib.NullableUUID(deliveryTargetID(attempt.Target, value.DeliveryTargetKindNotification)),
		"route_id":            attempt.RouteID,
		"delivery_id":         attempt.DeliveryID,
		"delivery_kind":       string(attempt.DeliveryKind),
		"status":              string(attempt.Status),
		"channel_message_ref": attempt.ChannelMessageRef,
		"attempt_number":      attempt.AttemptNumber,
		"next_retry_at":       postgreslib.NullableTime(attempt.NextRetryAt),
		"error_code":          attempt.ErrorCode,
		"error_class":         string(attempt.ErrorClass),
		"payload_digest":      attempt.PayloadDigest,
		"created_at":          attempt.CreatedAt,
		"updated_at":          attempt.UpdatedAt,
		"sent_at":             postgreslib.NullableTime(attempt.SentAt),
	}
}

func deliveryTargetID(target value.DeliveryTarget, kind value.DeliveryTargetKind) *uuid.UUID {
	if target.Kind != kind || target.ID == uuid.Nil {
		return nil
	}
	id := target.ID
	return &id
}

func deliveryAttemptFilterArgs(filter query.DeliveryAttemptFilter) pgx.NamedArgs {
	return pgx.NamedArgs{
		"request_id":      postgreslib.NullableUUID(deliveryTargetID(filter.Target, value.DeliveryTargetKindRequest)),
		"notification_id": postgreslib.NullableUUID(deliveryTargetID(filter.Target, value.DeliveryTargetKindNotification)),
		"delivery_id":     filter.DeliveryID,
		"limit":           filter.Limit,
	}
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	return pgx.NamedArgs{
		"key":                 result.Key,
		"command_id":          postgreslib.NullableCommandID(result.CommandID),
		"idempotency_key":     result.IdempotencyKey,
		"actor_ref":           result.ActorRef,
		"operation":           string(result.Operation),
		"aggregate_type":      result.AggregateType,
		"aggregate_id":        result.AggregateID,
		"request_fingerprint": result.RequestFingerprint,
		"result_payload":      postgreslib.JSONPayload(result.ResultPayload),
		"created_at":          result.CreatedAt,
	}
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"command_id":      postgreslib.NullableCommandID(identity.CommandID),
		"idempotency_key": identity.IdempotencyKey,
		"actor_ref":       identity.ActorRef,
		"operation":       string(identity.Operation),
	}
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	return postgreslib.OutboxCreateArgs(
		event.ID,
		event.EventType,
		event.SchemaVersion,
		event.AggregateType,
		event.AggregateID,
		event.Payload,
		event.OccurredAt,
		event.PublishedAt,
	)
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	pageSize, offset, nextOffset := postgreslib.OffsetPageBounds(page.PageSize, page.PageToken, defaultPageSize, maxPageSize)
	args["limit"] = pageSize + 1
	args["offset"] = offset
	return pageQueryArgs{NamedArgs: args, PageSize: pageSize, NextOffset: nextOffset}
}

func pageFromItems[T any](items []T, args pageQueryArgs) ([]T, value.PageResult) {
	trimmed, token := postgreslib.TrimOffsetPage(items, args.PageSize, args.NextOffset)
	return trimmed, value.PageResult{NextPageToken: token}
}

func objectPayload(value any) string {
	return jsonPayload(value, "{}")
}

func arrayPayload(value any) string {
	return jsonPayload(value, "[]")
}

func jsonPayload(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	payload, err := json.Marshal(value)
	if err != nil || string(payload) == "null" {
		return fallback
	}
	return string(payload)
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
