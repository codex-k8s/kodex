package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

// AccountUsageResolver confirms whether provider-hub can use one external account.
type AccountUsageResolver interface {
	ResolveExternalAccountUsage(context.Context, ExternalAccountUsageInput) (ExternalAccountUsageResult, error)
}

// ExternalAccountUsageInput identifies one access-manager account usage decision.
type ExternalAccountUsageInput struct {
	ExternalAccountID uuid.UUID
	ActionKey         string
	ScopeType         string
	ScopeID           string
}

// ExternalAccountUsageResult contains safe account metadata and secret reference.
type ExternalAccountUsageResult struct {
	ExternalAccountID string
	ProviderSlug      enum.ProviderSlug
	SecretStoreType   string
	SecretStoreRef    string
	AllowedActionKeys []string
}

// Dependencies contains domain service collaborators.
type Dependencies struct {
	Repository             providerrepo.Repository
	Clock                  providerrepo.Clock
	IDGenerator            providerrepo.IDGenerator
	AccountUsageResolver   AccountUsageResolver
	SecretResolver         secretresolver.Resolver
	ProviderAdapters       []providerclient.Adapter
	ProviderWriteExecutors []providerclient.WriteExecutor
	WebhookNormalizers     []providerrepo.WebhookNormalizer
}

// GetProviderAccountRuntimeStateInput identifies one runtime state.
type GetProviderAccountRuntimeStateInput struct {
	ProviderAccountRuntimeStateID *uuid.UUID
	ExternalAccountID             *uuid.UUID
	ProviderSlug                  enum.ProviderSlug
	Meta                          value.QueryMeta
}

// ListProviderAccountRuntimeStatesInput selects runtime states.
type ListProviderAccountRuntimeStatesInput struct {
	ProviderSlug       enum.ProviderSlug
	ExternalAccountIDs []uuid.UUID
	Statuses           []enum.ProviderAccountRuntimeStatus
	ProjectID          *uuid.UUID
	OrganizationID     *uuid.UUID
	Page               value.PageRequest
	Meta               value.QueryMeta
}

// ListProviderAccountRuntimeStatesResult returns runtime states and paging metadata.
type ListProviderAccountRuntimeStatesResult struct {
	RuntimeStates []entity.ProviderAccountRuntimeState
	Page          query.PageResult
}

// IngestWebhookEventInput stores a verified webhook from the edge gateway.
type IngestWebhookEventInput struct {
	ProviderSlug         enum.ProviderSlug
	DeliveryID           string
	EventName            string
	RepositoryProviderID string
	PayloadJSON          []byte
	ReceivedAt           time.Time
	Meta                 value.CommandMeta
}

// GetWebhookEventInput identifies a stored webhook.
type GetWebhookEventInput struct {
	WebhookEventID uuid.UUID
	Meta           value.QueryMeta
}

// ListWebhookEventsInput selects stored webhooks.
type ListWebhookEventsInput struct {
	ProviderSlug         enum.ProviderSlug
	DeliveryID           string
	EventNames           []string
	ProcessingStatuses   []enum.WebhookProcessingStatus
	RepositoryProviderID string
	ReceivedSince        *time.Time
	ReceivedUntil        *time.Time
	Page                 value.PageRequest
	Meta                 value.QueryMeta
}

// ListWebhookEventsResult returns stored webhooks and paging metadata.
type ListWebhookEventsResult struct {
	WebhookEvents []entity.WebhookEvent
	Page          query.PageResult
}

// RetryWebhookEventProcessingInput reprocesses a stored webhook.
type RetryWebhookEventProcessingInput struct {
	WebhookEventID uuid.UUID
	Meta           value.CommandMeta
}

// GetWorkItemProjectionInput identifies one stored work item projection.
type GetWorkItemProjectionInput struct {
	WorkItemProjectionID uuid.UUID
	Meta                 value.QueryMeta
}

// FindWorkItemByProviderRefInput selects a work item by provider-native reference.
type FindWorkItemByProviderRefInput struct {
	ProviderSlug       enum.ProviderSlug
	RepositoryFullName string
	Kind               enum.WorkItemKind
	Number             int64
	ProviderObjectID   string
	WebURL             string
	Meta               value.QueryMeta
}

// ListWorkItemProjectionsInput selects work item projections.
type ListWorkItemProjectionsInput struct {
	ProjectID          *uuid.UUID
	RepositoryID       *uuid.UUID
	ProviderSlug       enum.ProviderSlug
	RepositoryFullName string
	Kinds              []enum.WorkItemKind
	States             []string
	Labels             []string
	WorkItemTypes      []string
	DriftStatuses      []enum.WorkItemDriftStatus
	UpdatedSince       *time.Time
	Page               value.PageRequest
	Meta               value.QueryMeta
}

// ListWorkItemProjectionsResult returns projections and paging metadata.
type ListWorkItemProjectionsResult struct {
	WorkItemProjections []entity.ProviderWorkItemProjection
	Page                query.PageResult
}

