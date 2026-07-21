package admin

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

var (
	ErrNotFound                    = errors.New("admin repository item not found")
	ErrClusterAdminAdmissionDenied = errors.New("cluster-admin assignment is not present in the server-side profile")
)

type UpsertRepositoryInput struct {
	Provider          string
	Owner             string
	Name              string
	DefaultBranch     string
	GitHubAccountName string
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
	FlowID               string
	Status               string
	Provider             string
	Owner                string
	Name                 string
	BaseBranch           string
	HeadBranch           string
	Title                string
	Task                 string
	Attempt              int
	MaxAttempts          int
	DeveloperProfileName string
	ReviewerProfileName  string
	FlowPreset           string
	OwnerUserID          string
	OwnerUser            string
	ActionToken          string
	Summary              string
}

type UpdateAgentFlowInput struct {
	FlowID                string
	Status                string
	PRURL                 string
	PRNumber              int
	Attempt               int
	CurrentDeveloperRunID string
	CurrentReviewerRunID  string
	OwnerUserID           string
	OwnerUser             string
	ControlChannelID      string
	ControlPostID         string
	ActionToken           string
	OwnerDecision         string
	Summary               string
}

type UpdateAgentRunArtifactsInput struct {
	RunID  string
	Status string
	PRURL  string
}

type UpsertAgentProfileInput struct {
	Name              string
	Role              string
	Description       string
	Enabled           bool
	OpenAIAccountName string
	GitHubAccountName string
	KubernetesAccess  string
	SandboxMode       string
	ConfigOverlay     string
}

type UpsertOpenAIAccountInput struct {
	Name           string
	CredentialName string
	SecretRef      string
	Status         string
}

