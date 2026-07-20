package admin

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

type EnsureTurnProcessInput struct {
	TurnID               int64
	ParentTurnID         int64
	ProjectID            int64
	RoleID               int64
	InitiatorUserID      string
	InitiatorUserName    string
	TriggerPostID        string
	MattermostChannelID  string
	MattermostRootPostID string
}

type UpdateWorkClaimInput struct {
	TurnID       int64
	Summary      string
	Domains      []string
	ResourceKeys []string
	Links        []string
	Status       string
}

type RememberMemoryInput struct {
	ProjectID       int64
	Scope           string
	RoleID          int64
	CreatedByRoleID int64
	SourceTurnID    int64
	SourcePostID    string
	Importance      string
	Title           string
	Content         string
}

type SearchMemoryInput struct {
	ProjectID int64
	RoleID    int64
	Query     string
	Limit     int
}

type CreateOwnerAttentionInput struct {
	ProcessRunID   int64
	TurnID         int64
	Severity       string
	Summary        string
	Options        []string
	Recommendation string
	EvidenceLinks  []string
	PauseScope     string
	IdempotencyKey string
}

type CoordinationRepository interface {
	EnsureTurnProcess(ctx context.Context, input EnsureTurnProcessInput) (entity.ProcessContext, error)
	GetTurnProcess(ctx context.Context, turnID int64) (entity.ProcessContext, error)
	GetTurnLineage(ctx context.Context, turnID int64) ([]entity.ProcessLineageStep, error)
	IsRoleCapabilityAllowed(ctx context.Context, turnID int64, projectID int64, roleID int64, capability string) (bool, error)
	IsRoleRelationshipAllowed(ctx context.Context, turnID int64, projectID int64, sourceRoleID int64, action string, targetRoleID int64) (bool, error)
	UpdateWorkClaim(ctx context.Context, input UpdateWorkClaimInput) (entity.WorkClaim, error)
	ListActiveWork(ctx context.Context, processRunID int64, projectID int64, limit int) ([]entity.WorkClaim, error)
	RememberMemory(ctx context.Context, input RememberMemoryInput) (entity.MemoryRecord, error)
	SearchMemory(ctx context.Context, input SearchMemoryInput) ([]entity.MemoryRecord, error)
	CreateOwnerAttention(ctx context.Context, input CreateOwnerAttentionInput) (entity.OwnerAttentionRequest, bool, error)
	SetOwnerAttentionPost(ctx context.Context, id int64, postID string) (entity.OwnerAttentionRequest, error)
	ReconcileProcessRun(ctx context.Context, turnID int64) error
}

type CoordinationPolicyPresetRepository interface {
	ApplyCoordinationPolicyPreset(ctx context.Context, projectID int64, topCoordinatorRoleID int64, waveCoordinatorRoleIDs []int64) error
}
