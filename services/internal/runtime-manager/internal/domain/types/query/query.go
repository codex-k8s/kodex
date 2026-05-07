// Package query contains runtime-manager read filters and paging helpers.
package query

import (
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
