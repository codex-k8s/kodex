// Package runtime defines persistence ports owned by the runtime-manager domain.
package runtime

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

// Repository is the domain persistence contract for runtime-manager use cases.
type Repository interface {
	// Ping checks that the runtime database is reachable.
	Ping(ctx context.Context) error
	// GetCommandResult returns the aggregate linked to an idempotent command.
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	// CreateSlot stores a new slot, its command result and its outbox event atomically.
	CreateSlot(ctx context.Context, slot entity.Slot, event entity.OutboxEvent, result entity.CommandResult) error
	// ClaimReusableSlot atomically reserves one safe prewarmed or ready slot for deterministic reuse.
	ClaimReusableSlot(ctx context.Context, filter query.ReusableSlotFilter, recordFactory SlotReuseRecordFactory) (entity.Slot, error)
	// UpdateSlot stores an existing slot mutation, its outbox event and optional command result atomically.
	UpdateSlot(ctx context.Context, slot entity.Slot, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetSlot returns one slot by id.
	GetSlot(ctx context.Context, id uuid.UUID) (entity.Slot, error)
	// ListSlots returns slots matching the filter and page.
	ListSlots(ctx context.Context, filter query.SlotFilter) ([]entity.Slot, query.PageResult, error)
	// PrepareRuntime creates a slot, starts materialization and stores both events and command result atomically.
	PrepareRuntime(ctx context.Context, slot entity.Slot, materialization entity.WorkspaceMaterialization, slotEvent entity.OutboxEvent, workspaceEvent entity.OutboxEvent, result entity.CommandResult) error
	// CreateWorkspaceMaterialization starts materialization in an existing slot atomically with the slot state update.
	CreateWorkspaceMaterialization(ctx context.Context, slot entity.Slot, materialization entity.WorkspaceMaterialization, previousSlotVersion int64, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateWorkspaceMaterialization stores materialization progress, optional slot mutation, optional event and command result atomically.
	UpdateWorkspaceMaterialization(ctx context.Context, slot entity.Slot, materialization entity.WorkspaceMaterialization, previousSlotVersion int64, previousMaterializationVersion int64, event *entity.OutboxEvent, result entity.CommandResult) error
	// GetWorkspaceMaterialization returns one materialization attempt by id.
	GetWorkspaceMaterialization(ctx context.Context, id uuid.UUID) (entity.WorkspaceMaterialization, error)
	// ListWorkspaceMaterializations returns materialization attempts matching the filter and page.
	ListWorkspaceMaterializations(ctx context.Context, filter query.WorkspaceMaterializationFilter) ([]entity.WorkspaceMaterialization, query.PageResult, error)
	// PrepareBuildContext stores or reuses a build context request and records command idempotency.
	PrepareBuildContext(ctx context.Context, buildContext entity.BuildContext, resultFactory BuildContextCommandResultFactory) (entity.BuildContext, error)
	// UpdateBuildContext stores build context progress and command result atomically.
	UpdateBuildContext(ctx context.Context, buildContext entity.BuildContext, previousVersion int64, result entity.CommandResult) error
	// GetBuildContext returns one build context by id.
	GetBuildContext(ctx context.Context, id uuid.UUID) (entity.BuildContext, error)
	// GetBuildContextByFingerprint returns one build context by deterministic context fingerprint.
	GetBuildContextByFingerprint(ctx context.Context, fingerprint string) (entity.BuildContext, error)
	// ListRunnableBuildContexts returns non-terminal build contexts ready for runtime-owned materialization.
	ListRunnableBuildContexts(ctx context.Context, limit int) ([]entity.BuildContext, error)
	// CreateJob stores a new platform job, its event and command result atomically.
	CreateJob(ctx context.Context, job entity.Job, event entity.OutboxEvent, result entity.CommandResult) error
	// ClaimRunnableJob atomically leases one runnable job and stores its start event and command result.
	ClaimRunnableJob(ctx context.Context, filter query.JobClaimFilter, recordFactory JobClaimRecordFactory) (entity.Job, error)
	// UpdateJob stores a job mutation, changed steps, artifact refs, optional event and command result atomically.
	UpdateJob(ctx context.Context, job entity.Job, previousVersion int64, steps []entity.JobStep, refs []entity.RuntimeArtifactRef, event *entity.OutboxEvent, result entity.CommandResult) error
	// GetJob returns one platform job by id.
	GetJob(ctx context.Context, id uuid.UUID) (entity.Job, error)
	// ListJobs returns platform jobs matching the filter and page.
	ListJobs(ctx context.Context, filter query.JobFilter) ([]entity.Job, query.PageResult, error)
	// RecordRuntimeArtifactRef stores one reference to an external runtime artifact.
	RecordRuntimeArtifactRef(ctx context.Context, ref entity.RuntimeArtifactRef, result entity.CommandResult) error
	// GetRuntimeArtifactRef returns one external runtime artifact reference by id.
	GetRuntimeArtifactRef(ctx context.Context, id uuid.UUID) (entity.RuntimeArtifactRef, error)
	// ListRuntimeArtifactRefs returns external runtime artifact references matching the filter and page.
	ListRuntimeArtifactRefs(ctx context.Context, filter query.RuntimeArtifactRefFilter) ([]entity.RuntimeArtifactRef, query.PageResult, error)
	// CreateCleanupPolicy stores a cleanup policy and command result atomically.
	CreateCleanupPolicy(ctx context.Context, policy entity.CleanupPolicy, result entity.CommandResult) error
	// UpdateCleanupPolicy stores cleanup policy changes and command result atomically.
	UpdateCleanupPolicy(ctx context.Context, policy entity.CleanupPolicy, previousVersion int64, result entity.CommandResult) error
	// GetCleanupPolicy returns one cleanup policy by id.
	GetCleanupPolicy(ctx context.Context, id uuid.UUID) (entity.CleanupPolicy, error)
	// RunCleanupBatch claims expired runtime objects, records cleanup events and command result atomically.
	RunCleanupBatch(ctx context.Context, filter query.CleanupBatchFilter, recordFactory CleanupBatchRecordFactory) (CleanupBatchResult, error)
	// CreatePrewarmPool stores a prewarm pool and command result atomically.
	CreatePrewarmPool(ctx context.Context, pool entity.PrewarmPool, result entity.CommandResult) error
	// UpdatePrewarmPool stores prewarm pool changes and command result atomically.
	UpdatePrewarmPool(ctx context.Context, pool entity.PrewarmPool, previousVersion int64, result entity.CommandResult) error
	// GetPrewarmPool returns one prewarm pool by id.
	GetPrewarmPool(ctx context.Context, id uuid.UUID) (entity.PrewarmPool, error)
	// ReconcilePrewarmPool changes actual prewarmed slots toward target capacity atomically.
	ReconcilePrewarmPool(ctx context.Context, filter query.PrewarmPoolReconcileFilter, recordFactory PrewarmPoolReconcileRecordFactory) (entity.PrewarmPool, error)
	// ClaimOutboxEvents leases unpublished outbox events for delivery.
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	// MarkOutboxEventPublished marks a leased outbox event as published.
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	// MarkOutboxEventFailed schedules a leased outbox event for retry.
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}

// Clock provides deterministic time for domain commands and tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides aggregate and event identifiers for domain commands.
type IDGenerator interface {
	New() uuid.UUID
}

// JobClaimRecordFactory builds the event and command result for the concrete job claimed inside repository transaction.
type JobClaimRecordFactory func(entity.Job) (entity.OutboxEvent, entity.CommandResult, error)

// SlotReuseRecordFactory builds event and command result for the concrete slot claimed inside repository transaction.
type SlotReuseRecordFactory func(entity.Slot) (entity.OutboxEvent, entity.CommandResult, error)

// BuildContextCommandResultFactory builds a command result for the concrete build context persisted by repository.
type BuildContextCommandResultFactory func(entity.BuildContext) (entity.CommandResult, error)

// CleanupBatchResult describes actual cleanup mutations done by repository.
type CleanupBatchResult struct {
	ClaimedCount    int
	CleanedCount    int
	FailedCount     int
	AffectedSlotIDs []uuid.UUID
	CleanedSlots    []entity.Slot
	FailedSlots     []entity.Slot
}

// CleanupBatchRecordFactory builds events and command result after repository selected cleanup objects.
type CleanupBatchRecordFactory func(CleanupBatchResult) ([]entity.OutboxEvent, entity.CommandResult, error)

// PrewarmPoolReconcileState contains locked pool and current capacity snapshot.
type PrewarmPoolReconcileState struct {
	Pool        entity.PrewarmPool
	CurrentSize int64
	ExcessSlots []entity.Slot
}

// PrewarmPoolReconcileRecord contains pool and slot mutations for one reconcile command.
type PrewarmPoolReconcileRecord struct {
	Pool         entity.PrewarmPool
	CreatedSlots []entity.Slot
	CleanupSlots []entity.Slot
}

// PrewarmPoolReconcileRecordFactory builds pool, slot, event and command-result records inside repository transaction.
type PrewarmPoolReconcileRecordFactory func(PrewarmPoolReconcileState) (PrewarmPoolReconcileRecord, []entity.OutboxEvent, entity.CommandResult, error)
