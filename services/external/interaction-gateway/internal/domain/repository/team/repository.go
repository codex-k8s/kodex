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
	ErrCreateFenceConflict = errors.New("mattermost team create fence conflict")
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
	BeginCreate(context.Context, entity.MattermostTeamOperation, string, time.Duration, time.Duration) (entity.MattermostTeamOperation, CreateDisposition, error)
	GetCreateOperation(context.Context, entity.TeamPrincipal, string) (entity.MattermostTeamOperation, error)
	MarkEffectStarted(context.Context, entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error)
	DeferCreateRecovery(context.Context, entity.MattermostTeamOperation, string, time.Duration) (entity.MattermostTeamOperation, error)
	MarkRepairRequired(context.Context, entity.MattermostTeamOperation, string) error
	AcceptProvider(context.Context, entity.MattermostTeamOperation, entity.MattermostTeam, string, time.Duration) (entity.MattermostTeamOperation, error)
	ClaimRecovery(context.Context, string, time.Duration) (entity.MattermostTeamOperation, bool, error)
	AdvanceProviderGeneration(context.Context, entity.TeamPrincipal) (uint64, error)
	BeginMapping(context.Context, entity.WorkspaceMappingOperation, string, time.Duration, time.Duration) (entity.WorkspaceMappingOperation, MappingDisposition, error)
	GetMappingOperation(context.Context, entity.TeamPrincipal, string, string) (entity.WorkspaceMappingOperation, error)
	PrepareMappingAttempt(context.Context, entity.WorkspaceMappingOperation, entity.MattermostTeam, time.Duration) (entity.WorkspaceMappingOperation, error)
	DeferMappingRecovery(context.Context, entity.WorkspaceMappingOperation, string, time.Duration) (entity.WorkspaceMappingOperation, error)
	MarkMappingTerminal(context.Context, entity.WorkspaceMappingOperation, entity.WorkspaceMattermostMapping, []entity.MattermostRuntimeRoute) error
	ReconcileRuntimeRoutes(context.Context, entity.TeamPrincipal, entity.WorkspaceMattermostMapping, []entity.MattermostRuntimeRoute) error
	MarkMappingRepairRequired(context.Context, entity.WorkspaceMappingOperation, string) error
	ClaimMappingRecovery(context.Context, string, time.Duration) (entity.WorkspaceMappingOperation, bool, error)
	ResolveRuntimeRoute(context.Context, string, string) (entity.MattermostRuntimeRoute, error)
	ResolveRuntimeDelivery(context.Context, string, string, string) (entity.MattermostRuntimeRoute, error)
	ListRuntimeRoutes(context.Context) ([]entity.MattermostRuntimeRoute, error)
	GetRuntimeAdmission(context.Context, entity.TeamPrincipal, string) (entity.MattermostRuntimeRoute, error)
}
