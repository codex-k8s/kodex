// Package controlplane задаёт доменный persistence port полного control-plane.
package controlplane

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
)

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
	TurnID     string
	TokenHash  string
	WorkloadID string
	ExpiresAt  time.Time
	Fence      uint64
}

// ExpiredTurn связывает claimed aggregate с устаревшей lease под одной блокировкой.
type ExpiredTurn struct {
	Turn  entity.Resource
	Lease TurnLease
}

// ScheduleOccurrence — server-owned unique due occurrence.
type ScheduleOccurrence struct {
	ID               string
	ScheduleID       string
	OrganizationID   string
	ProjectID        string
	ScheduledFor     time.Time
	TargetResourceID string
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
	ExpiredClaimedTurns(context.Context, string, string, int, time.Time) ([]ExpiredTurn, error)
	NextQueuedTurn(context.Context, string, string) (entity.Resource, error)
	SaveTurnLease(context.Context, TurnLease) error
	ValidateTurnLease(context.Context, string, string, string, time.Time) (TurnLease, error)
	DeleteTurnLease(context.Context, string, uint64) error
	DueSchedules(context.Context, string, string, int, time.Time) ([]entity.Resource, error)
	SaveScheduleOccurrence(context.Context, ScheduleOccurrence) error
	AuthorizeProject(context.Context, string, string, string, string, string) (entity.Resource, error)
	NextProofRevision(context.Context) (uint64, error)
}

// Repository — PostgreSQL authoritative boundary.
type Repository interface {
	Transact(context.Context, string, string, func(Transaction) error) error
	Get(context.Context, string, string, string) (entity.Resource, error)
	List(context.Context, query.ResourceFilter) ([]entity.Resource, error)
	ListEligibleProjects(context.Context, string, string, string, int) ([]entity.Resource, error)
	CacheEpoch(context.Context, string, string) (uint64, error)
	Check(context.Context) error
	Close()
}