type UpsertGitHubAccountInput struct {
	Name           string
	CredentialName string
	SecretRef      string
	Username       string
	Email          string
	Scopes         string
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

type UpgradeAgentPromptSeedInput struct {
	ProfileName  string
	TemplateKey  string
	PreviousBody string
	Body         string
	RoleNames    []string
	RoleTypes    []string
}

type UpgradeAgentPromptSeedResult struct {
	TemplatesUpdated int
	RolesUpdated     int
}

type AgentPromptSeedUpgradeRepository interface {
	UpgradeUnmodifiedAgentPromptSeed(ctx context.Context, input UpgradeAgentPromptSeedInput) (UpgradeAgentPromptSeedResult, error)
}

type UpsertProjectInput struct {
	Name              string
	Slug              string
	MattermostTeamID  string
	GitHubAccountName string
	GitHubOwner       string
	GitHubOwnerType   string
	Description       string
	AdvancedSettings  string
}

type UpsertProjectRepositoryInput struct {
	ProjectID    int64
	RepositoryID int64
	IsDefault    bool
	Metadata     string
}

type UpsertProjectRuntimeVariableInput struct {
	ProjectID   int64
	Name        string
	Slug        string
	Description string
	SecretRef   string
	SecretKey   string
	Sensitive   bool
	Enabled     bool
}

type UpsertAgentRoleRuntimeVariableInput struct {
	RoleID     int64
	VariableID int64
}

type UpsertAgentRoleInput struct {
	ProjectID         int64
	Name              string
	RoleType          string
	Description       string
	PromptTemplate    string
	PromptMode        string
	GitHubAccountName string
	OpenAIAccountName string
	KubernetesAccess  string
	SandboxMode       string
	ConfigOverlay     string
	AdvancedSettings  string
	Enabled           bool
	BotIdentity       string
}

type CreateChatInput struct {
	ProjectID           int64
	MattermostChannelID string
	Name                string
	Slug                string
	Description         string
	ChatType            string
	RootGitHubIssue     string
	WorkPolicy          string
	Settings            string
	SystemPurpose       string
	RoleIDs             []int64
	RepositoryIDs       []int64
}

type UpsertThreadContextInput struct {
	ProjectID                int64
	ChatID                   int64
	MattermostChannelID      string
	MattermostRootPostID     string
	RepositoryID             int64
	Status                   string
	PendingMattermostPostID  string
	PendingUserID            string
	PendingUserName          string
	PendingMessage           string
	PendingMattermostFileIDs []string
}

type UpsertMattermostBotIdentityInput struct {
	ProjectID        int64
	RoleID           int64
	Username         string
	DisplayName      string
	MattermostUserID string
	TokenSecretRef   string
	Status           string
	LastError        string
}

type UpsertAgentSessionInput struct {
	SessionKey           string
	ProjectID            int64
	ChatID               int64
	RoleID               int64
	SessionScope         string
	MattermostChannelID  string
	MattermostRootPostID string
	OpenAIAccountName    string
	TTLSeconds           int
	Capabilities         string
}

type UpdateAgentSessionRuntimeInput struct {
	SessionKey           string
	Status               string
	ActiveTurnID         int64
	ActiveRunID          string
	MattermostRootPostID string
	KubernetesNamespace  string
	PodName              string
	PVCName              string
	TokenSecretRef       string
	ExtendTTLSeconds     int
}

type UpdateAgentSessionSnapshotInput struct {
	SessionKey               string
	CodexSessionID           string
	SessionArchiveGzipBase64 string
	Status                   string
	ExtendTTLSeconds         int
}

type CreateAgentSessionTurnInput struct {
	SessionID            int64
	RunID                string
	MattermostChannelID  string
	MattermostRootPostID string
	MattermostPostID     string
	ParentTurnID         int64
	UserID               string
	UserName             string
	Message              string
}

type CompleteAgentSessionTurnInput struct {
	TurnID       int64
	Status       string
	FinalMessage string
	ErrorMessage string
	Artifacts    string
}

type CancelAgentSessionTurnInput struct {
	TurnID       int64
	ErrorMessage string
	Artifacts    string
}

type UpdateAgentSessionTurnStatusPostInput struct {
	TurnID       int64
	StatusPostID string
}

type UpdateAgentSessionTurnRunsPostInput struct {
	TurnID     int64
	RunsPostID string
}

type AddAgentSessionTurnOriginInput struct {
	TurnID            int64
	ParentTurnID      int64
	TriggerPostID     string
	InitiatorUserName string
}

type UpdateAgentSessionTurnMessageInput struct {
	TurnID  int64
	Message string
}

type CreateAgentDelegationInput struct {
	ProjectID       int64
	SourceSessionID int64
	SourceTurnID    int64
	TargetChatID    int64
	TargetRoleID    int64
	WorkItemKey     string
	Title           string
}

type CreateAgentDelegationCallbackDeliveryInput struct {
	DelegationID  int64
	CallbackRunID string
	Destination   string
	Publication   string
	ChannelID     string
	RootPostID    string
	Message       string
	PropsJSON     []byte
	PayloadSHA256 []byte
	ExternalID    string
}

type CreateAgentDelegationCallbackDeliveryManifestInput struct {
	DelegationID  int64
	CallbackRunID string
	ExpectedCount int
	ExpectedPlan  []byte
	PlanSHA256    []byte
}

type ClaimAgentDelegationCallbackDeliveryInput struct {
	DelegationID  int64
	CallbackRunID string
	Now           time.Time
	LeaseOwner    string
	LeaseUntil    time.Time
	ExcludedIDs   []int64
}

type ReleaseAgentDelegationCallbackDeliveryInput struct {
	ID            int64
	LeaseOwner    string
	Status        string
	LastErrorCode string
	Now           time.Time
}

type DeliverAgentDelegationCallbackDeliveryInput struct {
	ID               int64
	LeaseOwner       string
	MattermostPostID string
	Now              time.Time
}

type Repository interface {
	UpsertRepository(ctx context.Context, input UpsertRepositoryInput) (entity.Repository, bool, error)
	GetRepository(ctx context.Context, provider string, owner string, name string) (entity.Repository, error)
	ListRepositories(ctx context.Context, limit int) ([]entity.Repository, error)
	DeleteRepository(ctx context.Context, provider string, owner string, name string) (entity.Repository, error)
	UpsertProject(ctx context.Context, input UpsertProjectInput) (entity.Project, bool, error)
	UpdateProjectRunsChannel(ctx context.Context, projectID int64, channelID string) (entity.Project, error)
	GetProject(ctx context.Context, id int64) (entity.Project, error)
	GetProjectBySlug(ctx context.Context, slug string) (entity.Project, error)
	ListProjects(ctx context.Context, limit int) ([]entity.Project, error)
	UpsertProjectRepository(ctx context.Context, input UpsertProjectRepositoryInput) (entity.ProjectRepository, bool, error)
	ListProjectRepositories(ctx context.Context, projectID int64) ([]entity.ProjectRepository, error)
	UpsertProjectRuntimeVariable(ctx context.Context, input UpsertProjectRuntimeVariableInput) (entity.ProjectRuntimeVariable, bool, error)
	GetProjectRuntimeVariable(ctx context.Context, id int64) (entity.ProjectRuntimeVariable, error)
	ListProjectRuntimeVariables(ctx context.Context, projectID int64) ([]entity.ProjectRuntimeVariable, error)
	DeleteProjectRuntimeVariable(ctx context.Context, id int64) (entity.ProjectRuntimeVariable, error)
	UpsertAgentRoleRuntimeVariable(ctx context.Context, input UpsertAgentRoleRuntimeVariableInput) (entity.AgentRoleRuntimeVariableBinding, bool, error)
	DeleteAgentRoleRuntimeVariable(ctx context.Context, roleID int64, variableID int64) (entity.AgentRoleRuntimeVariableBinding, error)
	ListAgentRoleRuntimeVariables(ctx context.Context, roleID int64) ([]entity.AgentRoleRuntimeVariableBinding, error)
	UpsertAgentRole(ctx context.Context, input UpsertAgentRoleInput) (entity.AgentRole, bool, error)
	GetAgentRole(ctx context.Context, id int64) (entity.AgentRole, error)
	ListAgentRoles(ctx context.Context, projectID int64) ([]entity.AgentRole, error)
	CreateChat(ctx context.Context, input CreateChatInput) (entity.Chat, bool, error)
	GetChat(ctx context.Context, id int64) (entity.Chat, error)
	GetChatByMattermostChannelID(ctx context.Context, channelID string) (entity.Chat, error)
	ListChats(ctx context.Context, projectID int64) ([]entity.Chat, error)
	ListChatParticipants(ctx context.Context, chatID int64) ([]entity.ChatParticipant, error)
	ListChatRepositories(ctx context.Context, chatID int64) ([]entity.ChatRepositoryBinding, error)
	GetThreadContext(ctx context.Context, chatID int64, rootPostID string) (entity.ThreadContext, error)
	GetThreadContextByID(ctx context.Context, id int64) (entity.ThreadContext, error)
	UpsertThreadContext(ctx context.Context, input UpsertThreadContextInput) (entity.ThreadContext, bool, error)
	UpsertMattermostBotIdentity(ctx context.Context, input UpsertMattermostBotIdentityInput) (entity.MattermostBotIdentity, bool, error)
	GetMattermostBotIdentityByRoleID(ctx context.Context, roleID int64) (entity.MattermostBotIdentity, error)
	GetMattermostBotIdentityByUserID(ctx context.Context, mattermostUserID string) (entity.MattermostBotIdentity, error)
	ListMattermostBotIdentitiesByProject(ctx context.Context, projectID int64) ([]entity.MattermostBotIdentity, error)
	UpsertAgentSession(ctx context.Context, input UpsertAgentSessionInput) (entity.AgentSession, bool, error)
	GetAgentSession(ctx context.Context, sessionKey string) (entity.AgentSession, error)
	GetAgentSessionByID(ctx context.Context, id int64) (entity.AgentSession, error)
	ListAgentSessionsByThread(ctx context.Context, chatID int64, rootPostID string) ([]entity.AgentSession, error)
	ListAgentSessionsByChat(ctx context.Context, chatID int64) ([]entity.AgentSession, error)
	ListAgentSessionsByRole(ctx context.Context, roleID int64) ([]entity.AgentSession, error)
	AcquireAgentSessionCapacityLock(ctx context.Context) (func(), error)
	ListEvictableIdleAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error)
	ListQueuedIdleAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error)
	ListStaleActiveAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error)
	ListRunningActiveAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error)
	UpdateAgentSessionRuntime(ctx context.Context, input UpdateAgentSessionRuntimeInput) (entity.AgentSession, error)
	UpdateAgentSessionSnapshot(ctx context.Context, input UpdateAgentSessionSnapshotInput) (entity.AgentSession, error)
	ClearIdleAgentSessionPod(ctx context.Context, sessionKey string, podName string) (entity.AgentSession, error)
	ResetAgentSessionRuntime(ctx context.Context, sessionKey string, status string) (entity.AgentSession, error)
	CreateAgentSessionTurn(ctx context.Context, input CreateAgentSessionTurnInput) (entity.AgentSessionTurn, error)
	GetAgentSessionTurn(ctx context.Context, id int64) (entity.AgentSessionTurn, error)
	ClaimNextAgentSessionTurn(ctx context.Context, sessionKey string) (entity.AgentSessionTurn, error)
	CompleteAgentSessionTurn(ctx context.Context, input CompleteAgentSessionTurnInput) (entity.AgentSessionTurn, error)
	CancelAgentSessionTurn(ctx context.Context, input CancelAgentSessionTurnInput) (entity.AgentSessionTurn, error)
	UpdateAgentSessionTurnStatusPost(ctx context.Context, input UpdateAgentSessionTurnStatusPostInput) (entity.AgentSessionTurn, error)
	UpdateAgentSessionTurnRunsPost(ctx context.Context, input UpdateAgentSessionTurnRunsPostInput) (entity.AgentSessionTurn, error)
	AddAgentSessionTurnOrigin(ctx context.Context, input AddAgentSessionTurnOriginInput) (entity.AgentSessionTurn, error)
	UpdateAgentSessionTurnMessage(ctx context.Context, input UpdateAgentSessionTurnMessageInput) (entity.AgentSessionTurn, error)
	ListQueuedAgentSessionTurns(ctx context.Context, sessionID int64) ([]entity.AgentSessionTurn, error)
	CreateAgentDelegation(ctx context.Context, input CreateAgentDelegationInput) (entity.AgentDelegation, bool, error)
	GetAgentDelegationBySourceTurnKey(ctx context.Context, sourceTurnID int64, workItemKey string) (entity.AgentDelegation, error)
	GetAgentDelegationForCallback(ctx context.Context, targetSessionID int64) (entity.AgentDelegation, error)
	ListAgentDelegationsBySource(ctx context.Context, sourceSessionID int64, limit int) ([]entity.AgentDelegation, error)
	SetAgentDelegationRoot(ctx context.Context, id int64, rootPostID string) (entity.AgentDelegation, error)
	SetAgentDelegationTarget(ctx context.Context, id int64, targetSessionID int64, targetTurnID int64, targetRunID string) (entity.AgentDelegation, error)
	SetAgentDelegationFailed(ctx context.Context, id int64) (entity.AgentDelegation, error)
	SetAgentDelegationCallback(ctx context.Context, id int64, callbackTurnID int64, callbackRunID string) (entity.AgentDelegation, error)
	UpsertAgentProfile(ctx context.Context, input UpsertAgentProfileInput) (entity.AgentProfile, bool, error)
	GetAgentProfile(ctx context.Context, name string) (entity.AgentProfile, error)
	ListAgentProfiles(ctx context.Context) ([]entity.AgentProfile, error)
	ListAgentPromptTemplates(ctx context.Context, profileName string) ([]entity.AgentPromptTemplate, error)
	GetAgentPromptTemplate(ctx context.Context, profileName string, templateKey string) (entity.AgentPromptTemplate, error)
	UpsertAgentPromptTemplate(ctx context.Context, input UpsertAgentPromptTemplateInput) (entity.AgentPromptTemplate, bool, error)
	UpsertOpenAIAccount(ctx context.Context, input UpsertOpenAIAccountInput) (entity.OpenAIAccount, bool, error)
	ListOpenAIAccounts(ctx context.Context, limit int) ([]entity.OpenAIAccount, error)
	GetOpenAIAccount(ctx context.Context, name string) (entity.OpenAIAccount, error)
	UpdateOpenAIAccountStatus(ctx context.Context, input UpdateOpenAIAccountStatusInput) (entity.OpenAIAccount, error)
	DeleteOpenAIAccount(ctx context.Context, name string) (entity.OpenAIAccount, error)
	ListGitHubAccounts(ctx context.Context, limit int) ([]entity.GitHubAccount, error)
	GetGitHubAccount(ctx context.Context, name string) (entity.GitHubAccount, error)
	UpsertGitHubAccount(ctx context.Context, input UpsertGitHubAccountInput) (entity.GitHubAccount, bool, error)
	DeleteGitHubAccount(ctx context.Context, name string) (entity.GitHubAccount, error)
	CreateAgentFlow(ctx context.Context, input CreateAgentFlowInput) (entity.AgentFlow, bool, error)
	GetAgentFlow(ctx context.Context, flowID string) (entity.AgentFlow, error)
	ListAgentFlows(ctx context.Context, status string, limit int) ([]entity.AgentFlow, error)
	UpdateAgentFlow(ctx context.Context, input UpdateAgentFlowInput) (entity.AgentFlow, error)
	CreateAgentRun(ctx context.Context, input CreateAgentRunInput) (entity.AgentRun, error)
	GetAgentRun(ctx context.Context, runID string) (entity.AgentRun, error)
	ListAgentRuns(ctx context.Context, limit int) ([]entity.AgentRun, error)
	ListAgentRunsByFlowID(ctx context.Context, flowID string) ([]entity.AgentRun, error)
	UpdateAgentRunArtifacts(ctx context.Context, input UpdateAgentRunArtifactsInput) (entity.AgentRun, error)
	RecordAuditEvent(ctx context.Context, input AuditEventInput) error
}

