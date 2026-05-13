package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// ReserveSlotInput describes a request to allocate a runtime slot.
type ReserveSlotInput struct {
	RuntimeProfile        string
	RuntimeMode           enum.RuntimeMode
	WorkspacePolicyDigest string
	AgentRunID            *uuid.UUID
	ProjectID             *uuid.UUID
	RepositoryIDs         []uuid.UUID
	PlacementConstraints  PlacementConstraintsInput
	Meta                  value.CommandMeta
}

// PrepareRuntimeInput describes a facade request to reserve a slot and start workspace preparation.
type PrepareRuntimeInput struct {
	AgentRunID           *uuid.UUID
	RuntimeProfile       string
	RuntimeMode          enum.RuntimeMode
	WorkspacePolicy      WorkspacePolicyInput
	PlacementConstraints PlacementConstraintsInput
	Meta                 value.CommandMeta
}

// PrepareRuntimeResult contains the slot and materialization attempt started by PrepareRuntime.
type PrepareRuntimeResult struct {
	Slot                     entity.Slot
	WorkspaceMaterialization entity.WorkspaceMaterialization
	RuntimeContext           RuntimeContext
}

// RuntimeContext is the prepared runtime reference returned to orchestration callers.
type RuntimeContext struct {
	SlotID                     uuid.UUID
	AgentRunID                 *uuid.UUID
	FleetScopeID               *uuid.UUID
	ClusterID                  *uuid.UUID
	NamespaceName              string
	RuntimeProfile             string
	WorkspaceRoot              string
	MaterializationFingerprint string
}

// WorkspacePolicyInput is a checked project-catalog policy snapshot accepted by runtime-manager.
type WorkspacePolicyInput struct {
	ProjectID               uuid.UUID
	PolicyDigest            string
	PolicyVersion           int64
	Sources                 []value.WorkspaceSource
	ActivePolicyOverrideIDs []string
}

// StartWorkspaceMaterializationInput describes a request to start source preparation in a slot.
type StartWorkspaceMaterializationInput struct {
	SlotID          uuid.UUID
	WorkspacePolicy WorkspacePolicyInput
	Meta            value.CommandMeta
}

// ReportWorkspaceMaterializationProgressInput describes a materialization status update.
type ReportWorkspaceMaterializationProgressInput struct {
	WorkspaceMaterializationID uuid.UUID
	Status                     enum.WorkspaceMaterializationStatus
	Fingerprint                string
	StartedAt                  *time.Time
	FinishedAt                 *time.Time
	ErrorCode                  string
	ErrorMessage               string
	Meta                       value.CommandMeta
}

// GetWorkspaceMaterializationInput describes a materialization read request.
type GetWorkspaceMaterializationInput struct {
	WorkspaceMaterializationID uuid.UUID
	Meta                       value.QueryMeta
}

// ListWorkspaceMaterializationsInput describes materialization list filters.
type ListWorkspaceMaterializationsInput struct {
	SlotID     *uuid.UUID
	AgentRunID *uuid.UUID
	Statuses   []enum.WorkspaceMaterializationStatus
	Page       value.PageRequest
	Meta       value.QueryMeta
}

// ListWorkspaceMaterializationsResult contains a page of materialization attempts.
type ListWorkspaceMaterializationsResult struct {
	WorkspaceMaterializations []entity.WorkspaceMaterialization
	Page                      value.PageResult
}

// CreateJobInput describes a request to create a platform technical job.
type CreateJobInput struct {
	JobType               enum.JobType
	Priority              enum.JobPriority
	SlotID                *uuid.UUID
	AgentRunID            *uuid.UUID
	ProjectID             *uuid.UUID
	RepositoryID          *uuid.UUID
	ReleaseLineID         *uuid.UUID
	PackageInstallationID *uuid.UUID
	PlacementConstraints  PlacementConstraintsInput
	JobInputJSON          []byte
	Meta                  value.CommandMeta
}

