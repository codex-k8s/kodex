package mattermost

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var (
	ErrTeamNotFound  = errors.New("mattermost team is not found")
	ErrTeamConflict  = errors.New("mattermost team conflicts with provider state")
	ErrTeamForbidden = errors.New("mattermost team access is forbidden")
)

// TeamClient отделяет owner catalog/create/readback от существующего
// inbound/delivery transport и не расширяет его интерфейс.
type TeamClient interface {
	CheckTeamLifecycle(context.Context) error
	TeamReadinessBindings() []entity.MattermostReadinessBinding
	ReadOwner(context.Context, entity.TeamPrincipal) (entity.MattermostOwnerObservation, error)
	ListTeams(context.Context, entity.TeamPrincipal, uint32, uint32) ([]entity.MattermostTeam, bool, error)
	CreateTeam(context.Context, entity.TeamPrincipal, entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error)
	RecoverCreatedTeam(context.Context, entity.TeamPrincipal, entity.MattermostTeamCreateIntent) (entity.MattermostTeam, error)
	ReadTeam(context.Context, entity.TeamPrincipal, string) (entity.MattermostTeam, error)
}
