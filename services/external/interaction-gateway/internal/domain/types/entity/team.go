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

// MattermostOwnerObservation подтверждает fresh provider user readback без
// сохранения или выдачи provider payload.
type MattermostOwnerObservation struct {
	ProviderObjectRef string
	SnapshotSHA256    string
	ObservedAt        time.Time
}

type MattermostReadinessBinding struct {
	Principal      TeamPrincipal
	ProviderTeamID string
}

// WorkspaceMattermostMapping — внутренняя проекция авторитетного mapping.
// ProviderTeamID никогда не передаётся наружу и заменяется opaque selector-ом.
type WorkspaceMattermostMapping struct {
	ID                       string
	Name                     string
	Version                  uint64
	Generation               uint64
	State                    string
	ProviderTeamID           string
	ProviderEffectVersion    uint64
	ProviderEffectGeneration uint64
	ProviderObservedAt       time.Time
	UpdatedAt                time.Time
}

type WorkspaceMattermostBinding struct {
	Mapping WorkspaceMattermostMapping
	Team    MattermostTeam
}

type WorkspaceMappingOperation struct {
	ID                 string
	Principal          TeamPrincipal
	Action             string
	IdempotencyKey     string
	RequestSHA256      string
	MappingID          string
	ExpectedVersion    uint64
	ExpectedGeneration uint64
	DisplayName        string
	Team               MattermostTeam
	State              string
	EffectGeneration   uint64
	ReceiptID          string
	Fence              uint64
	LeaseToken         string
	FailureCode        string
	RetryNotBefore     time.Time
	Result             WorkspaceMattermostMapping
	CreatedAt          time.Time
	UpdatedAt          time.Time
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