type ExactAgentSessionsRuntimeGuardRepository interface {
	WithExactAgentSessionsRuntimeGuard(ctx context.Context, expected []entity.AgentSession, sideEffect func(Repository) error) error
}

type ExactAgentSessionsPublishFenceRepository interface {
	LockExactAgentSessionsPublishFence(ctx context.Context, expected []entity.AgentSession) error
}

type AgentDelegationCallbackDeliveryRepository interface {
	CreateAgentDelegationCallbackDeliveries(ctx context.Context, inputs []CreateAgentDelegationCallbackDeliveryInput) ([]entity.AgentDelegationCallbackDelivery, error)
	CreateAgentDelegationCallbackDeliveryManifest(ctx context.Context, input CreateAgentDelegationCallbackDeliveryManifestInput) error
	ValidateAgentDelegationCallbackDeliveryPlan(ctx context.Context, delegationID int64, callbackRunID string) error
	ListAgentDelegationCallbackDeliveries(ctx context.Context, delegationID int64, callbackRunID string) ([]entity.AgentDelegationCallbackDelivery, error)
	ClaimAgentDelegationCallbackDelivery(ctx context.Context, input ClaimAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error)
	ReleaseAgentDelegationCallbackDelivery(ctx context.Context, input ReleaseAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error)
	DeliverAgentDelegationCallbackDelivery(ctx context.Context, input DeliverAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error)
}
