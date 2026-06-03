// Package entity contains persisted aggregate models owned by provider-hub.
package entity

import (
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// Base stores common aggregate metadata used for optimistic concurrency.
type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderAccountRuntimeState stores operational provider-side account state.
type ProviderAccountRuntimeState struct {
	Base
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	Status            enum.ProviderAccountRuntimeStatus
	LastCheckedAt     *time.Time
	LastSuccessAt     *time.Time
	LastErrorCode     string
	LastErrorMessage  string
}

// WebhookEvent stores one provider webhook accepted from the edge gateway.
type WebhookEvent struct {
	ID                   uuid.UUID
	ProviderSlug         enum.ProviderSlug
	DeliveryID           string
	EventName            string
	RepositoryProviderID string
	ReceivedAt           time.Time
	ProcessingStatus     enum.WebhookProcessingStatus
	PayloadJSON          []byte
	PayloadDigest        string
	LastError            string
	RetainUntil          time.Time
}

// ProviderEvent stores one normalized provider event derived from a webhook or reconciliation.
type ProviderEvent struct {
	ID                   uuid.UUID
	SourceWebhookEventID *uuid.UUID
	EventType            string
	AggregateType        string
	AggregateID          string
	PayloadJSON          []byte
	OccurredAt           time.Time
}

// ProviderWorkItemProjection stores a normalized Issue or PR/MR mirror.
type ProviderWorkItemProjection struct {
	Base
	ProviderSlug       enum.ProviderSlug
	ProviderWorkItemID string
	ProjectID          *uuid.UUID
	RepositoryID       *uuid.UUID
	RepositoryFullName string
	Kind               enum.WorkItemKind
	Number             int64
	URL                string
	Title              string
	State              string
	WorkItemType       string
	LabelsJSON         []byte
	AssigneesJSON      []byte
	Milestone          string
	ProjectFieldsJSON  []byte
	WatermarkStatus    enum.WorkItemWatermarkStatus
	WatermarkJSON      []byte
	BodyDigest         string
	ProviderUpdatedAt  *time.Time
	SyncedAt           time.Time
	DriftStatus        enum.WorkItemDriftStatus
}

// ProviderCommentProjection stores a normalized comment, mention or review signal.
type ProviderCommentProjection struct {
	Base
	WorkItemProjectionID uuid.UUID
	ProviderCommentID    string
	Kind                 enum.CommentKind
	ReviewState          enum.ReviewState
	AuthorProviderLogin  string
	BodyDigest           string
	Summary              string
	ProviderCreatedAt    *time.Time
	ProviderUpdatedAt    *time.Time
}

// ProviderRelationship stores a normalized relationship between provider-native objects.
type ProviderRelationship struct {
	ID                uuid.UUID
	Version           int64
	SourceWorkItemID  uuid.UUID
	TargetWorkItemID  *uuid.UUID
	TargetProviderRef string
	RelationshipType  string
	Source            enum.RelationshipSource
	Confidence        enum.RelationshipConfidence
	CreatedAt         time.Time
}

// SyncCursor stores incremental reconciliation state for one provider scope.
type SyncCursor struct {
	Base
	ProviderSlug        enum.ProviderSlug
	ExternalAccountID   uuid.UUID
	ScopeType           enum.SyncCursorScopeType
	ScopeRef            string
	ArtifactKind        enum.SyncArtifactKind
	CursorValue         string
	OverlapSince        *time.Time
	Priority            enum.SyncCursorPriority
	LastSuccessAt       *time.Time
	LastCheckedAt       *time.Time
	LastError           string
	RateBudgetStateJSON []byte
	LeaseOwner          string
	LeaseUntil          *time.Time
}

// ReconciliationRequest stores an idempotent enqueue command for one provider scope.
type ReconciliationRequest struct {
	ID                uuid.UUID
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID uuid.UUID
	ScopeType         enum.SyncCursorScopeType
	ScopeRef          string
	IdempotencyKey    string
	ArtifactKinds     []enum.SyncArtifactKind
	Priority          enum.SyncCursorPriority
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ProviderArtifactSignal stores signal-level idempotency before cursor enqueue.
type ProviderArtifactSignal struct {
	ID                uuid.UUID
	IdentityKey       string
	ProviderSlug      enum.ProviderSlug
	ExternalAccountID uuid.UUID
	Source            string
	ScopeType         enum.SyncCursorScopeType
	ScopeRef          string
	ArtifactKinds     []enum.SyncArtifactKind
	TargetJSON        []byte
	PayloadJSON       []byte
	ObservedAt        time.Time
	CreatedAt         time.Time
}

// RepositoryMergeSignal stores a safe provider-side fact that an onboarding PR was merged.
type RepositoryMergeSignal struct {
	Base
	SignalKey                   string
	Kind                        enum.RepositoryMergeSignalKind
	ProviderSlug                enum.ProviderSlug
	ProjectID                   *uuid.UUID
	RepositoryID                *uuid.UUID
	RepositoryFullName          string
	ProviderRepositoryID        string
	WorkItemProjectionID        uuid.UUID
	ProviderWorkItemID          string
	PullRequestNumber           int64
	PullRequestProviderID       string
	PullRequestURL              string
	BaseBranch                  string
	HeadBranch                  string
	MergeCommitSHA              string
	SourceRef                   string
	RelatedProviderOperationRef string
	WatermarkDigest             string
	ObservedAt                  time.Time
	MergedAt                    time.Time
	Status                      enum.RepositoryMergeSignalStatus
}

// RepositoryChangePathCategoryCount stores one safe changed-path category counter.
type RepositoryChangePathCategoryCount struct {
	Category enum.RepositoryChangePathCategory
	Count    int64
}

// RepositoryChangeSignal stores a safe provider-side fact that repository contents changed.
type RepositoryChangeSignal struct {
	Base
	SignalKey             string
	Kind                  enum.RepositoryChangeSignalKind
	ProviderSlug          enum.ProviderSlug
	ProjectID             *uuid.UUID
	RepositoryID          *uuid.UUID
	RepositoryFullName    string
	ProviderRepositoryID  string
	Ref                   string
	BaseBranch            string
	CommitSHA             string
	BeforeSHA             string
	SourceRef             string
	PullRequestNumber     int64
	PullRequestProviderID string
	PullRequestURL        string
	PathSummaryStatus     enum.RepositoryChangePathSummaryStatus
	ChangedPathCount      int64
	PathDigest            string
	PathCategories        []RepositoryChangePathCategoryCount
	ServicesPolicyChanged bool
	DeployRelevantChanged bool
	ChangeFingerprint     string
	ObservedAt            time.Time
	Status                enum.RepositoryChangeSignalStatus
}

// RepositoryAdoptionScanMarker stores one safe marker path discovered without file contents.
type RepositoryAdoptionScanMarker struct {
	Path         string
	Kind         enum.RepositoryAdoptionMarkerKind
	ObjectDigest string
	SizeBytes    int64
}

// RepositoryAdoptionScanSnapshot stores a safe lightweight snapshot for repository adoption planning.
type RepositoryAdoptionScanSnapshot struct {
	Base
	SnapshotKey          string
	ProviderOperationID  uuid.UUID
	ExternalAccountID    uuid.UUID
	ProviderSlug         enum.ProviderSlug
	RepositoryFullName   string
	ProviderRepositoryID string
	RepositoryURL        string
	DefaultBranch        string
	RequestedRef         string
	ScannedRef           string
	HeadSHA              string
	Status               enum.RepositoryAdoptionScanStatus
	Markers              []RepositoryAdoptionScanMarker
	FileCount            int64
	VisibleFileCount     int64
	TreeTruncated        bool
	Warnings             []string
	SnapshotDigest       string
	ObservedAt           time.Time
}

// ProviderLimitSnapshot stores one observed provider rate or quota snapshot.
type ProviderLimitSnapshot struct {
	ID                uuid.UUID
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	LimitClass        string
	Remaining         *int64
	LimitValue        *int64
	ResetAt           *time.Time
	CapturedAt        time.Time
	Source            enum.ProviderLimitSource
}

// ProviderOperation stores audit and diagnostics for a provider operation.
type ProviderOperation struct {
	Base
	CommandID              string
	ActorID                *uuid.UUID
	ExternalAccountID      uuid.UUID
	ProviderSlug           enum.ProviderSlug
	OperationType          enum.ProviderOperationType
	TargetRef              string
	Status                 enum.ProviderOperationStatus
	ResultRef              string
	ProviderObjectID       string
	RepositoryFullName     string
	ErrorCode              string
	ErrorMessage           string
	RateLimitSnapshotID    *uuid.UUID
	OperationPolicyContext value.ProviderOperationPolicyContext
	ApprovalGateRef        value.ApprovalGateReference
	ProviderVersion        string
	BaseBranch             string
	StartedAt              time.Time
	FinishedAt             *time.Time
}

// OutboxEvent stores a domain event until it is published to platform-event-log.
type OutboxEvent = outboxlib.Record
