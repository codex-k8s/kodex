package entity

import "time"

type Project struct {
	ID               int64
	Name             string
	Slug             string
	MattermostTeamID string
	Description      string
	AdvancedSettings string
	CreatedAt        time.Time
	UpdatedAt        time.Time
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
