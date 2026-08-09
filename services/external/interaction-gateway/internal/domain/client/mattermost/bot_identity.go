package mattermost

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var (
	ErrBotNotFound        = errors.New("mattermost bot identity is not found")
	ErrBotConflict        = errors.New("mattermost bot identity conflicts with provider state")
	ErrBotForbidden       = errors.New("mattermost bot identity access is forbidden")
	ErrBotAmbiguousEffect = errors.New("mattermost bot identity effect is ambiguous")
)

type BotIdentityClient interface {
	CheckBotIdentityLifecycle(context.Context) error
	ListBotIdentities(context.Context, entity.TeamPrincipal, string, uint32, uint32) ([]entity.AgentMattermostBotIdentity, bool, error)
	CreateBotIdentity(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotCreateIntent, string) (entity.AgentMattermostBotIdentity, error)
	RecoverCreatedBotIdentity(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotCreateIntent, string) (entity.AgentMattermostBotIdentity, error)
	ReadBotIdentity(context.Context, entity.TeamPrincipal, string, string) (entity.AgentMattermostBotIdentity, error)
	EnsureBotTeamMembership(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, error)
	CreateBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) (string, string, error)
	ResolveBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) (string, bool, error)
	RecoverBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) (string, bool, error)
	RevokeBotAccessToken(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity) (bool, error)
	RevokeBotIdentity(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotIdentity, bool, error)
	VerifyRuntimeBotCredential(context.Context, entity.TeamPrincipal, entity.AgentMattermostBotIdentity, string) error
}
