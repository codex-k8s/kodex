package provider

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

func scanAccountRuntimeState(row postgreslib.RowScanner) (entity.ProviderAccountRuntimeState, error) {
	var state entity.ProviderAccountRuntimeState
	var providerSlug, status string
	var lastCheckedAt, lastSuccessAt pgtype.Timestamptz
	err := row.Scan(
		&state.ID,
		&state.ExternalAccountID,
		&providerSlug,
		&status,
		&lastCheckedAt,
		&lastSuccessAt,
		&state.LastErrorCode,
		&state.LastErrorMessage,
		&state.Version,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	state.ProviderSlug = enum.ProviderSlug(providerSlug)
	state.Status = enum.ProviderAccountRuntimeStatus(status)
	state.LastCheckedAt = timePtrFromPG(lastCheckedAt)
	state.LastSuccessAt = timePtrFromPG(lastSuccessAt)
	return state, err
}

func scanWebhookEvent(row postgreslib.RowScanner) (entity.WebhookEvent, error) {
	var event entity.WebhookEvent
	var providerSlug, status string
	var payload []byte
	err := row.Scan(
		&event.ID,
		&providerSlug,
		&event.DeliveryID,
		&event.EventName,
		&event.RepositoryProviderID,
		&event.ReceivedAt,
		&status,
		&payload,
		&event.LastError,
		&event.RetainUntil,
	)
	event.ProviderSlug = enum.ProviderSlug(providerSlug)
	event.ProcessingStatus = enum.WebhookProcessingStatus(status)
	event.PayloadJSON = append(event.PayloadJSON[:0], payload...)
	return event, err
}

func scanProviderEvent(row postgreslib.RowScanner) (entity.ProviderEvent, error) {
	var event entity.ProviderEvent
	var sourceWebhookEventID pgtype.UUID
	var payload []byte
	err := row.Scan(
		&event.ID,
		&sourceWebhookEventID,
		&event.EventType,
		&event.AggregateType,
		&event.AggregateID,
		&payload,
		&event.OccurredAt,
	)
	event.SourceWebhookEventID = postgreslib.UUIDPtrFromPG(sourceWebhookEventID)
	event.PayloadJSON = append(event.PayloadJSON[:0], payload...)
	return event, err
}

func scanWorkItemProjection(row postgreslib.RowScanner) (entity.ProviderWorkItemProjection, error) {
	var projection entity.ProviderWorkItemProjection
	var providerSlug, kind, watermarkStatus, driftStatus string
	var projectID, repositoryID pgtype.UUID
	var labels, assignees, projectFields, watermark []byte
	var providerUpdatedAt pgtype.Timestamptz
	err := row.Scan(
		&projection.ID,
		&providerSlug,
		&projection.ProviderWorkItemID,
		&projectID,
		&repositoryID,
		&projection.RepositoryFullName,
		&kind,
		&projection.Number,
		&projection.URL,
		&projection.Title,
		&projection.State,
		&projection.WorkItemType,
		&labels,
		&assignees,
		&projection.Milestone,
		&projectFields,
		&watermarkStatus,
		&watermark,
		&projection.BodyDigest,
		&providerUpdatedAt,
		&projection.SyncedAt,
		&driftStatus,
		&projection.Version,
		&projection.CreatedAt,
		&projection.UpdatedAt,
	)
	projection.ProviderSlug = enum.ProviderSlug(providerSlug)
	projection.ProjectID = postgreslib.UUIDPtrFromPG(projectID)
	projection.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	projection.Kind = enum.WorkItemKind(kind)
	projection.LabelsJSON = append(projection.LabelsJSON[:0], labels...)
	projection.AssigneesJSON = append(projection.AssigneesJSON[:0], assignees...)
	projection.ProjectFieldsJSON = append(projection.ProjectFieldsJSON[:0], projectFields...)
	projection.WatermarkStatus = enum.WorkItemWatermarkStatus(watermarkStatus)
	projection.WatermarkJSON = append(projection.WatermarkJSON[:0], watermark...)
	projection.ProviderUpdatedAt = timePtrFromPG(providerUpdatedAt)
	projection.DriftStatus = enum.WorkItemDriftStatus(driftStatus)
	return projection, err
}

func scanCommentProjection(row postgreslib.RowScanner) (entity.ProviderCommentProjection, error) {
	var comment entity.ProviderCommentProjection
	var kind, reviewState string
	var providerCreatedAt, providerUpdatedAt pgtype.Timestamptz
	err := row.Scan(
		&comment.ID,
		&comment.WorkItemProjectionID,
		&comment.ProviderCommentID,
		&kind,
		&reviewState,
		&comment.AuthorProviderLogin,
		&comment.BodyDigest,
		&comment.Summary,
		&providerCreatedAt,
		&providerUpdatedAt,
		&comment.Version,
		&comment.CreatedAt,
		&comment.UpdatedAt,
	)
	comment.Kind = enum.CommentKind(kind)
	comment.ReviewState = enum.ReviewState(reviewState)
	comment.ProviderCreatedAt = timePtrFromPG(providerCreatedAt)
	comment.ProviderUpdatedAt = timePtrFromPG(providerUpdatedAt)
	return comment, err
}

func scanRelationship(row postgreslib.RowScanner) (entity.ProviderRelationship, error) {
	var relationship entity.ProviderRelationship
	var targetID pgtype.UUID
	var source, confidence string
	err := row.Scan(
		&relationship.ID,
		&relationship.SourceWorkItemID,
		&targetID,
		&relationship.TargetProviderRef,
		&relationship.RelationshipType,
		&source,
		&confidence,
		&relationship.Version,
		&relationship.CreatedAt,
	)
	relationship.TargetWorkItemID = postgreslib.UUIDPtrFromPG(targetID)
	relationship.Source = enum.RelationshipSource(source)
	relationship.Confidence = enum.RelationshipConfidence(confidence)
	return relationship, err
}

func scanSyncCursor(row postgreslib.RowScanner) (entity.SyncCursor, error) {
	var cursor entity.SyncCursor
	var providerSlug, scopeType, artifactKind, priority string
	var overlapSince, lastSuccessAt, lastCheckedAt, leaseUntil pgtype.Timestamptz
	var budget []byte
	err := row.Scan(
		&cursor.ID,
		&providerSlug,
		&cursor.ExternalAccountID,
		&scopeType,
		&cursor.ScopeRef,
		&artifactKind,
		&cursor.CursorValue,
		&overlapSince,
		&priority,
		&lastSuccessAt,
		&lastCheckedAt,
		&cursor.LastError,
		&budget,
		&cursor.LeaseOwner,
		&leaseUntil,
		&cursor.Version,
		&cursor.CreatedAt,
		&cursor.UpdatedAt,
	)
	cursor.ProviderSlug = enum.ProviderSlug(providerSlug)
	cursor.ScopeType = enum.SyncCursorScopeType(scopeType)
	cursor.ArtifactKind = enum.SyncArtifactKind(artifactKind)
	cursor.OverlapSince = timePtrFromPG(overlapSince)
	cursor.Priority = enum.SyncCursorPriority(priority)
	cursor.LastSuccessAt = timePtrFromPG(lastSuccessAt)
	cursor.LastCheckedAt = timePtrFromPG(lastCheckedAt)
	cursor.RateBudgetStateJSON = append(cursor.RateBudgetStateJSON[:0], budget...)
	cursor.LeaseUntil = timePtrFromPG(leaseUntil)
	return cursor, err
}

func scanReconciliationRequest(row postgreslib.RowScanner) (entity.ReconciliationRequest, error) {
	var request entity.ReconciliationRequest
	var providerSlug, scopeType, priority string
	var artifactKinds []byte
	if err := row.Scan(
		&request.ID,
		&providerSlug,
		&request.ExternalAccountID,
		&scopeType,
		&request.ScopeRef,
		&request.IdempotencyKey,
		&artifactKinds,
		&priority,
		&request.CreatedAt,
		&request.UpdatedAt,
	); err != nil {
		return request, err
	}
	request.ProviderSlug = enum.ProviderSlug(providerSlug)
	request.ScopeType = enum.SyncCursorScopeType(scopeType)
	request.Priority = enum.SyncCursorPriority(priority)
	kinds, err := scanArtifactKinds(artifactKinds)
	request.ArtifactKinds = kinds
	return request, err
}

func scanProviderArtifactSignal(row postgreslib.RowScanner) (entity.ProviderArtifactSignal, error) {
	var signal entity.ProviderArtifactSignal
	var providerSlug, scopeType string
	var artifactKinds, targetJSON, payloadJSON []byte
	if err := row.Scan(
		&signal.ID,
		&signal.IdentityKey,
		&providerSlug,
		&signal.ExternalAccountID,
		&signal.Source,
		&scopeType,
		&signal.ScopeRef,
		&artifactKinds,
		&targetJSON,
		&payloadJSON,
		&signal.ObservedAt,
		&signal.CreatedAt,
	); err != nil {
		return signal, err
	}
	kinds, err := scanArtifactKinds(artifactKinds)
	signal.ProviderSlug = enum.ProviderSlug(providerSlug)
	signal.ScopeType = enum.SyncCursorScopeType(scopeType)
	signal.ArtifactKinds = kinds
	signal.TargetJSON = append(signal.TargetJSON[:0], targetJSON...)
	signal.PayloadJSON = append(signal.PayloadJSON[:0], payloadJSON...)
	return signal, err
}

func scanLimitSnapshot(row postgreslib.RowScanner) (entity.ProviderLimitSnapshot, error) {
	var snapshot entity.ProviderLimitSnapshot
	var providerSlug, source string
	var remaining, limitValue pgtype.Int8
	var resetAt pgtype.Timestamptz
	err := row.Scan(
		&snapshot.ID,
		&snapshot.ExternalAccountID,
		&providerSlug,
		&snapshot.LimitClass,
		&remaining,
		&limitValue,
		&resetAt,
		&snapshot.CapturedAt,
		&source,
	)
	snapshot.ProviderSlug = enum.ProviderSlug(providerSlug)
	snapshot.Remaining = int64PtrFromPG(remaining)
	snapshot.LimitValue = int64PtrFromPG(limitValue)
	snapshot.ResetAt = timePtrFromPG(resetAt)
	snapshot.Source = enum.ProviderLimitSource(source)
	return snapshot, err
}

func scanArtifactKinds(payload []byte) ([]enum.SyncArtifactKind, error) {
	var raw []string
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	result := make([]enum.SyncArtifactKind, 0, len(raw))
	for _, value := range raw {
		result = append(result, enum.SyncArtifactKind(value))
	}
	return result, nil
}

func scanProviderOperation(row postgreslib.RowScanner) (entity.ProviderOperation, error) {
	var operation entity.ProviderOperation
	var providerSlug, operationType, status string
	var actorID, snapshotID pgtype.UUID
	var finishedAt pgtype.Timestamptz
	var policyJSON, approvalJSON []byte
	err := row.Scan(
		&operation.ID,
		&operation.CommandID,
		&actorID,
		&operation.ExternalAccountID,
		&providerSlug,
		&operationType,
		&operation.TargetRef,
		&status,
		&operation.ResultRef,
		&operation.ProviderObjectID,
		&operation.ErrorCode,
		&operation.ErrorMessage,
		&snapshotID,
		&policyJSON,
		&approvalJSON,
		&operation.ProviderVersion,
		&operation.BaseBranch,
		&operation.StartedAt,
		&finishedAt,
		&operation.Version,
		&operation.CreatedAt,
		&operation.UpdatedAt,
	)
	operation.ActorID = postgreslib.UUIDPtrFromPG(actorID)
	operation.ProviderSlug = enum.ProviderSlug(providerSlug)
	operation.OperationType = enum.ProviderOperationType(operationType)
	operation.Status = enum.ProviderOperationStatus(status)
	operation.RateLimitSnapshotID = postgreslib.UUIDPtrFromPG(snapshotID)
	operation.FinishedAt = timePtrFromPG(finishedAt)
	if len(policyJSON) == 0 {
		policyJSON = []byte("{}")
	}
	if len(approvalJSON) == 0 {
		approvalJSON = []byte("{}")
	}
	if err := json.Unmarshal(policyJSON, &operation.OperationPolicyContext); err != nil {
		return entity.ProviderOperation{}, err
	}
	if err := json.Unmarshal(approvalJSON, &operation.ApprovalGateRef); err != nil {
		return entity.ProviderOperation{}, err
	}
	return operation, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	scanned, err := postgreslib.ScanOutboxEventRow(row)
	event := outboxlib.NewEvent(
		scanned.Identity.RowID,
		scanned.Identity.TypeName,
		scanned.Identity.ContractVersion,
		scanned.Identity.SubjectKind,
		scanned.Identity.SubjectID,
		scanned.Body,
		scanned.Identity.CreatedAt,
		scanned.Delivery.Attempts,
	)
	delivery := outboxlib.RecordDelivery{
		PublishedAt:   scanned.Delivery.SentAt,
		AttemptCount:  scanned.Delivery.Attempts,
		NextAttemptAt: scanned.Delivery.RetryAt,
		LockedUntil:   scanned.Delivery.LeaseUntil,
	}
	failure := outboxlib.RecordFailure{
		FailedPermanentlyAt: scanned.Failure.DeadAt,
		FailureKind:         scanned.Failure.FailureCode,
		LastError:           scanned.Failure.ErrorText,
	}
	return outboxlib.RecordFromParts(event, delivery, failure), err
}

func timePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	converted := value.Time.UTC()
	return &converted
}

func int64PtrFromPG(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	converted := value.Int64
	return &converted
}
