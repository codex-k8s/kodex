package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

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
