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

type CreateAgentRunInput struct {
	RunID               string
	ProfileName         string
	Role                string
	Provider            string
	Owner               string
	Name                string
	BaseBranch          string
	HeadBranch          string
	Status              string
	KubernetesNamespace string
	JobName             string
	PVCName             string
	Summary             string
}

type UpdateAgentRunArtifactsInput struct {
	RunID  string
	Status string
	PRURL  string
}

type Repository interface {
	UpsertRepository(ctx context.Context, input UpsertRepositoryInput) (entity.Repository, bool, error)
	ListRepositories(ctx context.Context, limit int) ([]entity.Repository, error)
	ListAgentProfiles(ctx context.Context) ([]entity.AgentProfile, error)
	CreateAgentRun(ctx context.Context, input CreateAgentRunInput) (entity.AgentRun, error)
	GetAgentRun(ctx context.Context, runID string) (entity.AgentRun, error)
	UpdateAgentRunArtifacts(ctx context.Context, input UpdateAgentRunArtifactsInput) (entity.AgentRun, error)
	RecordAuditEvent(ctx context.Context, input AuditEventInput) error
}
