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
	FlowID              string
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

type CreateAgentFlowInput struct {
	FlowID      string
	Status      string
	Provider    string
	Owner       string
	Name        string
	BaseBranch  string
	HeadBranch  string
	Title       string
	Task        string
	Attempt     int
	MaxAttempts int
	Summary     string
}

type UpdateAgentFlowInput struct {
	FlowID                string
	Status                string
	PRURL                 string
	PRNumber              int
	Attempt               int
	CurrentDeveloperRunID string
	CurrentReviewerRunID  string
	Summary               string
}

type UpdateAgentRunArtifactsInput struct {
	RunID  string
	Status string
	PRURL  string
}

type UpsertOpenAIAccountInput struct {
	Name           string
	CredentialName string
	SecretRef      string
	Status         string
}

type UpdateOpenAIAccountStatusInput struct {
	Name      string
	SecretRef string
	Status    string
}

type UpsertAgentPromptTemplateInput struct {
	ProfileName string
	TemplateKey string
	Body        string
}

type Repository interface {
	UpsertRepository(ctx context.Context, input UpsertRepositoryInput) (entity.Repository, bool, error)
	ListRepositories(ctx context.Context, limit int) ([]entity.Repository, error)
	ListAgentProfiles(ctx context.Context) ([]entity.AgentProfile, error)
	ListAgentPromptTemplates(ctx context.Context, profileName string) ([]entity.AgentPromptTemplate, error)
	GetAgentPromptTemplate(ctx context.Context, profileName string, templateKey string) (entity.AgentPromptTemplate, error)
	UpsertAgentPromptTemplate(ctx context.Context, input UpsertAgentPromptTemplateInput) (entity.AgentPromptTemplate, bool, error)
	UpsertOpenAIAccount(ctx context.Context, input UpsertOpenAIAccountInput) (entity.OpenAIAccount, bool, error)
	ListOpenAIAccounts(ctx context.Context, limit int) ([]entity.OpenAIAccount, error)
	GetOpenAIAccount(ctx context.Context, name string) (entity.OpenAIAccount, error)
	UpdateOpenAIAccountStatus(ctx context.Context, input UpdateOpenAIAccountStatusInput) (entity.OpenAIAccount, error)
	GetGitHubAccount(ctx context.Context, name string) (entity.GitHubAccount, error)
	CreateAgentFlow(ctx context.Context, input CreateAgentFlowInput) (entity.AgentFlow, bool, error)
	GetAgentFlow(ctx context.Context, flowID string) (entity.AgentFlow, error)
	UpdateAgentFlow(ctx context.Context, input UpdateAgentFlowInput) (entity.AgentFlow, error)
	CreateAgentRun(ctx context.Context, input CreateAgentRunInput) (entity.AgentRun, error)
	GetAgentRun(ctx context.Context, runID string) (entity.AgentRun, error)
	ListAgentRunsByFlowID(ctx context.Context, flowID string) ([]entity.AgentRun, error)
	UpdateAgentRunArtifacts(ctx context.Context, input UpdateAgentRunArtifactsInput) (entity.AgentRun, error)
	RecordAuditEvent(ctx context.Context, input AuditEventInput) error
}
