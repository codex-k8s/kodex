// Package controlplane задаёт доменный persistence port полного control-plane.
package controlplane

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
)

// Scope — проверенная domain identity для одной PostgreSQL transaction.
type Scope struct {
	OrganizationID string
	ProjectID      string
	ActorID        string
}

// Receipt хранит persisted result одной semantic command.
type Receipt struct {
	OrganizationID string
	ProjectID      string
	Scope          string
	KeyHash        string
	RequestHash    string
	Result         entity.Resource
	Payload        []byte
	CreatedAt      time.Time
}

// Audit хранит безопасную атрибуцию state change.
type Audit struct {
	ID              string
	OrganizationID  string
	ProjectID       string
	ActorID         string
	Action          string
	ResourceID      string
	ResourceKind    string
	ResourceVersion uint64
	Outcome         string
	CorrelationID   string
	PolicyRevision  uint64
	OccurredAt      time.Time
}

// TurnLease защищает одну runtime attempt.
type TurnLease struct {
	TurnID              string
	TokenHash           string
	WorkloadID          string
	AuthorityGeneration uint64
	Attempt             uint32
	ExpiresAt           time.Time
	Fence               uint64
}

// ExpiredTurn связывает claimed aggregate с устаревшей lease под одной блокировкой.
type ExpiredTurn struct {
	Turn  entity.Resource
	Lease TurnLease
}

// ScheduleOccurrence — server-owned unique due occurrence.
type ScheduleOccurrence struct {
	ID                   string
	ScheduleID           string
	OrganizationID       string
	ProjectID            string
	ScheduledFor         time.Time
	TargetResourceID     string
	TargetKind           enum.Kind
	TargetVersion        uint64
	EffectiveInputSHA256 string
	OverlapPolicy        string
	MaximumAttempts      uint32
	InitialBackoff       time.Duration
	MaximumBackoff       time.Duration
	DeadLetterAt         time.Time
	State                string
	Attempt              uint32
	ClaimantWorkloadID   string
	AuthorityGeneration  uint64
	TokenHash            string
	LeaseExpiresAt       time.Time
	AvailableAt          time.Time
	Outcome              string
	ResultArtifactID     string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// TurnAttempt сохраняет immutable lineage каждой runtime lease.
type TurnAttempt struct {
	TurnID              string
	Attempt             uint32
	WorkloadID          string
	AuthorityGeneration uint64
	State               string
	InputSHA256         string
	LeaseFence          uint64
	StartedAt           time.Time
	FinishedAt          time.Time
	Outcome             string
}

// Tombstone — безопасный authoritative read удалённого агрегата.
type Tombstone struct {
	ResourceID       string
	Kind             enum.Kind
	Version          uint64
	ProjectionSHA256 string
	DeletedAt        time.Time
}

// Diagnostics содержит только bounded safe operational counters.
type Diagnostics struct {
	SchemaVersion              uint64
	PendingOutboxEvents        uint64
	TerminalOutboxEvents       uint64
	OldestPendingAge           time.Duration
	ActiveTurnLeases           uint64
	QueuedScheduleOccurrences  uint64
	RuntimePrincipalStatus     string
	RuntimePrincipalGeneration uint64
}

// Transaction выражает одну command transaction без утечки pgx.
type Transaction interface {
	GetReceipt(context.Context, string, string, string) (Receipt, error)
	SaveReceipt(context.Context, Receipt) error
	GetForUpdate(context.Context, string, string, string) (entity.Resource, error)
	Insert(context.Context, entity.Resource) error
	Update(context.Context, entity.Resource, uint64) error
	AppendAudit(context.Context, Audit) error
	AppendEvent(context.Context, event.Change) error
	ActorPermissions(context.Context, string, string, string) ([]string, error)
	ListSnapshotResources(context.Context, string, string) ([]entity.Resource, error)
	LatestRuntimeRevision(context.Context, string, string) (entity.Resource, error)
	ExpiredClaimedTurns(context.Context, string, string, int, time.Time) ([]ExpiredTurn, error)
	NextQueuedTurn(context.Context, string, string) (entity.Resource, error)
	SaveTurnLease(context.Context, TurnLease) error
	RenewTurnLease(context.Context, TurnLease, time.Time) (TurnLease, error)
	ValidateTurnLease(
		context.Context,
		string,
		string,
		string,
		uint64,
		uint32,
		time.Time,
	) (TurnLease, error)
	GetTurnLeaseForUpdate(context.Context, string) (TurnLease, error)
	DeleteTurnLease(context.Context, string, uint64) error
	SaveTurnAttempt(context.Context, TurnAttempt) error
	FinishTurnAttempt(context.Context, TurnAttempt) error
	DueSchedules(context.Context, string, string, int, time.Time) ([]entity.Resource, error)
	SaveScheduleOccurrence(context.Context, ScheduleOccurrence) error
	SkipOverlappedScheduleOccurrences(
		context.Context,
		string,
		string,
		time.Time,
	) ([]ScheduleOccurrence, error)
	RecoverExpiredScheduleOccurrences(
		context.Context,
		string,
		string,
		time.Time,
	) ([]ScheduleOccurrence, error)
	NextScheduleOccurrence(context.Context, string, string, time.Time) (ScheduleOccurrence, error)
	UpdateScheduleOccurrence(context.Context, ScheduleOccurrence, uint32, string) error
	GetScheduleOccurrenceForUpdate(context.Context, string, string, string) (ScheduleOccurrence, error)
	AuthorizeProject(context.Context, string, string, string, string, string) (entity.Resource, error)
	NextProofRevision(context.Context) (uint64, error)
}

// Repository — PostgreSQL authoritative boundary.
type Repository interface {
	Transact(context.Context, Scope, func(Transaction) error) error
	Get(context.Context, string, string, string, enum.Kind) (entity.Resource, error)
	List(context.Context, query.ResourceFilter) ([]entity.Resource, error)
	Search(context.Context, query.ResourceSearch) ([]entity.Resource, error)
	ListEligibleProjects(context.Context, string, string, string, int) ([]entity.Resource, error)
	ListAudit(context.Context, query.AuditFilter) ([]Audit, error)
	ListTombstones(context.Context, query.TombstoneFilter) ([]Tombstone, error)
	ListScheduleOccurrences(context.Context, query.ScheduleOccurrenceFilter) ([]ScheduleOccurrence, error)
	Diagnostics(context.Context, Scope) (Diagnostics, error)
	CacheEpoch(context.Context, string, string) (uint64, error)
	Check(context.Context) error
	Close()
}
