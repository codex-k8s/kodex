package interaction

import (
	"encoding/json"
	"time"

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
