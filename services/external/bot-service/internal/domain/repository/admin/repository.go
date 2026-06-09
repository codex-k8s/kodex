package admin

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

type UpsertRepositoryInput struct {
	Provider          string
	Owner             string
	Name              string
	DefaultBranch     string
	MattermostChannel string
}

type AuditEventInput struct {
	EventType    string
	ActorUserID  string
	ActorUser    string
	ResourceType string
	ResourceName string
	Summary      string
}

type Repository interface {
	UpsertRepository(ctx context.Context, input UpsertRepositoryInput) (entity.Repository, bool, error)
	ListRepositories(ctx context.Context, limit int) ([]entity.Repository, error)
	ListAgentProfiles(ctx context.Context) ([]entity.AgentProfile, error)
	RecordAuditEvent(ctx context.Context, input AuditEventInput) error
}
