package provider

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

type pageQueryArgs struct {
	args       pgx.NamedArgs
	limit      int32
	nextOffset int32
}

func allRowsArgs(args pgx.NamedArgs, expectedRows int) pageQueryArgs {
	limit := int32(expectedRows)
	return pageQueryArgs{args: args, limit: limit, nextOffset: limit}
}

func accountRuntimeStateArgs(state entity.ProviderAccountRuntimeState) pgx.NamedArgs {
	return withBaseArgs(state.Base, pgx.NamedArgs{
		"external_account_id": state.ExternalAccountID,
		"provider_slug":       string(state.ProviderSlug),
		"status":              string(state.Status),
		"last_checked_at":     postgreslib.NullableTime(state.LastCheckedAt),
		"last_success_at":     postgreslib.NullableTime(state.LastSuccessAt),
		"last_error_code":     state.LastErrorCode,
		"last_error_message":  state.LastErrorMessage,
	})
}

func accountRuntimeStateLookupArgs(lookup query.AccountRuntimeStateLookup) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  postgreslib.NullableUUID(lookup.ID),
		"external_account_id": postgreslib.NullableUUID(lookup.ExternalAccountID),
		"provider_slug":       string(lookup.ProviderSlug),
	}
}

func accountRuntimeStateFilterArgs(filter query.AccountRuntimeStateFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"provider_slug":        string(filter.ProviderSlug),
		"external_account_ids": postgreslib.UUIDValues(filter.ExternalAccountIDs),
		"statuses":             postgreslib.StringValues(filter.Statuses),
	})
}

func webhookEventArgs(event entity.WebhookEvent) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                     event.ID,
		"provider_slug":          string(event.ProviderSlug),
		"delivery_id":            event.DeliveryID,
		"event_name":             event.EventName,
		"repository_provider_id": event.RepositoryProviderID,
		"received_at":            event.ReceivedAt,
		"processing_status":      string(event.ProcessingStatus),
		"payload_json":           postgreslib.JSONPayload(event.PayloadJSON),
		"last_error":             event.LastError,
		"retain_until":           event.RetainUntil,
	}
}

func webhookEventIdentityArgs(event entity.WebhookEvent) pgx.NamedArgs {
	return pgx.NamedArgs{
		"provider_slug": string(event.ProviderSlug),
		"delivery_id":   event.DeliveryID,
	}
}

func webhookEventFilterArgs(filter query.WebhookEventFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"provider_slug":          string(filter.ProviderSlug),
		"delivery_id":            filter.DeliveryID,
		"event_names":            textValues(filter.EventNames),
		"processing_statuses":    postgreslib.StringValues(filter.ProcessingStatuses),
		"repository_provider_id": filter.RepositoryProviderID,
		"received_since":         postgreslib.NullableTime(filter.ReceivedSince),
		"received_until":         postgreslib.NullableTime(filter.ReceivedUntil),
	})
}

func webhookEventProcessingArgs(event entity.WebhookEvent) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                event.ID,
		"processing_status": string(event.ProcessingStatus),
		"last_error":        event.LastError,
	}
}

func providerEventArgs(event entity.ProviderEvent) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                      event.ID,
		"source_webhook_event_id": postgreslib.NullableUUID(event.SourceWebhookEventID),
		"event_type":              event.EventType,
		"aggregate_type":          event.AggregateType,
		"aggregate_id":            event.AggregateID,
		"payload_json":            postgreslib.JSONPayload(event.PayloadJSON),
		"occurred_at":             event.OccurredAt,
	}
}

func providerEventFilterArgs(filter query.ProviderEventFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"source_webhook_event_id": postgreslib.NullableUUID(filter.SourceWebhookEventID),
	})
}

