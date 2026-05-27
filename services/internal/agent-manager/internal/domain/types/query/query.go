// Package query contains agent-manager query filters.
package query

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type CommandIdentity struct {
	CommandID      *uuid.UUID
	IdempotencyKey string
	Operation      string
	Actor          value.Actor
}

type FlowFilter struct {
	Scope  value.ScopeRef
	Status *enum.FlowStatus
	Page   value.PageRequest
}

type FlowVersionFilter struct {
	FlowID uuid.UUID
	Status *enum.FlowVersionStatus
	Page   value.PageRequest
}

type RoleProfileFilter struct {
	Scope  value.ScopeRef
	Kind   *enum.RoleKind
	Status *enum.RoleStatus
	Page   value.PageRequest
}

type PromptTemplateFilter struct {
	RoleProfileID uuid.UUID
	Kind          *enum.PromptKind
	Page          value.PageRequest
}

type PromptTemplateVersionFilter struct {
	RoleProfileID uuid.UUID
	Kind          *enum.PromptKind
	Status        *enum.PromptVersionStatus
	Page          value.PageRequest
}

type AgentRunFilter struct {
	SessionID           uuid.UUID
	RoleProfileID       uuid.UUID
	Status              *enum.AgentRunStatus
	ProviderWorkItemRef string
	Page                value.PageRequest
}

type AcceptanceResultFilter struct {
	SessionID uuid.UUID
	RunID     uuid.UUID
	StageID   uuid.UUID
	Status    *enum.AcceptanceStatus
	Page      value.PageRequest
}

type AgentActivityFilter struct {
	SessionID    uuid.UUID
	RunID        uuid.UUID
	ActivityKind *enum.AgentActivityKind
	Status       *enum.AgentActivityStatus
	Page         value.PageRequest
}

type HumanGateFilter struct {
	SessionID uuid.UUID
	RunID     uuid.UUID
	StageID   uuid.UUID
	Status    *enum.HumanGateStatus
	Outcome   *enum.HumanGateOutcome
	Page      value.PageRequest
}
