// Package team задаёт interaction-owned PostgreSQL port Team provider state.
package team

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var (
	ErrNotFound            = errors.New("mattermost team provider state is not found")
	ErrIdempotencyConflict = errors.New("mattermost team semantic idempotency conflict")
)

type CreateDisposition uint8

const (
	CreateClaimed CreateDisposition = iota + 1
	CreateReplay
	CreateBusy
)

type MappingDisposition uint8

const (
	MappingClaimed MappingDisposition = iota + 1
	MappingReplay
	MappingBusy
)

type Repository interface {
	Check(context.Context) error
	ResolveCatalogOffset(context.Context, entity.TeamPrincipal, string, uint32) (uint32, error)
	SaveCatalogPage(context.Context, entity.TeamPrincipal, []entity.MattermostTeam, uint32, uint32, bool, time.Duration) ([]entity.MattermostTeam, string, error)
	ResolveSelector(context.Context, entity.TeamPrincipal, string) (string, error)
	RefreshSelector(context.Context, entity.TeamPrincipal, entity.MattermostTeam, time.Duration) (entity.MattermostTeam, error)
	BeginCreate(context.Context, entity.MattermostTeamOperation, string, time.Duration) (entity.MattermostTeamOperation, CreateDisposition, error)
	MarkEffectStarted(context.Context, entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error)
	MarkAmbiguous(context.Context, entity.MattermostTeamOperation, string, time.Time) error
	MarkRepairRequired(context.Context, entity.MattermostTeamOperation, string) error
	AcceptProvider(context.Context, entity.MattermostTeamOperation, entity.MattermostTeam, string, time.Duration) (entity.MattermostTeamOperation, error)
	ClaimRecovery(context.Context, string, time.Duration) (entity.MattermostTeamOperation, bool, error)
	AdvanceProviderGeneration(context.Context, entity.TeamPrincipal) (uint64, error)
	BeginMapping(context.Context, entity.WorkspaceMappingOperation, string, time.Duration) (entity.WorkspaceMappingOperation, MappingDisposition, error)
	RefreshMappingReceipt(context.Context, entity.WorkspaceMappingOperation) (entity.WorkspaceMappingOperation, error)
	MarkMappingAmbiguous(context.Context, entity.WorkspaceMappingOperation, string, time.Time) error
	MarkMappingTerminal(context.Context, entity.WorkspaceMappingOperation, entity.WorkspaceMattermostMapping) error
	MarkMappingRepairRequired(context.Context, entity.WorkspaceMappingOperation, string) error
	ClaimMappingRecovery(context.Context, string, time.Duration) (entity.WorkspaceMappingOperation, bool, error)
}
