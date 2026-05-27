package provider

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// Repository is the storage boundary owned by provider-hub.
//
// Business methods are added together with concrete provider workflows. The
// initial scaffold keeps only the readiness contract needed by the process.
type Repository interface {
	Ping(context.Context) error
	StoreWebhookEvent(context.Context, entity.WebhookEvent, ProjectionUpdate, []entity.ProviderEvent, []entity.OutboxEvent) (entity.WebhookEvent, []entity.ProviderEvent, error)
	ProcessWebhookEvent(context.Context, entity.WebhookEvent, ProjectionUpdate, []entity.ProviderEvent, []entity.OutboxEvent) (entity.WebhookEvent, error)
	GetWebhookEvent(context.Context, uuid.UUID) (entity.WebhookEvent, error)
	ListWebhookEvents(context.Context, query.WebhookEventFilter) ([]entity.WebhookEvent, query.PageResult, error)
	ListProviderEvents(context.Context, query.ProviderEventFilter) ([]entity.ProviderEvent, query.PageResult, error)
	GetWorkItemProjection(context.Context, query.ProviderTargetLookup) (entity.ProviderWorkItemProjection, error)
	ListWorkItemProjections(context.Context, query.WorkItemProjectionFilter) ([]entity.ProviderWorkItemProjection, query.PageResult, error)
	GetCommentProjectionByProviderID(context.Context, uuid.UUID, string) (entity.ProviderCommentProjection, error)
	ListComments(context.Context, query.CommentProjectionFilter) ([]entity.ProviderCommentProjection, query.PageResult, error)
	GetRelationshipByIdentity(context.Context, query.RelationshipLookup) (entity.ProviderRelationship, error)
	ListRelationships(context.Context, query.RelationshipFilter) ([]entity.ProviderRelationship, query.PageResult, error)
	RegisterProviderArtifactSignal(context.Context, entity.ProviderArtifactSignal, entity.ReconciliationRequest, []entity.SyncCursor) ([]entity.SyncCursor, error)
	EnqueueSyncCursors(context.Context, entity.ReconciliationRequest, []entity.SyncCursor) ([]entity.SyncCursor, error)
	GetSyncCursor(context.Context, uuid.UUID) (entity.SyncCursor, error)
	ListSyncCursors(context.Context, query.SyncCursorFilter) ([]entity.SyncCursor, query.PageResult, error)
	ClaimSyncCursor(context.Context, SyncCursorClaim) (entity.SyncCursor, error)
	ApplyReconciliationBatch(context.Context, ReconciliationBatchCompletion) (entity.SyncCursor, []entity.ProviderEvent, error)
	UpsertAccountRuntimeState(context.Context, entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error)
	GetAccountRuntimeState(context.Context, query.AccountRuntimeStateLookup) (entity.ProviderAccountRuntimeState, error)
	ListAccountRuntimeStates(context.Context, query.AccountRuntimeStateFilter) ([]entity.ProviderAccountRuntimeState, query.PageResult, error)
	RecordLimitSnapshot(context.Context, entity.ProviderLimitSnapshot, entity.ProviderAccountRuntimeState) (entity.ProviderLimitSnapshot, error)
	ListLimitSnapshots(context.Context, query.LimitSnapshotFilter) ([]entity.ProviderLimitSnapshot, query.PageResult, error)
	ApplyProviderOperation(context.Context, ProviderOperationCompletion) (entity.ProviderOperation, error)
	GetProviderOperationByCommand(context.Context, enum.ProviderOperationType, string) (entity.ProviderOperation, error)
	GetRepositoryAdoptionScanByOperation(context.Context, uuid.UUID) (entity.RepositoryAdoptionScanSnapshot, error)
	RecordProviderOperation(context.Context, entity.ProviderOperation) (entity.ProviderOperation, bool, error)
	ListProviderOperations(context.Context, query.ProviderOperationFilter) ([]entity.ProviderOperation, query.PageResult, error)
	ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error
	MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error
	MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error
}

// ProjectionUpdate stores projection changes derived from webhook, reconciliation or provider operation.
type ProjectionUpdate struct {
	WorkItem      *entity.ProviderWorkItemProjection
	Comments      []entity.ProviderCommentProjection
	Relationships []entity.ProviderRelationship
	MergeSignal   *entity.RepositoryMergeSignal
	AdoptionScan  *entity.RepositoryAdoptionScanSnapshot
}

// SyncCursorClaim identifies one cursor lease attempt for a reconciliation worker.
type SyncCursorClaim struct {
	ID                *uuid.UUID
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID *uuid.UUID
	LeaseOwner        string
	Now               time.Time
	LeaseUntil        time.Time
}

// ReconciliationBatchCompletion stores provider snapshots and advances or marks one cursor.
type ReconciliationBatchCompletion struct {
	Cursor             entity.SyncCursor
	ExpectedLeaseOwner string
	ProjectionUpdate   ProjectionUpdate
	ProviderEvents     []entity.ProviderEvent
	OutboxEvents       []entity.OutboxEvent
	LimitSnapshots     []entity.ProviderLimitSnapshot
	RuntimeState       *entity.ProviderAccountRuntimeState
	Now                time.Time
}

// ProviderOperationCompletion stores one finalized provider operation and its outbox side effects.
type ProviderOperationCompletion struct {
	Operation        entity.ProviderOperation
	ProjectionUpdate ProjectionUpdate
	ProviderEvents   []entity.ProviderEvent
	OutboxEvents     []entity.OutboxEvent
}

// WebhookNormalizer isolates provider-specific webhook payload parsing from the domain service.
type WebhookNormalizer interface {
	ProviderSlug() enum.ProviderSlug
	NormalizeWebhook(entity.WebhookEvent) (value.ProviderWebhookFacts, bool, error)
}

// Clock provides deterministic time for domain commands and tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides aggregate identifiers for domain commands.
type IDGenerator interface {
	New() uuid.UUID
}
