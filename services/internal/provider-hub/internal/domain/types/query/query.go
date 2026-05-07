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
