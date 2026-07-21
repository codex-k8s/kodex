package automations

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

var (
	ErrNotFound         = errors.New("automation item not found")
	ErrForbidden        = errors.New("automation scope is forbidden")
	ErrConflict         = errors.New("automation state conflicts with the request")
	ErrCallbackMismatch = errors.New("automation callback does not match the accepted payload")
	ErrCallbackRevoked  = errors.New("automation callback is revoked")
)

type CreateScheduleInput struct {
	PublicID                string
	ProjectID               int64
	TargetAgentRoleID       int64
	TargetChatID            int64
	Name                    string
	OwnerMattermostUserID   string
	OwnerMattermostUserName string
	Preset                  string
	LocalTime               string
	TimeZone                string
	NextRunAt               time.Time
	PlaybookKey             string
	PromptVersion           string
	PromptSnapshot          string
	PromptSHA256            []byte
	CallbackContractVersion string
	IdempotencyKey          string
	CommandHash             []byte
	Now                     time.Time
}

type CreateManualRunInput struct {
	SchedulePublicID      string
	ProjectID             int64
	OwnerMattermostUserID string
	IdempotencyKey        string
	OccurrencePublicID    string
	RunPublicID           string
	ScheduledFor          time.Time
	CallbackExpiresAt     time.Time
}

type BindRunInput struct {
	RunPublicID           string
	ProjectID             int64
	OwnerMattermostUserID string
	RuntimeSessionID      int64
	RuntimeSessionKey     string
	RuntimeTurnID         int64
	RuntimeRunID          string
	MattermostChannelID   string
	MattermostRootPostID  string
	Now                   time.Time
}

type FailRunInput struct {
	RunPublicID           string
	ProjectID             int64
	OwnerMattermostUserID string
	SafeSummary           string
	Now                   time.Time
}

type CompleteCallbackInput struct {
	RunPublicID             string
	ProjectID               int64
	RuntimeSessionID        int64
	RuntimeTurnID           int64
	RuntimeRunID            string
	CallbackContractVersion string
	Status                  string
	Outcome                 string
	SafeSummary             string
	PayloadSHA256           []byte
	Now                     time.Time
}

type Repository interface {
	CreateSchedule(ctx context.Context, input CreateScheduleInput) (entity.AutomationSchedule, bool, error)
	GetSchedule(ctx context.Context, publicID string, projectID int64, ownerMattermostUserID string) (entity.AutomationSchedule, error)
	ListSchedules(ctx context.Context, projectID int64, ownerMattermostUserID string, limit int) ([]entity.AutomationSchedule, error)
	CreateManualRun(ctx context.Context, input CreateManualRunInput) (entity.ScheduledRun, bool, error)
	BindRun(ctx context.Context, input BindRunInput) (entity.ScheduledRun, error)
	FailRun(ctx context.Context, input FailRunInput) (entity.ScheduledRun, error)
	GetRun(ctx context.Context, publicID string, projectID int64, ownerMattermostUserID string) (entity.ScheduledRun, error)
	ListRuns(ctx context.Context, schedulePublicID string, projectID int64, ownerMattermostUserID string, limit int) ([]entity.ScheduledRun, error)
	CompleteCallback(ctx context.Context, input CompleteCallbackInput) (entity.ScheduledRun, bool, error)
	RevokeCallback(ctx context.Context, runPublicID string, projectID int64, now time.Time) error
}
