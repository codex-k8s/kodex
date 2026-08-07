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
	ProviderTeamID          string
	Selector                string
	DisplayName             string
	Slug                    string
	Status                  enum.MattermostTeamStatus
	ProviderSnapshotSHA256  string
	ProviderCausalitySHA256 string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	ObservedAt              time.Time
}

// MattermostOwnerObservation подтверждает fresh provider user readback без
// сохранения или выдачи provider payload.
type MattermostOwnerObservation struct {
	ProviderObjectRef string
	SnapshotSHA256    string
	ObservedAt        time.Time
}

type MattermostReadinessBinding struct {
	Principal TeamPrincipal
}

// MattermostRuntimeRoute — durable joined projection exact owner mapping и
// provider Team/channel. TemplateKey связывает только неизменяемую route policy;
// Team authority всегда задают MappingVersion/MappingGeneration.
type MattermostRuntimeRoute struct {
	TemplateKey            string
	Principal              TeamPrincipal
	MappingID              string
	MappingVersion         uint64
	MappingGeneration      uint64
	MappingDigestSHA256    string
	ProviderTeamID         string
	ProviderSnapshotSHA256 string
	Boundary               Boundary
	OwnerDelivery          bool
	RouteDigestSHA256      string
	CreatedAt              time.Time
	UpdatedAt              time.Time
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
	Mapping   WorkspaceMattermostMapping
	Team      MattermostTeam
	Operation WorkspaceMappingOperation
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
	RecoveryDeadline   time.Time
	Result             WorkspaceMattermostMapping
	CreateOperationID  string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type MattermostTeamCreateIntent struct {
	DisplayName         string
	Slug                string
	IdempotencyKey      string
	RequestSHA256       string
	ProviderCorrelation string
}

// MattermostTeamOperation является durable interaction-owned provider
// checkpoint, но не копией авторитетного Workspace mapping control-plane.
type MattermostTeamOperation struct {
	ID                      string
	Principal               TeamPrincipal
	Intent                  MattermostTeamCreateIntent
	State                   enum.MattermostTeamOperationState
	Team                    MattermostTeam
	ProviderReceiptSHA256   string
	ProviderCausalitySHA256 string
	ProviderGeneration      uint64
	FailureCode             string
	Fence                   uint64
	LeaseToken              string
	EffectStartedAt         time.Time
	RetryNotBefore          time.Time
	RecoveryDeadline        time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}