func workItemProjectionArgs(projection entity.ProviderWorkItemProjection) pgx.NamedArgs {
	return withBaseArgs(projection.Base, pgx.NamedArgs{
		"provider_slug":         string(projection.ProviderSlug),
		"provider_work_item_id": projection.ProviderWorkItemID,
		"project_id":            postgreslib.NullableUUID(projection.ProjectID),
		"repository_id":         postgreslib.NullableUUID(projection.RepositoryID),
		"repository_full_name":  projection.RepositoryFullName,
		"kind":                  string(projection.Kind),
		"number":                projection.Number,
		"url":                   projection.URL,
		"title":                 projection.Title,
		"state":                 projection.State,
		"work_item_type":        projection.WorkItemType,
		"labels_json":           jsonPayloadOrDefault(projection.LabelsJSON, "[]"),
		"assignees_json":        jsonPayloadOrDefault(projection.AssigneesJSON, "[]"),
		"milestone":             projection.Milestone,
		"project_fields_json":   jsonPayloadOrDefault(projection.ProjectFieldsJSON, "{}"),
		"watermark_status":      string(projection.WatermarkStatus),
		"watermark_json":        jsonPayloadOrDefault(projection.WatermarkJSON, "{}"),
		"body_digest":           projection.BodyDigest,
		"provider_updated_at":   postgreslib.NullableTime(projection.ProviderUpdatedAt),
		"synced_at":             projection.SyncedAt,
		"drift_status":          string(projection.DriftStatus),
	})
}

func workItemProjectionLookupArgs(lookup query.ProviderTargetLookup) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                   postgreslib.NullableUUID(lookup.ID),
		"provider_slug":        string(lookup.ProviderSlug),
		"repository_full_name": lookup.RepositoryFullName,
		"kind":                 string(lookup.Kind),
		"number":               lookup.Number,
		"provider_object_id":   lookup.ProviderObjectID,
		"web_url":              lookup.WebURL,
	}
}

func workItemProjectionFilterArgs(filter query.WorkItemProjectionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_id":           postgreslib.NullableUUID(filter.ProjectID),
		"repository_id":        postgreslib.NullableUUID(filter.RepositoryID),
		"provider_slug":        string(filter.ProviderSlug),
		"repository_full_name": filter.RepositoryFullName,
		"kinds":                postgreslib.StringValues(filter.Kinds),
		"states":               textValues(filter.States),
		"labels":               textValues(filter.Labels),
		"work_item_types":      textValues(filter.WorkItemTypes),
		"drift_statuses":       postgreslib.StringValues(filter.DriftStatuses),
		"updated_since":        postgreslib.NullableTime(filter.UpdatedSince),
	})
}

func commentProjectionArgs(comment entity.ProviderCommentProjection) pgx.NamedArgs {
	return withBaseArgs(comment.Base, pgx.NamedArgs{
		"work_item_projection_id": comment.WorkItemProjectionID,
		"provider_comment_id":     comment.ProviderCommentID,
		"kind":                    string(comment.Kind),
		"review_state":            string(comment.ReviewState),
		"author_provider_login":   comment.AuthorProviderLogin,
		"body_digest":             comment.BodyDigest,
		"summary":                 comment.Summary,
		"provider_created_at":     postgreslib.NullableTime(comment.ProviderCreatedAt),
		"provider_updated_at":     postgreslib.NullableTime(comment.ProviderUpdatedAt),
	})
}

func commentProjectionFilterArgs(filter query.CommentProjectionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"work_item_projection_id": filter.WorkItemProjectionID,
		"kinds":                   postgreslib.StringValues(filter.Kinds),
	})
}

func commentProjectionLookupArgs(workItemProjectionID uuid.UUID, providerCommentID string) pgx.NamedArgs {
	return pgx.NamedArgs{
		"work_item_projection_id": workItemProjectionID,
		"provider_comment_id":     providerCommentID,
	}
}

func relationshipArgs(relationship entity.ProviderRelationship) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  relationship.ID,
		"source_work_item_id": relationship.SourceWorkItemID,
		"target_work_item_id": postgreslib.NullableUUID(relationship.TargetWorkItemID),
		"target_provider_ref": relationship.TargetProviderRef,
		"relationship_type":   relationship.RelationshipType,
		"source":              string(relationship.Source),
		"confidence":          string(relationship.Confidence),
		"created_at":          relationship.CreatedAt,
	}
}

