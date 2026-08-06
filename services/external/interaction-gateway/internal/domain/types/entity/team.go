package entity

import (
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
)

// TeamPrincipal приходит только из проверенного authorization context.
type TeamPrincipal struct {
	ActorID        string
	OrganizationID string
	ProjectID      string
}

// MattermostTeam содержит raw provider identity только внутри gateway.
// Transport DTO всегда использует Selector.
type MattermostTeam struct {
	ProviderTeamID         string
	Selector               string
	DisplayName            string
	Slug                   string
	Status                 enum.MattermostTeamStatus
	ProviderSnapshotSHA256 string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ObservedAt             time.Time
}

type MattermostTeamCreateIntent struct {
	DisplayName    string
	Slug           string
	IdempotencyKey string
	RequestSHA256  string
}

// MattermostTeamOperation является durable interaction-owned provider
// checkpoint, но не копией авторитетного Workspace mapping control-plane.
type MattermostTeamOperation struct {
	ID                    string
	Principal             TeamPrincipal
	Intent                MattermostTeamCreateIntent
	State                 enum.MattermostTeamOperationState
	Team                  MattermostTeam
	ProviderReceiptSHA256 string
	ProviderGeneration    uint64
	FailureCode           string
	Fence                 uint64
	LeaseToken            string
	EffectStartedAt       time.Time
	RetryNotBefore        time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}
