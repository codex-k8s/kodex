// Package botidentity задаёт interaction-owned PostgreSQL port Agent bot state.
package botidentity

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var (
	ErrNotFound            = errors.New("agent bot identity state is not found")
	ErrIdempotencyConflict = errors.New("agent bot identity semantic idempotency conflict")
	ErrGenerationConflict  = errors.New("agent bot identity generation conflict")
)

type Disposition uint8

const (
	Claimed Disposition = iota + 1
	Replay
	Busy
)

type Repository interface {
	Check(context.Context) error
	ResolveCatalogOffset(context.Context, entity.TeamPrincipal, string, uint32) (uint32, error)
	SaveCatalogPage(context.Context, entity.TeamPrincipal, []entity.AgentMattermostBotIdentity, uint32, uint32, bool, time.Duration) ([]entity.AgentMattermostBotIdentity, string, error)
	ResolveSelector(context.Context, entity.TeamPrincipal, string) (entity.AgentMattermostBotIdentity, error)
	ReserveProviderObject(context.Context, entity.AgentMattermostBotOperation, string) error
	BeginOperation(context.Context, entity.AgentMattermostBotOperation, string, time.Duration, time.Duration) (entity.AgentMattermostBotOperation, Disposition, error)
	GetOperation(context.Context, entity.TeamPrincipal, string, string, string) (entity.AgentMattermostBotOperation, error)
	MarkEffectStarted(context.Context, entity.AgentMattermostBotOperation) (entity.AgentMattermostBotOperation, error)
	MarkMembershipPending(context.Context, entity.AgentMattermostBotOperation, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error)
	DeferRecovery(context.Context, entity.AgentMattermostBotOperation, string, time.Duration) (entity.AgentMattermostBotOperation, error)
	AcceptProvider(context.Context, entity.AgentMattermostBotOperation, entity.AgentMattermostBotIdentity) (entity.AgentMattermostBotOperation, error)
	Finish(context.Context, entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding) error
	MarkRepairRequired(context.Context, entity.AgentMattermostBotOperation, string) error
	ClaimRecovery(context.Context, string, time.Duration) (entity.AgentMattermostBotOperation, bool, error)
	GetBinding(context.Context, entity.TeamPrincipal, string) (entity.AgentMattermostBotBinding, error)
	CloseGeneration(context.Context, entity.AgentMattermostBotOperation, uint64) error
	AdmitRuntimeIdentity(context.Context, entity.TeamPrincipal, string, string, uint64) (entity.AgentMattermostBotIdentity, error)
	ResolveRuntimeIdentity(context.Context, entity.TeamPrincipal, string, string) (entity.AgentMattermostBotIdentity, error)
}