func watermarkRelationshipCleanupArgs(sourceWorkItemID uuid.UUID, relationshipIDs []uuid.UUID) pgx.NamedArgs {
	return pgx.NamedArgs{
		"source_work_item_id": sourceWorkItemID,
		"relationship_types":  textValues([]string{"source", "parent", "next"}),
		"relationship_ids":    postgreslib.UUIDValues(relationshipIDs),
	}
}

func relationshipFilterArgs(filter query.RelationshipFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"work_item_projection_id": postgreslib.NullableUUID(filter.WorkItemProjectionID),
		"relationship_types":      textValues(filter.RelationshipTypes),
		"sources":                 postgreslib.StringValues(filter.Sources),
		"confidence_levels":       postgreslib.StringValues(filter.ConfidenceLevels),
	})
}

func reconciliationRequestArgs(request entity.ReconciliationRequest) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  request.ID,
		"provider_slug":       string(request.ProviderSlug),
		"external_account_id": request.ExternalAccountID,
		"scope_type":          string(request.ScopeType),
		"scope_ref":           request.ScopeRef,
		"idempotency_key":     request.IdempotencyKey,
		"artifact_kinds_json": artifactKindsJSON(request.ArtifactKinds),
		"priority":            string(request.Priority),
		"created_at":          request.CreatedAt,
		"updated_at":          request.UpdatedAt,
	}
}

func syncCursorsBatchArgs(cursors []entity.SyncCursor) pgx.NamedArgs {
	ids := make([]uuid.UUID, 0, len(cursors))
	artifactKinds := make([]enum.SyncArtifactKind, 0, len(cursors))
	var first entity.SyncCursor
	if len(cursors) > 0 {
		first = cursors[0]
	}
	for _, cursor := range cursors {
		ids = append(ids, cursor.ID)
		artifactKinds = append(artifactKinds, cursor.ArtifactKind)
	}
	return withBaseArgs(first.Base, pgx.NamedArgs{
		"ids":                    ids,
		"provider_slug":          string(first.ProviderSlug),
		"external_account_id":    first.ExternalAccountID,
		"scope_type":             string(first.ScopeType),
		"scope_ref":              first.ScopeRef,
		"artifact_kinds":         postgreslib.StringValues(artifactKinds),
		"cursor_value":           first.CursorValue,
		"overlap_since":          postgreslib.NullableTime(first.OverlapSince),
		"priority":               string(first.Priority),
		"last_success_at":        postgreslib.NullableTime(first.LastSuccessAt),
		"last_checked_at":        postgreslib.NullableTime(first.LastCheckedAt),
		"last_error":             first.LastError,
		"rate_budget_state_json": jsonPayloadOrDefault(first.RateBudgetStateJSON, "{}"),
		"lease_owner":            first.LeaseOwner,
		"lease_until":            postgreslib.NullableTime(first.LeaseUntil),
	})
}

func syncCursorRequestLookupArgs(request entity.ReconciliationRequest) pgx.NamedArgs {
	return pgx.NamedArgs{
		"provider_slug":       string(request.ProviderSlug),
		"external_account_id": request.ExternalAccountID,
		"scope_type":          string(request.ScopeType),
		"scope_ref":           request.ScopeRef,
		"idempotency_key":     request.IdempotencyKey,
		"artifact_kinds":      postgreslib.StringValues(request.ArtifactKinds),
	}
}

func syncCursorFilterArgs(filter query.SyncCursorFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"provider_slug":       string(filter.ProviderSlug),
		"external_account_id": postgreslib.NullableUUID(filter.ExternalAccountID),
		"scope_type":          string(filter.ScopeType),
		"scope_ref":           filter.ScopeRef,
		"artifact_kinds":      postgreslib.StringValues(filter.ArtifactKinds),
		"priorities":          postgreslib.StringValues(filter.Priorities),
		"include_healthy":     filter.IncludeHealthy,
	})
}

