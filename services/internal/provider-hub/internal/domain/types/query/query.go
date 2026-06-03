// Package query contains read filters for provider-hub repositories.
package query

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// PageResult returns list continuation state.
type PageResult = value.PageResult

// AccountRuntimeStateLookup selects one runtime state by id or account identity.
type AccountRuntimeStateLookup struct {
	ID                *uuid.UUID
	ExternalAccountID *uuid.UUID
	ProviderSlug      enum.ProviderSlug
}

// AccountRuntimeStateFilter selects provider account runtime states.
type AccountRuntimeStateFilter struct {
	ProviderSlug       enum.ProviderSlug
	ExternalAccountIDs []uuid.UUID
	Statuses           []enum.ProviderAccountRuntimeStatus
	Page               value.PageRequest
}

// WebhookEventFilter selects raw webhook events.
type WebhookEventFilter struct {
	ProviderSlug         enum.ProviderSlug
	DeliveryID           string
	EventNames           []string
	ProcessingStatuses   []enum.WebhookProcessingStatus
	RepositoryProviderID string
	ReceivedSince        *time.Time
	ReceivedUntil        *time.Time
	Page                 value.PageRequest
}

// ProviderEventFilter selects normalized provider events.
type ProviderEventFilter struct {
	SourceWebhookEventID *uuid.UUID
	Page                 value.PageRequest
}

// ProviderTargetLookup selects one work item projection by provider-native reference.
type ProviderTargetLookup struct {
	ID                 *uuid.UUID
	ProviderSlug       enum.ProviderSlug
	RepositoryFullName string
	Kind               enum.WorkItemKind
	Number             int64
	ProviderObjectID   string
	WebURL             string
}

// WorkItemProjectionFilter selects work item projections.
type WorkItemProjectionFilter struct {
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
}

// CommentProjectionFilter selects comment projections for one work item.
type CommentProjectionFilter struct {
	WorkItemProjectionID uuid.UUID
	Kinds                []enum.CommentKind
	Page                 value.PageRequest
}

// RelationshipFilter selects provider relationships.
type RelationshipFilter struct {
	WorkItemProjectionID *uuid.UUID
	RelationshipTypes    []string
	Sources              []enum.RelationshipSource
	ConfidenceLevels     []enum.RelationshipConfidence
	Page                 value.PageRequest
}

// RelationshipLookup selects one provider relationship by its natural identity.
type RelationshipLookup struct {
	SourceWorkItemID  uuid.UUID
	TargetWorkItemID  *uuid.UUID
	TargetProviderRef string
	RelationshipType  string
}

// RepositoryMergeSignalLookup selects one provider-owned merge signal.
type RepositoryMergeSignalLookup struct {
	ID        *uuid.UUID
	SignalKey string
}

// RepositoryMergeSignalFilter selects provider-owned merge signals.
type RepositoryMergeSignalFilter struct {
	ProjectID            *uuid.UUID
	RepositoryID         *uuid.UUID
	ProviderSlug         enum.ProviderSlug
	RepositoryFullName   string
	ProviderRepositoryID string
	Kinds                []enum.RepositoryMergeSignalKind
	Statuses             []enum.RepositoryMergeSignalStatus
	PullRequestNumber    *int64
	MergedSince          *time.Time
	Page                 value.PageRequest
}

// RepositoryChangeSignalLookup selects one provider-owned repository change signal.
type RepositoryChangeSignalLookup struct {
	ID        *uuid.UUID
	SignalKey string
}

// RepositoryChangeSignalFilter selects provider-owned repository change signals.
type RepositoryChangeSignalFilter struct {
	ProjectID             *uuid.UUID
	RepositoryID          *uuid.UUID
	ProviderSlug          enum.ProviderSlug
	RepositoryFullName    string
	ProviderRepositoryID  string
	Kinds                 []enum.RepositoryChangeSignalKind
	Statuses              []enum.RepositoryChangeSignalStatus
	BaseBranch            string
	CommitSHA             string
	ServicesPolicyChanged *bool
	DeployRelevantChanged *bool
	ObservedSince         *time.Time
	Page                  value.PageRequest
}

// RepositoryAdoptionScanLookup selects one provider-owned adoption scan snapshot.
type RepositoryAdoptionScanLookup struct {
	ID                  *uuid.UUID
	SnapshotKey         string
	ProviderOperationID *uuid.UUID
}

// RepositoryAdoptionScanFilter selects provider-owned adoption scan snapshots.
type RepositoryAdoptionScanFilter struct {
	ProjectID            *uuid.UUID
	RepositoryID         *uuid.UUID
	ExternalAccountID    *uuid.UUID
	ProviderSlug         enum.ProviderSlug
	RepositoryFullName   string
	ProviderRepositoryID string
	Statuses             []enum.RepositoryAdoptionScanStatus
	ObservedSince        *time.Time
	Page                 value.PageRequest
}

// SyncCursorFilter selects reconciliation cursors.
type SyncCursorFilter struct {
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID *uuid.UUID
	ScopeType         enum.SyncCursorScopeType
	ScopeRef          string
	ArtifactKinds     []enum.SyncArtifactKind
	Priorities        []enum.SyncCursorPriority
	IncludeHealthy    bool
	Page              value.PageRequest
}

// LimitSnapshotFilter selects provider limit snapshots.
type LimitSnapshotFilter struct {
	ExternalAccountID *uuid.UUID
	ProviderSlug      enum.ProviderSlug
	LimitClasses      []string
	CapturedSince     *time.Time
	Page              value.PageRequest
}

// ProviderOperationFilter selects provider operation records.
type ProviderOperationFilter struct {
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID *uuid.UUID
	OperationTypes    []enum.ProviderOperationType
	Statuses          []enum.ProviderOperationStatus
	TargetRef         string
	StartedSince      *time.Time
	Page              value.PageRequest
}
