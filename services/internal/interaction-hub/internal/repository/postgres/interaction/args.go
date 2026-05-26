package interaction

import (
	"encoding/json"

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
	if value == nil {
		return "{}"
	}
	payload, err := json.Marshal(value)
	if err != nil || string(payload) == "null" {
		return "{}"
	}
	return string(payload)
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