func syncCursorClaimArgs(claim providerrepo.SyncCursorClaim) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  postgreslib.NullableUUID(claim.ID),
		"provider_slug":       string(claim.ProviderSlug),
		"external_account_id": postgreslib.NullableUUID(claim.ExternalAccountID),
		"lease_owner":         claim.LeaseOwner,
		"now":                 claim.Now,
		"lease_until":         claim.LeaseUntil,
	}
}

func artifactKindsJSON(kinds []enum.SyncArtifactKind) string {
	values := postgreslib.StringValues(kinds)
	payload, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func limitSnapshotArgs(snapshot entity.ProviderLimitSnapshot) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  snapshot.ID,
		"external_account_id": snapshot.ExternalAccountID,
		"provider_slug":       string(snapshot.ProviderSlug),
		"limit_class":         snapshot.LimitClass,
		"remaining":           int64PtrValue(snapshot.Remaining),
		"limit_value":         int64PtrValue(snapshot.LimitValue),
		"reset_at":            postgreslib.NullableTime(snapshot.ResetAt),
		"captured_at":         snapshot.CapturedAt,
		"source":              string(snapshot.Source),
	}
}

func limitSnapshotFilterArgs(filter query.LimitSnapshotFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"external_account_id": postgreslib.NullableUUID(filter.ExternalAccountID),
		"provider_slug":       string(filter.ProviderSlug),
		"limit_classes":       textValues(filter.LimitClasses),
		"captured_since":      postgreslib.NullableTime(filter.CapturedSince),
	})
}

func providerOperationArgs(operation entity.ProviderOperation) pgx.NamedArgs {
	return withBaseArgs(operation.Base, pgx.NamedArgs{
		"command_id":             operation.CommandID,
		"actor_id":               postgreslib.NullableUUID(operation.ActorID),
		"external_account_id":    operation.ExternalAccountID,
		"provider_slug":          string(operation.ProviderSlug),
		"operation_type":         string(operation.OperationType),
		"target_ref":             operation.TargetRef,
		"status":                 string(operation.Status),
		"result_ref":             operation.ResultRef,
		"error_code":             operation.ErrorCode,
		"error_message":          operation.ErrorMessage,
		"rate_limit_snapshot_id": postgreslib.NullableUUID(operation.RateLimitSnapshotID),
		"started_at":             operation.StartedAt,
		"finished_at":            postgreslib.NullableTime(operation.FinishedAt),
	})
}

func providerOperationFilterArgs(filter query.ProviderOperationFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"provider_slug":       string(filter.ProviderSlug),
		"external_account_id": postgreslib.NullableUUID(filter.ExternalAccountID),
		"operation_types":     postgreslib.StringValues(filter.OperationTypes),
		"statuses":            postgreslib.StringValues(filter.Statuses),
		"target_ref":          filter.TargetRef,
		"started_since":       postgreslib.NullableTime(filter.StartedSince),
	})
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	return postgreslib.OutboxCreateArgs(event.ID, event.EventType, event.SchemaVersion, event.AggregateType, event.AggregateID, event.Payload, event.OccurredAt, event.PublishedAt)
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	limit, offset, nextOffset := postgreslib.OffsetPageBounds(page.PageSize, page.PageToken, defaultPageSize, maxPageSize)
	args["limit"] = limit + 1
	args["offset"] = offset
	return pageQueryArgs{args: args, limit: limit, nextOffset: nextOffset}
}

func pageResult[T any](items []T, limit int32, nextOffset int32) ([]T, value.PageResult) {
	pageItems, token := postgreslib.TrimOffsetPage(items, limit, nextOffset)
	return pageItems, value.PageResult{NextPageToken: token}
}

func withBaseArgs(base entity.Base, args pgx.NamedArgs) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(args, base.ID, base.Version, base.CreatedAt, base.UpdatedAt)
}

func int64PtrValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func textValues(values []string) []string {
	result := make([]string, 0, len(values))
	result = append(result, values...)
	return result
}

func jsonPayloadOrDefault(payload []byte, fallback string) string {
	if len(payload) == 0 {
		return fallback
	}
	return string(payload)
}
