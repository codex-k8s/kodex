package entity

import "time"

type AutomationSchedule struct {
	ID                      int64
	PublicID                string
	ProjectID               int64
	ProjectName             string
	TargetAgentRoleID       int64
	TargetAgentRoleName     string
	TargetChatID            int64
	TargetChatName          string
	Name                    string
	OwnerMattermostUserID   string
	OwnerMattermostUserName string
	Preset                  string
	LocalTime               string
	TimeZone                string
	Enabled                 bool
	NextRunAt               time.Time
	PlaybookKey             string
	PromptVersion           string
	PromptSnapshot          string
	PromptSHA256            []byte
	CallbackContractVersion string
	CommandHash             []byte
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type ScheduleOccurrence struct {
	ID             int64
	PublicID       string
	ScheduleID     int64
	ProjectID      int64
	Source         string
	IdempotencyKey string
	ScheduledFor   time.Time
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ScheduledRun struct {
	ID                      int64
	PublicID                string
	OccurrenceID            int64
	ScheduleID              int64
	SchedulePublicID        string
	ScheduleName            string
	ProjectID               int64
	ProjectName             string
	TargetAgentRoleID       int64
	TargetAgentRoleName     string
	TargetChatID            int64
	TargetChatName          string
	OwnerMattermostUserID   string
	OwnerMattermostUserName string
	Source                  string
	Status                  string
	Outcome                 string
	SafeSummary             string
	CorrelationID           string
	PromptVersion           string
	CallbackContractVersion string
	CallbackPayloadSHA256   []byte
	CallbackRevokedAt       time.Time
	CallbackExpiresAt       time.Time
	RuntimeSessionID        int64
	RuntimeSessionKey       string
	RuntimeTurnID           int64
	RuntimeRunID            string
	MattermostChannelID     string
	MattermostRootPostID    string
	StartedAt               time.Time
	FinishedAt              time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type AutomationOwnerGateContext struct {
	ScheduledRunID       int64
	ScheduledRunPublicID string
	ProjectID            int64
	RuntimeTurnID        int64
	ProcessRunID         int64
	ProcessPublicID      string
	PolicyRevisionID     int64
	RootInitiatorUserID  string
	RootInitiatorName    string
	MattermostChannelID  string
	MattermostRootPostID string
}

type AutomationOwnerAttentionDelivery struct {
	AttentionID           int64
	ScheduledRunID        int64
	ScheduledRunPublicID  string
	ProcessRunID          int64
	PolicyRevisionID      int64
	RootInitiatorUserID   string
	MattermostChannelID   string
	MattermostRootPostID  string
	MattermostPostID      string
	Status                string
	DeliveryID            string
	DeliveryMessage       string
	DeliveryPropsJSON     []byte
	DeliveryPayloadSHA256 []byte
}

type AutomationAuditEvent struct {
	ID             int64
	ProjectID      int64
	ScheduleID     int64
	ScheduledRunID int64
	EventType      string
	ActorUserID    string
	ActorUserName  string
	CorrelationID  string
	SafeSummary    string
	CreatedAt      time.Time
}
