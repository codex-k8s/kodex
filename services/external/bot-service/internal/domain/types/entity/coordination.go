package entity

import "time"

const (
	CoordinationCapabilityStartAgents        = "agents.start"
	CoordinationCapabilityReturnCallback     = "callbacks.return"
	CoordinationCapabilityReceiveCallback    = "callbacks.receive"
	CoordinationCapabilityRequestAttention   = "owner_attention.request"
	CoordinationCapabilityReadProjectMemory  = "memory.project.read"
	CoordinationCapabilityWriteProjectMemory = "memory.project.write"
	CoordinationCapabilityReadRoleMemory     = "memory.role.read"
	CoordinationCapabilityWriteRoleMemory    = "memory.role.write"
	CoordinationCapabilityReadProjectWork    = "work.project.read"
	CoordinationCapabilityUpdateOwnWork      = "work.own.update"
	CoordinationCapabilityManageProjectWork  = "work.project.manage"
	CoordinationCapabilityRequestSync        = "sync.request"
	CoordinationCapabilityReceiveSync        = "sync.receive"

	CoordinationActionStart       = "start"
	CoordinationActionCallback    = "callback"
	CoordinationActionRequestSync = "request_sync"
)

type ProcessContext struct {
	ProcessRunID          int64
	ProcessPublicID       string
	ProjectID             int64
	PolicyRevisionID      int64
	RootRoleID            int64
	RootInitiatorUserID   string
	RootInitiatorUserName string
	RootTriggerPostID     string
	RootChannelID         string
	RootThreadPostID      string
	Status                string
}

type ProcessLineageStep struct {
	TurnID       int64
	ParentTurnID int64
	RoleID       int64
	RoleName     string
	BotIdentity  string
	RunID        string
	LaunchPostID string
}

type WorkClaim struct {
	ID           int64     `json:"id"`
	ProcessRunID int64     `json:"process_run_id"`
	TurnID       int64     `json:"turn_id"`
	RoleID       int64     `json:"role_id"`
	RoleName     string    `json:"role_name"`
	Summary      string    `json:"summary"`
	Domains      []string  `json:"domains"`
	ResourceKeys []string  `json:"resource_keys"`
	Links        []string  `json:"links"`
	Status       string    `json:"status"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type MemoryRecord struct {
	ID              int64     `json:"id"`
	ProjectID       int64     `json:"project_id"`
	Scope           string    `json:"scope"`
	RoleID          int64     `json:"role_id,omitempty"`
	Status          string    `json:"status"`
	Importance      string    `json:"importance"`
	Title           string    `json:"title"`
	Content         string    `json:"content"`
	Version         int       `json:"version"`
	SourcePostID    string    `json:"source_post_id,omitempty"`
	CreatedByRoleID int64     `json:"created_by_role_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type OwnerAttentionRequest struct {
	ID               int64    `json:"id"`
	ProcessRunID     int64    `json:"process_run_id"`
	TurnID           int64    `json:"turn_id"`
	Severity         string   `json:"severity"`
	Summary          string   `json:"summary"`
	Options          []string `json:"options"`
	Recommendation   string   `json:"recommendation,omitempty"`
	EvidenceLinks    []string `json:"evidence_links,omitempty"`
	PauseScope       string   `json:"pause_scope"`
	IdempotencyKey   string   `json:"idempotency_key"`
	MattermostPostID string   `json:"mattermost_post_id,omitempty"`
	Status           string   `json:"status"`
}
