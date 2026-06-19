package entity

import "time"

type Project struct {
	ID                int64
	Name              string
	Slug              string
	MattermostTeamID  string
	GitHubAccountName string
	GitHubOwner       string
	GitHubOwnerType   string
	Description       string
	AdvancedSettings  string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ProjectRepository struct {
	ID            int64
	ProjectID     int64
	RepositoryID  int64
	Provider      string
	Owner         string
	Name          string
	DefaultBranch string
	IsDefault     bool
	Metadata      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (repo ProjectRepository) FullName() string {
	return repo.Owner + "/" + repo.Name
}

type ProjectRuntimeVariable struct {
	ID          int64
	ProjectID   int64
	Name        string
	Slug        string
	Description string
	SecretRef   string
	SecretKey   string
	Sensitive   bool
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AgentRoleRuntimeVariableBinding struct {
	ID          int64
	RoleID      int64
	RoleName    string
	VariableID  int64
	ProjectID   int64
	Name        string
	Slug        string
	Description string
	SecretRef   string
	SecretKey   string
	Sensitive   bool
	Enabled     bool
	CreatedAt   time.Time
}

type AgentRole struct {
	ID                int64
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
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Chat struct {
	ID                  int64
	ProjectID           int64
	MattermostChannelID string
	Name                string
	Slug                string
	Description         string
	ChatType            string
	RootGitHubIssue     string
	WorkPolicy          string
	Settings            string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ChatParticipant struct {
	ID        int64
	ChatID    int64
	RoleID    int64
	RoleName  string
	Enabled   bool
	CreatedAt time.Time
}

type ChatRepositoryBinding struct {
	ID           int64
	ChatID       int64
	RepositoryID int64
	Provider     string
	Owner        string
	Name         string
	CreatedAt    time.Time
}

func (binding ChatRepositoryBinding) FullName() string {
	return binding.Owner + "/" + binding.Name
}

type ThreadContext struct {
	ID                      int64
	ProjectID               int64
	ChatID                  int64
	MattermostChannelID     string
	MattermostRootPostID    string
	RepositoryID            int64
	RepositoryProvider      string
	RepositoryOwner         string
	RepositoryName          string
	RepositoryDefaultBranch string
	Status                  string
	PendingMattermostPostID string
	PendingUserID           string
	PendingUserName         string
	PendingMessage          string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (context ThreadContext) RepositoryFullName() string {
	if context.RepositoryOwner == "" || context.RepositoryName == "" {
		return ""
	}
	return context.RepositoryOwner + "/" + context.RepositoryName
}

type MattermostBotIdentity struct {
	ID               int64
	ProjectID        int64
	RoleID           int64
	Username         string
	DisplayName      string
	MattermostUserID string
	TokenSecretRef   string
	Status           string
	LastError        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AgentSession struct {
	ID                       int64
	SessionKey               string
	ProjectID                int64
	ChatID                   int64
	RoleID                   int64
	SessionScope             string
	MattermostChannelID      string
	MattermostRootPostID     string
	CodexSessionID           string
	Status                   string
	ActiveTurnID             int64
	ActiveRunID              string
	KubernetesNamespace      string
	PodName                  string
	PVCName                  string
	TokenSecretRef           string
	Capabilities             string
	SessionArchiveGzipBase64 string
	TTLSeconds               int
	LastActivityAt           time.Time
	ExpiresAt                time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type AgentSessionTurn struct {
	ID                   int64
	SessionID            int64
	RunID                string
	MattermostChannelID  string
	MattermostRootPostID string
	MattermostPostID     string
	UserID               string
	UserName             string
	Message              string
	Status               string
	FinalMessage         string
	ErrorMessage         string
	Artifacts            string
	CreatedAt            time.Time
	StartedAt            time.Time
	FinishedAt           time.Time
	UpdatedAt            time.Time
}
