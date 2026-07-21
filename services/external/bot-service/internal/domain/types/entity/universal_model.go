package entity

import "time"

type ConfigurationOwner string

const (
	ConfigurationOwnerUI  ConfigurationOwner = "ui"
	ConfigurationOwnerGit ConfigurationOwner = "git"
)

type Workspace struct {
	ID                int64
	OrganizationScope string
	LegacyProjectID   int64
	Name              string
	Slug              string
	Description       string
	MattermostTeamID  string
	Status            string
	ManagedBy         ConfigurationOwner
	SourceRevision    string
	Provenance        string
	RecordVersion     int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Room struct {
	ID                  int64
	OrganizationScope   string
	WorkspaceID         int64
	LegacyChatID        int64
	Name                string
	Slug                string
	Description         string
	RoomType            string
	Purpose             string
	WorkPolicy          string
	MattermostChannelID string
	Status              string
	ManagedBy           ConfigurationOwner
	SourceRevision      string
	Provenance          string
	RecordVersion       int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type RoleDefinition struct {
	ID                int64
	OrganizationScope string
	LegacyAgentRoleID int64
	Name              string
	Slug              string
	RoleType          string
	Description       string
	DefaultPolicy     string
	Status            string
	ManagedBy         ConfigurationOwner
	SourceRevision    string
	Provenance        string
	RecordVersion     int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Agent struct {
	ID                int64
	OrganizationScope string
	LegacyAgentRoleID int64
	RoleDefinitionID  int64
	InstructionSetID  int64
	BotIdentityID     int64
	Name              string
	Slug              string
	Status            string
	ManagedBy         ConfigurationOwner
	SourceRevision    string
	Provenance        string
	RecordVersion     int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type AgentAssignment struct {
	ID                int64
	OrganizationScope string
	AgentID           int64
	WorkspaceID       int64
	RoomID            int64
	Enabled           bool
	Default           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type InstructionSet struct {
	ID                int64
	OrganizationScope string
	Name              string
	Slug              string
	SourceType        string
	ManagedBy         ConfigurationOwner
	SourceRevision    string
	Provenance        string
	CurrentVersionID  int64
	Status            string
	RecordVersion     int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type InstructionVersion struct {
	ID                int64
	OrganizationScope string
	InstructionSetID  int64
	Version           int64
	Markdown          string
	ContentSHA256     []byte
	ActorRef          string
	CreatedAt         time.Time
}

type AgentInstructionSnapshot struct {
	RoleDefinition     RoleDefinition
	Agent              Agent
	InstructionSet     InstructionSet
	InstructionVersion InstructionVersion
}
