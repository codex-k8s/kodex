package query

import (
	"encoding/json"
	"time"

	enumtypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/enum"
)

// MissionControlEntityUpsertParams defines one unconditional projection upsert.
type MissionControlEntityUpsertParams struct {
	ProjectID         string
	EntityKind        enumtypes.MissionControlEntityKind
	EntityExternalKey string
	ProviderKind      enumtypes.MissionControlProviderKind
	ProviderURL       string
	Title             string
	ActiveState       enumtypes.MissionControlActiveState
	SyncStatus        enumtypes.MissionControlSyncStatus
	ProjectionVersion int64
	CardPayloadJSON   json.RawMessage
	DetailPayloadJSON json.RawMessage
	LastTimelineAt    *time.Time
	ProviderUpdatedAt *time.Time
	ProjectedAt       time.Time
	StaleAfter        *time.Time
}

// MissionControlEntityProjectionUpdateParams defines one optimistic projection update.
type MissionControlEntityProjectionUpdateParams struct {
	ProjectID                 string
	EntityKind                enumtypes.MissionControlEntityKind
	EntityExternalKey         string
	ExpectedProjectionVersion int64
	ProviderURL               string
	Title                     string
	ActiveState               enumtypes.MissionControlActiveState
	SyncStatus                enumtypes.MissionControlSyncStatus
	CardPayloadJSON           json.RawMessage
	DetailPayloadJSON         json.RawMessage
	LastTimelineAt            *time.Time
	ProviderUpdatedAt         *time.Time
	ProjectedAt               time.Time
	StaleAfter                *time.Time
}

// MissionControlEntityListFilter defines one active-set lookup filter.
type MissionControlEntityListFilter struct {
	ProjectID    string
	ActiveStates []enumtypes.MissionControlActiveState
	SyncStatuses []enumtypes.MissionControlSyncStatus
	Limit        int
}

// MissionControlRelationSeed defines one relation row used in replace operations.
type MissionControlRelationSeed struct {
	TargetEntityID int64
	RelationKind   enumtypes.MissionControlRelationKind
	SourceKind     enumtypes.MissionControlRelationSourceKind
}

// MissionControlRelationReplaceParams defines one relation-set rewrite for one source entity.
type MissionControlRelationReplaceParams struct {
	ProjectID      string
	SourceEntityID int64
	Relations      []MissionControlRelationSeed
}

// MissionControlTimelineEntryUpsertParams defines one timeline projection upsert.
type MissionControlTimelineEntryUpsertParams struct {
	ProjectID        string
	EntityID         int64
	SourceKind       enumtypes.MissionControlTimelineSourceKind
	EntryExternalKey string
	CommandID        string
	Summary          string
	BodyMarkdown     string
	PayloadJSON      json.RawMessage
	OccurredAt       time.Time
	ProviderURL      string
	IsReadOnly       bool
}

// MissionControlTimelineListFilter defines one timeline lookup filter.
type MissionControlTimelineListFilter struct {
	EntityID int64
	Limit    int
}

// MissionControlCommandCreateParams defines one command-ledger insert.
type MissionControlCommandCreateParams struct {
	ProjectID           string
	CommandKind         enumtypes.MissionControlCommandKind
	TargetEntityID      *int64
	ActorID             string
	BusinessIntentKey   string
	CorrelationID       string
	Status              enumtypes.MissionControlCommandStatus
	FailureReason       enumtypes.MissionControlCommandFailureReason
	ApprovalRequestID   string
	ApprovalState       enumtypes.MissionControlApprovalState
	ApprovalRequestedAt *time.Time
	ApprovalDecidedAt   *time.Time
	PayloadJSON         json.RawMessage
	ResultPayloadJSON   json.RawMessage
	ProviderDeliveries  json.RawMessage
	RequestedAt         time.Time
	UpdatedAt           time.Time
	ReconciledAt        *time.Time
}

// MissionControlCommandStatusUpdateParams defines one command status transition persistence update.
type MissionControlCommandStatusUpdateParams struct {
	CommandID           string
	Status              enumtypes.MissionControlCommandStatus
	FailureReason       enumtypes.MissionControlCommandFailureReason
	ApprovalRequestID   string
	ApprovalState       enumtypes.MissionControlApprovalState
	ApprovalRequestedAt *time.Time
	ApprovalDecidedAt   *time.Time
	ResultPayloadJSON   json.RawMessage
	ProviderDeliveries  json.RawMessage
	UpdatedAt           time.Time
	ReconciledAt        *time.Time
}

// MissionControlCommandListFilter defines one command lookup filter for warmup/reconcile workers.
type MissionControlCommandListFilter struct {
	ProjectID string
	Statuses  []enumtypes.MissionControlCommandStatus
	Limit     int
}

// MissionControlWarmupRequest defines the owner-owned entry-point contract for worker warmup/backfill.
type MissionControlWarmupRequest struct {
	ProjectID     string
	RequestedBy   string
	CorrelationID string
	ForceRebuild  bool
}
