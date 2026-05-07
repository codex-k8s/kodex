package service

import (
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
	PreferredFleetScopeID *uuid.UUID
	Meta                  value.CommandMeta
}

// PrepareRuntimeInput describes a facade request to reserve a slot and start workspace preparation.
type PrepareRuntimeInput struct {
	AgentRunID            *uuid.UUID
	RuntimeProfile        string
	RuntimeMode           enum.RuntimeMode
	WorkspacePolicy       WorkspacePolicyInput
	PreferredFleetScopeID *uuid.UUID
	Meta                  value.CommandMeta
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
