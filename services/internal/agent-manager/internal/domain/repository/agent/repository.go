// Package agent defines agent-manager persistence ports.
package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type Repository interface {
	CreateFlowWithResult(ctx context.Context, flow entity.Flow, result entity.CommandResult) error
	UpdateFlowWithResult(ctx context.Context, flow entity.Flow, previousVersion int64, result entity.CommandResult) error
	GetFlow(ctx context.Context, id uuid.UUID) (entity.Flow, error)
	ListFlows(ctx context.Context, filter query.FlowFilter) ([]entity.Flow, value.PageResult, error)
	CreateFlowVersionWithResult(ctx context.Context, version entity.FlowVersion, result entity.CommandResult) (entity.FlowVersion, error)
	ActivateFlowVersionWithResult(ctx context.Context, flow entity.Flow, previousFlowVersion int64, version entity.FlowVersion, result entity.CommandResult, event entity.OutboxEvent) error
	GetFlowVersion(ctx context.Context, id uuid.UUID) (entity.FlowVersion, error)
	ListFlowVersions(ctx context.Context, filter query.FlowVersionFilter) ([]entity.FlowVersion, value.PageResult, error)
	CreateRoleProfileWithResult(ctx context.Context, role entity.RoleProfile, result entity.CommandResult) error
	UpdateRoleProfileWithResult(ctx context.Context, role entity.RoleProfile, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error
	GetRoleProfile(ctx context.Context, id uuid.UUID) (entity.RoleProfile, error)
	ListRoleProfiles(ctx context.Context, filter query.RoleProfileFilter) ([]entity.RoleProfile, value.PageResult, error)
	CreatePromptTemplateWithResult(ctx context.Context, template entity.PromptTemplate, result entity.CommandResult) error
	GetPromptTemplate(ctx context.Context, id uuid.UUID) (entity.PromptTemplate, error)
	ListPromptTemplates(ctx context.Context, filter query.PromptTemplateFilter) ([]entity.PromptTemplate, value.PageResult, error)
	CreatePromptTemplateVersionWithResult(ctx context.Context, newTemplate *entity.PromptTemplate, version entity.PromptTemplateVersion, result entity.CommandResult) (entity.PromptTemplateVersion, error)
	ActivatePromptTemplateVersionWithResult(ctx context.Context, template entity.PromptTemplate, previousTemplateVersion int64, version entity.PromptTemplateVersion, result entity.CommandResult, event entity.OutboxEvent) error
	GetPromptTemplateVersion(ctx context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error)
	ListPromptTemplateVersions(ctx context.Context, filter query.PromptTemplateVersionFilter) ([]entity.PromptTemplateVersion, value.PageResult, error)
	CreateAgentSessionWithResult(ctx context.Context, session entity.AgentSession, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateAgentSessionWithResult(ctx context.Context, session entity.AgentSession, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	GetAgentSession(ctx context.Context, id uuid.UUID) (entity.AgentSession, error)
	FindActiveAgentSessionByProviderWorkItem(ctx context.Context, scope value.ScopeRef, providerWorkItemRef string) (entity.AgentSession, error)
	CreateAgentRunWithResult(ctx context.Context, run entity.AgentRun, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateAgentRunWithResult(ctx context.Context, run entity.AgentRun, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error
	GetAgentRun(ctx context.Context, id uuid.UUID) (entity.AgentRun, error)
	ListAgentRuns(ctx context.Context, filter query.AgentRunFilter) ([]entity.AgentRun, value.PageResult, error)
	CreateSessionStateSnapshotWithResult(ctx context.Context, snapshot entity.AgentSessionStateSnapshot, session entity.AgentSession, previousSessionVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	GetSessionStateSnapshot(ctx context.Context, id uuid.UUID) (entity.AgentSessionStateSnapshot, error)
	CreateAcceptanceResultWithResult(ctx context.Context, acceptance entity.AcceptanceResult, result entity.CommandResult, event entity.OutboxEvent) error
	UpdateAcceptanceResultWithResult(ctx context.Context, acceptance entity.AcceptanceResult, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error
	GetAcceptanceResult(ctx context.Context, id uuid.UUID) (entity.AcceptanceResult, error)
	ListAcceptanceResults(ctx context.Context, filter query.AcceptanceResultFilter) ([]entity.AcceptanceResult, value.PageResult, error)
	CreateFollowUpIntentWithResult(ctx context.Context, intent entity.FollowUpIntent, result entity.CommandResult, event entity.OutboxEvent) error
	GetFollowUpIntent(ctx context.Context, id uuid.UUID) (entity.FollowUpIntent, error)
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	RecordCommandResult(ctx context.Context, result entity.CommandResult) error
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() uuid.UUID
}
