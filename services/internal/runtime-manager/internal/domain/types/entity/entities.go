// Package entity contains persisted aggregate models owned by runtime-manager.
package entity

import (
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// Base stores common aggregate metadata used for optimistic concurrency.
type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Slot is an isolated runtime environment.
type Slot struct {
	Base
	SlotKey                          string
	Status                           enum.SlotStatus
	RuntimeMode                      enum.RuntimeMode
	IsPrewarmed                      bool
	FleetScopeID                     *uuid.UUID
	ClusterID                        *uuid.UUID
	NamespaceName                    string
	AgentRunID                       *uuid.UUID
	ProjectID                        *uuid.UUID
	RepositoryIDs                    []uuid.UUID
	ActiveWorkspaceMaterializationID *uuid.UUID
	RuntimeProfile                   string
	Fingerprint                      string
	LeaseOwner                       string
	LeaseUntil                       *time.Time
	LastErrorCode                    string
	LastErrorMessage                 string
}

// WorkspaceMaterialization is one attempt to prepare workspace sources.
type WorkspaceMaterialization struct {
	Base
	SlotID           uuid.UUID
	Status           enum.WorkspaceMaterializationStatus
	PolicyDigest     string
	Sources          []value.WorkspaceSource
	Fingerprint      string
	StartedAt        *time.Time
	FinishedAt       *time.Time
	LastErrorCode    string
	LastErrorMessage string
}

// BuildContext is one runtime-owned self-deploy build context request.
type BuildContext struct {
	Base
	Status                enum.BuildContextStatus
	ProjectID             uuid.UUID
	RepositoryID          uuid.UUID
	Provider              string
	ProviderOwner         string
	ProviderName          string
	SourceRef             string
	SourceCommitSHA       string
	AffectedServiceKeys   []string
	BuildPlanFingerprint  string
	ContextFingerprint    string
	SourceSnapshotRef     string
	SourceSnapshotDigest  string
	BuildContextRef       string
	BuildContextDigest    string
	ManifestBundleDigests map[string]string
	StartedAt             *time.Time
	FinishedAt            *time.Time
	LastErrorCode         string
	LastErrorMessage      string
	NextAction            string
}

// CommandResult stores idempotency trail for mutating runtime-manager commands.
type CommandResult struct {
	Key            string
	CommandID      *uuid.UUID
	IdempotencyKey string
	Actor          value.Actor
	Operation      string
	AggregateType  string
	AggregateID    uuid.UUID
	ResultPayload  []byte
	CreatedAt      time.Time
}

// Job is a technical platform operation owned by runtime-manager.
type Job struct {
	Base
	CommandID             string
	JobType               enum.JobType
	Status                enum.JobStatus
	Priority              enum.JobPriority
	JobInputJSON          []byte
	LeaseOwner            string
	LeaseTokenHash        string
	LeaseUntil            *time.Time
	ClaimAttempt          int64
	SlotID                *uuid.UUID
	AgentRunID            *uuid.UUID
	ProjectID             *uuid.UUID
	RepositoryID          *uuid.UUID
	ReleaseLineID         *uuid.UUID
	PackageInstallationID *uuid.UUID
	FleetScopeID          *uuid.UUID
	ClusterID             *uuid.UUID
	RequestedBy           *uuid.UUID
	StartedAt             *time.Time
	FinishedAt            *time.Time
	NextAction            string
	LastErrorCode         string
	LastErrorMessage      string
	ShortLogTail          string
	FullLogRef            string
	Steps                 []JobStep
}

// JobStep is one step of a platform job.
type JobStep struct {
	Base
	JobID        uuid.UUID
	StepKey      string
	Status       enum.JobStepStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ShortLogTail string
	ExternalRef  string
	ErrorCode    string
	ErrorMessage string
}

// RuntimeArtifactRef points to an external runtime artifact.
type RuntimeArtifactRef struct {
	ID           uuid.UUID
	JobID        *uuid.UUID
	SlotID       *uuid.UUID
	ArtifactType enum.RuntimeArtifactType
	ExternalRef  string
	Digest       string
	MetadataJSON []byte
	CreatedAt    time.Time
}

// CleanupPolicy describes retention and cleanup rules for runtime objects.
type CleanupPolicy struct {
	Base
	ScopeType        enum.RuntimeScopeType
	ScopeID          string
	TTLSeconds       int64
	FailedTTLSeconds int64
	KeepShortLogTail bool
	Status           enum.CleanupPolicyStatus
}

// PrewarmPool describes desired prewarmed slot capacity.
type PrewarmPool struct {
	Base
	ScopeType          enum.PrewarmPoolScopeType
	ScopeID            string
	RuntimeProfile     string
	FleetScopeID       *uuid.UUID
	TargetSize         int64
	Status             enum.PrewarmPoolStatus
	LastCapacityStatus enum.CapacityStatus
}

// OutboxEvent stores a domain event until it is published to consumers.
type OutboxEvent struct {
	outboxlib.Event
	PublishedAt         *time.Time
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailureKind         string
	FailedPermanentlyAt *time.Time
	LastError           string
}
