package instructions

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

var ErrManagedByGit = errors.New("agent instruction configuration is managed by git")

type UpsertAgentInput struct {
	ProjectID         int64
	Name              string
	RoleType          string
	Description       string
	PromptTemplate    string
	PromptMode        string
	GitHubAccountName string
	OpenAIAccountName string
	KubernetesAccess  string
	SandboxMode       string
	ConfigOverlay     string
	AdvancedSettings  string
	Enabled           bool
	BotIdentity       string
	ActorRef          string
}

type UpsertAgentResult struct {
	Snapshot entity.AgentInstructionSnapshot
	Legacy   entity.AgentRole
	Created  bool
}

type DetachInstructionSetInput struct {
	LegacyAgentRoleID int64
	ActorRef          string
}

type Repository interface {
	UpsertAgent(ctx context.Context, input UpsertAgentInput) (UpsertAgentResult, error)
	GetAgentInstructionSnapshot(ctx context.Context, legacyAgentRoleID int64) (entity.AgentInstructionSnapshot, error)
	DetachInstructionSet(ctx context.Context, input DetachInstructionSetInput) (entity.AgentInstructionSnapshot, error)
}
