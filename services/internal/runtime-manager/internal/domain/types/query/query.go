// Package query contains runtime-manager read filters and paging helpers.
package query

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// PageRequest is a stable repository paging contract.
type PageRequest = value.PageRequest

// PageResult describes list continuation state.
type PageResult = value.PageResult

// CommandIdentity identifies a previously applied idempotent command.
type CommandIdentity struct {
	CommandID      uuid.UUID
	IdempotencyKey string
	Operation      string
	Actor          value.Actor
}

// SlotFilter selects runtime slots for list queries.
type SlotFilter struct {
	ProjectID      *uuid.UUID
	Statuses       []enum.SlotStatus
	RuntimeProfile string
	FleetScopeID   *uuid.UUID
	AgentRunID     *uuid.UUID
	Page           value.PageRequest
}

// ReusableSlotFilter selects one safe slot for deterministic reuse.
type ReusableSlotFilter struct {
	RuntimeProfile string
	RuntimeMode    enum.RuntimeMode
	Fingerprint    string
	AgentRunID     *uuid.UUID
	ProjectID      *uuid.UUID
	RepositoryIDs  []uuid.UUID
	FleetScopeID   *uuid.UUID
	ClusterID      *uuid.UUID
	LeaseOwner     string
	LeaseUntil     time.Time
	Now            time.Time
}

// WorkspaceMaterializationFilter selects workspace preparation attempts for list queries.
type WorkspaceMaterializationFilter struct {
	SlotID     *uuid.UUID
	AgentRunID *uuid.UUID
	Statuses   []enum.WorkspaceMaterializationStatus
	Page       value.PageRequest
}

// JobFilter selects platform technical jobs.
type JobFilter struct {
	Statuses      []enum.JobStatus
	JobTypes      []enum.JobType
	ProjectID     *uuid.UUID
	SlotID        *uuid.UUID
	AgentRunID    *uuid.UUID
	ReleaseLineID *uuid.UUID
	Page          value.PageRequest
}

// JobClaimFilter selects one runnable job for an exclusive short lease.
type JobClaimFilter struct {
	JobTypes       []enum.JobType
	FleetScopeID   *uuid.UUID
	LeaseOwner     string
	LeaseTokenHash string
	LeaseUntil     time.Time
	Now            time.Time
}

// RuntimeArtifactRefFilter selects external artifact references.
type RuntimeArtifactRefFilter struct {
	JobID         *uuid.UUID
	SlotID        *uuid.UUID
	ArtifactTypes []enum.RuntimeArtifactType
	Page          value.PageRequest
}

// CleanupBatchFilter selects expired runtime objects for one cleanup command.
type CleanupBatchFilter struct {
	CleanupPolicyID *uuid.UUID
	Limit           int
	LeaseOwner      string
	LeaseUntil      time.Time
	Now             time.Time
}

// PrewarmPoolReconcileFilter selects one prewarm pool for capacity reconciliation.
type PrewarmPoolReconcileFilter struct {
	PrewarmPoolID uuid.UUID
	LeaseOwner    string
	LeaseUntil    time.Time
	Now           time.Time
}