// ListCommentsInput selects comments for one work item projection.
type ListCommentsInput struct {
	WorkItemProjectionID uuid.UUID
	Kinds                []enum.CommentKind
	Page                 value.PageRequest
	Meta                 value.QueryMeta
}

// ListCommentsResult returns comment projections and paging metadata.
type ListCommentsResult struct {
	Comments []entity.ProviderCommentProjection
	Page     query.PageResult
}

// ListRelationshipsInput selects relationships.
type ListRelationshipsInput struct {
	WorkItemProjectionID *uuid.UUID
	RelationshipTypes    []string
	Sources              []enum.RelationshipSource
	ConfidenceLevels     []enum.RelationshipConfidence
	Page                 value.PageRequest
	Meta                 value.QueryMeta
}

// ListRelationshipsResult returns relationships and paging metadata.
type ListRelationshipsResult struct {
	Relationships []entity.ProviderRelationship
	Page          query.PageResult
}

// ProviderArtifactTarget identifies a provider-native object referenced by an accelerating signal.
type ProviderArtifactTarget struct {
	ProviderSlug         enum.ProviderSlug
	RepositoryFullName   string
	ProviderRepositoryID string
	WorkItemKind         enum.WorkItemKind
	Number               int64
	ProviderObjectID     string
	WebURL               string
}

// RegisterProviderArtifactSignalInput records an accelerating signal from an agent or manager.
type RegisterProviderArtifactSignalInput struct {
	SignalID          string
	ExternalAccountID uuid.UUID
	Target            ProviderArtifactTarget
	Source            string
	ObservedAt        time.Time
	PayloadJSON       []byte
	Meta              value.CommandMeta
}

// ProviderArtifactSignalResult returns the accepted signal state.
type ProviderArtifactSignalResult struct {
	SignalID string
	Status   string
	Target   ProviderArtifactTarget
}

// EnqueueReconciliationInput schedules reconciliation cursors for one provider scope.
type EnqueueReconciliationInput struct {
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID uuid.UUID
	ScopeType         enum.SyncCursorScopeType
	ScopeRef          string
	ArtifactKinds     []enum.SyncArtifactKind
	Priority          enum.SyncCursorPriority
	Meta              value.CommandMeta
}

// EnqueueReconciliationResult returns affected reconciliation cursors.
type EnqueueReconciliationResult struct {
	SyncCursors []entity.SyncCursor
}

// RunReconciliationBatchInput leases one cursor for a reconciliation worker.
type RunReconciliationBatchInput struct {
	SyncCursorID      *uuid.UUID
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID *uuid.UUID
	MaxItems          int32
	LeaseOwner        string
	Meta              value.CommandMeta
}

// RunReconciliationBatchResult returns the leased cursor and current batch counters.
type RunReconciliationBatchResult struct {
	SyncCursor      entity.SyncCursor
	ItemsProcessed  int64
	EventsPublished int64
	RetryAfter      string
}

// GetSyncCursorInput identifies one reconciliation cursor.
type GetSyncCursorInput struct {
	SyncCursorID uuid.UUID
	Meta         value.QueryMeta
}

// ListSyncCursorsInput selects reconciliation cursors.
type ListSyncCursorsInput struct {
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID *uuid.UUID
	ScopeType         enum.SyncCursorScopeType
	ScopeRef          string
	ArtifactKinds     []enum.SyncArtifactKind
	Priorities        []enum.SyncCursorPriority
	IncludeHealthy    bool
	Page              value.PageRequest
	Meta              value.QueryMeta
}

// ListSyncCursorsResult returns cursors and paging metadata.
type ListSyncCursorsResult struct {
	SyncCursors []entity.SyncCursor
	Page        query.PageResult
}

// RecordProviderLimitSnapshotInput records an observed provider limit state.
type RecordProviderLimitSnapshotInput struct {
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	LimitClass        string
	Remaining         *int64
	LimitValue        *int64
	ResetAt           *time.Time
	CapturedAt        time.Time
	Source            enum.ProviderLimitSource
	Meta              value.CommandMeta
}

// ListProviderLimitSnapshotsInput selects provider limit snapshots.
type ListProviderLimitSnapshotsInput struct {
	ExternalAccountID *uuid.UUID
	ProviderSlug      enum.ProviderSlug
	LimitClasses      []string
	CapturedSince     *time.Time
	Page              value.PageRequest
	Meta              value.QueryMeta
}

// ListProviderLimitSnapshotsResult returns snapshots and paging metadata.
type ListProviderLimitSnapshotsResult struct {
	LimitSnapshots []entity.ProviderLimitSnapshot
	Page           query.PageResult
}

// ListProviderOperationsInput selects provider operation records.
type ListProviderOperationsInput struct {
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID *uuid.UUID
	OperationTypes    []enum.ProviderOperationType
	Statuses          []enum.ProviderOperationStatus
	TargetRef         string
	StartedSince      *time.Time
	Page              value.PageRequest
	Meta              value.QueryMeta
}

// ListProviderOperationsResult returns operation records and paging metadata.
type ListProviderOperationsResult struct {
	ProviderOperations []entity.ProviderOperation
	Page               query.PageResult
}