// PlacementConstraintsInput contains safe placement hints accepted by runtime-manager callers.
type PlacementConstraintsInput struct {
	ProjectID             *uuid.UUID
	RepositoryIDs         []uuid.UUID
	ServiceKeys           []string
	RuntimeProfile        string
	PreferredFleetScopeID *uuid.UUID
	RequiredCapabilities  []string
	MetadataJSON          []byte
}

// PlacementResolutionRequest is the normalized request sent to the fleet placement owner.
type PlacementResolutionRequest struct {
	ProjectID                *uuid.UUID
	RepositoryIDs            []uuid.UUID
	ServiceKeys              []string
	RuntimeMode              enum.RuntimeMode
	RuntimeProfile           string
	PreferredFleetScopeID    *uuid.UUID
	RequiredCapabilities     []string
	PlacementConstraintsJSON []byte
	RuntimeRequirementsJSON  []byte
	Meta                     value.CommandMeta
}

// PlacementResolution is the fleet-owned result runtime-manager persists on slots and jobs.
type PlacementResolution struct {
	FleetScopeID uuid.UUID
	ClusterID    uuid.UUID
}

// PlacementResolver resolves runtime placement through fleet-manager.
type PlacementResolver interface {
	ResolvePlacement(ctx context.Context, request PlacementResolutionRequest) (PlacementResolution, error)
}

// ClaimRunnableJobInput describes a worker claim request for a runnable job.
type ClaimRunnableJobInput struct {
	JobTypes     []enum.JobType
	WorkerID     string
	LeaseOwner   string
	LeaseUntil   time.Time
	FleetScopeID *uuid.UUID
	Meta         value.CommandMeta
}

// ClaimRunnableJobResult contains a claimed job and its one-time lease token.
type ClaimRunnableJobResult struct {
	Job        entity.Job
	LeaseToken string
}

// ReportJobStepProgressInput describes one step update from a job worker.
type ReportJobStepProgressInput struct {
	JobID        uuid.UUID
	LeaseToken   string
	StepKey      string
	Status       enum.JobStepStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ShortLogTail string
	ExternalRef  string
	ErrorCode    string
	ErrorMessage string
	ArtifactRefs []RuntimeArtifactRefInput
	Meta         value.CommandMeta
}

// CompleteJobInput describes a successful job completion.
type CompleteJobInput struct {
	JobID        uuid.UUID
	LeaseToken   string
	ShortLogTail string
	FullLogRef   string
	Meta         value.CommandMeta
}

// FailJobInput describes a failed job completion with operator diagnostics.
type FailJobInput struct {
	JobID        uuid.UUID
	LeaseToken   string
	ErrorCode    string
	ErrorMessage string
	ShortLogTail string
	FullLogRef   string
	NextAction   string
	Meta         value.CommandMeta
}

// CancelJobInput describes policy-driven cancellation for a non-terminal job.
type CancelJobInput struct {
	JobID uuid.UUID
	Meta  value.CommandMeta
}

// GetJobInput describes a job read request.
type GetJobInput struct {
	JobID uuid.UUID
	Meta  value.QueryMeta
}

// ListJobsInput describes job list filters.
type ListJobsInput struct {
	Statuses      []enum.JobStatus
	JobTypes      []enum.JobType
	ProjectID     *uuid.UUID
	SlotID        *uuid.UUID
	AgentRunID    *uuid.UUID
	ReleaseLineID *uuid.UUID
	Page          value.PageRequest
	Meta          value.QueryMeta
}

// ListJobsResult contains a page of platform jobs.
type ListJobsResult struct {
	Jobs []entity.Job
	Page value.PageResult
}

// RuntimeArtifactRefInput is the caller-provided reference data.
type RuntimeArtifactRefInput struct {
	ArtifactType enum.RuntimeArtifactType
	ExternalRef  string
	Digest       string
	MetadataJSON []byte
}

