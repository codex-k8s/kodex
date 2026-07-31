package resource

import (
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type CreateInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Kind           enum.Kind
	Name           string
	ParentID       string
	Spec           entity.Spec
	TenantProject  bool
}

type UpdateInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
	Name            string
	Spec            entity.Spec
}

type TransitionInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
	Target          enum.State
	ReasonCode      string
}

type DeleteInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
}

type GetInput struct {
	Principal  value.Principal
	ResourceID string
	Kind       enum.Kind
}

type ListInput struct {
	Principal      value.Principal
	Filter         query.ResourceFilter
	TenantProjects bool
}

type EnqueueTurnInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	SessionID        string
	SourceRef        string
	PromptArtifactID string
	ProcessRunID     string
}

type ClaimTurnInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ClaimTurnResult struct {
	Turn           entity.Resource
	LeaseToken     string
	LeaseExpiresAt time.Time
}

type CompleteTurnInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	TurnID           string
	LeaseToken       string
	ExpectedVersion  uint64
	TerminalState    enum.State
	Outcome          string
	ResultArtifactID string
}

type ClaimDueSchedulesInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Limit          int
}

type ClaimDueSchedulesResult struct {
	Occurrences []ScheduleOccurrence
}

type ScheduleOccurrence struct {
	ScheduleID       string    `json:"scheduleId"`
	ScheduledFor     time.Time `json:"scheduledFor"`
	OccurrenceID     string    `json:"occurrenceId"`
	TargetResourceID string    `json:"targetResourceId"`
}

type ResolveOwnerGateInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	OwnerGateID     string
	ExpectedVersion uint64
	Decision        string
	Reason          string
}
