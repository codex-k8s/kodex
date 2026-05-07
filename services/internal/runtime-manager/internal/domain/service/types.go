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