// RecordRuntimeArtifactRefInput describes a command to store one external artifact reference.
type RecordRuntimeArtifactRefInput struct {
	JobID       *uuid.UUID
	SlotID      *uuid.UUID
	ArtifactRef RuntimeArtifactRefInput
	Meta        value.CommandMeta
}

// ListRuntimeArtifactRefsInput describes artifact reference list filters.
type ListRuntimeArtifactRefsInput struct {
	JobID         *uuid.UUID
	SlotID        *uuid.UUID
	ArtifactTypes []enum.RuntimeArtifactType
	Page          value.PageRequest
	Meta          value.QueryMeta
}

// ListRuntimeArtifactRefsResult contains a page of external runtime artifact references.
type ListRuntimeArtifactRefsResult struct {
	RuntimeArtifactRefs []entity.RuntimeArtifactRef
	Page                value.PageResult
}

// CreateOrUpdateCleanupPolicyInput describes retention policy upsert.
type CreateOrUpdateCleanupPolicyInput struct {
	CleanupPolicyID  *uuid.UUID
	ScopeType        enum.RuntimeScopeType
	ScopeID          string
	TTLSeconds       int64
	FailedTTLSeconds int64
	KeepShortLogTail bool
	Status           enum.CleanupPolicyStatus
	Meta             value.CommandMeta
}

// RunCleanupBatchInput describes one cleanup worker command.
type RunCleanupBatchInput struct {
	CleanupPolicyID *uuid.UUID
	Limit           int
	LeaseOwner      string
	LeaseUntil      time.Time
	Meta            value.CommandMeta
}

// RunCleanupBatchResult contains cleanup counters and touched slots.
type RunCleanupBatchResult struct {
	ClaimedCount    int
	CleanedCount    int
	FailedCount     int
	AffectedSlotIDs []uuid.UUID
}

// CreateOrUpdatePrewarmPoolInput describes prewarm pool policy upsert.
type CreateOrUpdatePrewarmPoolInput struct {
	PrewarmPoolID  *uuid.UUID
	ScopeType      enum.PrewarmPoolScopeType
	ScopeID        string
	RuntimeProfile string
	FleetScopeID   *uuid.UUID
	TargetSize     int64
	Status         enum.PrewarmPoolStatus
	Meta           value.CommandMeta
}

// ReconcilePrewarmPoolInput describes one prewarm capacity reconciliation.
type ReconcilePrewarmPoolInput struct {
	PrewarmPoolID uuid.UUID
	LeaseOwner    string
	LeaseUntil    time.Time
	Meta          value.CommandMeta
}

// ExtendSlotLeaseInput describes a request to prolong an active slot lease.
type ExtendSlotLeaseInput struct {
	SlotID     uuid.UUID
	LeaseOwner string
	LeaseUntil time.Time
	Meta       value.CommandMeta
}

// ReleaseSlotInput describes a request to release a runtime slot.
type ReleaseSlotInput struct {
	SlotID     uuid.UUID
	LeaseOwner string
	Meta       value.CommandMeta
}

// MarkSlotFailedInput describes a request to move a slot into failed state.
type MarkSlotFailedInput struct {
	SlotID       uuid.UUID
	ErrorCode    string
	ErrorMessage string
	Meta         value.CommandMeta
}

// GetSlotInput describes a slot read request.
type GetSlotInput struct {
	SlotID uuid.UUID
	Meta   value.QueryMeta
}

// ListSlotsInput describes slot list filters.
type ListSlotsInput struct {
	ProjectID      *uuid.UUID
	Statuses       []enum.SlotStatus
	RuntimeProfile string
	FleetScopeID   *uuid.UUID
	AgentRunID     *uuid.UUID
	Page           value.PageRequest
	Meta           value.QueryMeta
}

// ListSlotsResult contains a page of slots.
type ListSlotsResult struct {
	Slots []entity.Slot
	Page  value.PageResult
}
