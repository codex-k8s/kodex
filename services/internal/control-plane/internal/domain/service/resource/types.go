package resource

import (
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
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
	Administrative bool
}

type UpdateInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
	Name            string
	Spec            entity.Spec
	Administrative  bool
}

type TransitionInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
	Target          enum.State
	ReasonCode      string
	Administrative  bool
}

type DeleteInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
	Administrative  bool
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

type SearchInput struct {
	Principal value.Principal
	Filter    query.ResourceSearch
}

type ListAuditInput struct {
	Principal value.Principal
	Filter    query.AuditFilter
}

type ListTombstonesInput struct {
	Principal value.Principal
	Filter    query.TombstoneFilter
}

type DiagnosticsInput struct {
	Principal value.Principal
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
	Turn                entity.Resource
	LeaseToken          string
	LeaseExpiresAt      time.Time
	Attempt             uint32
	AuthorityGeneration uint64
}

type RenewTurnInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	TurnID          string
	LeaseToken      string
	ExpectedVersion uint64
	Attempt         uint32
}

type RenewTurnResult = ClaimTurnResult

type CompleteTurnInput struct {
	Principal           value.Principal
	IdempotencyKey      string
	TurnID              string
	LeaseToken          string
	ExpectedVersion     uint64
	TerminalState       enum.State
	Outcome             string
	ResultArtifactID    string
	Attempt             uint32
	AuthorityGeneration uint64
}

type RetryTurnInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	TurnID          string
	ExpectedVersion uint64
	ReasonCode      string
}

type CancelTurnInput = RetryTurnInput

type ClaimDueSchedulesInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Limit          int
}

type ClaimDueSchedulesResult struct {
	Occurrences []ScheduleOccurrence
}

type ScheduleOccurrence struct {
	ScheduleID           string    `json:"scheduleId"`
	ScheduledFor         time.Time `json:"scheduledFor"`
	OccurrenceID         string    `json:"occurrenceId"`
	TargetResourceID     string    `json:"targetResourceId"`
	TargetKind           enum.Kind `json:"targetKind"`
	TargetVersion        uint64    `json:"targetVersion"`
	EffectiveInputSHA256 string    `json:"effectiveInputSha256"`
	State                string    `json:"state"`
	Attempt              uint32    `json:"attempt"`
	AvailableAt          time.Time `json:"availableAt"`
}

type ManageScheduleInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	Action          string
	ScheduleID      string
	ExpectedVersion uint64
	Name            string
	Spec            entity.ScheduleSpec
}

type ClaimScheduleOccurrenceInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ScheduleOccurrenceResult struct {
	Occurrence domainrepo.ScheduleOccurrence
	LeaseToken string
}

type CompleteScheduleOccurrenceInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	OccurrenceID     string
	LeaseToken       string
	ExpectedAttempt  uint32
	TerminalState    string
	Outcome          string
	ResultArtifactID string
}

type CancelScheduleOccurrenceInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	OccurrenceID    string
	ExpectedAttempt uint32
	ReasonCode      string
}

type ListScheduleOccurrencesInput struct {
	Principal value.Principal
	Filter    query.ScheduleOccurrenceFilter
}

type StartProcessInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	Name            string
	ParentProcessID string
	PlaybookRef     string
	PolicyRevision  uint64
	RootTriggerRef  string
	RootSessionID   string
	RootTurnID      string
	RootAttempt     uint32
	InputArtifactID string
}

type CancelProcessInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ProcessRunID    string
	ExpectedVersion uint64
	ReasonCode      string
}

type RequestOwnerGateInput struct {
	Principal              value.Principal
	IdempotencyKey         string
	ProcessRunID           string
	ProcessExpectedVersion uint64
	SessionID              string
	TurnID                 string
	Attempt                uint32
	ResultArtifactID       string
	ExpiresAt              time.Time
}

type OwnerGateResult struct {
	OwnerGate entity.Resource
	Process   entity.Resource
}

type ResolveOwnerGateInput struct {
	Principal              value.Principal
	IdempotencyKey         string
	OwnerGateID            string
	ExpectedVersion        uint64
	Decision               string
	Reason                 string
	ProcessRunID           string
	ProcessExpectedVersion uint64
	SessionID              string
	TurnID                 string
	Attempt                uint32
	ImmutableInputSHA256   string
}

type RegisterArtifactInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Name           string
	ParentID       string
	Spec           entity.ArtifactSpec
}

type RecordArtifactScanInput struct {
	Principal          value.Principal
	IdempotencyKey     string
	ArtifactID         string
	ExpectedVersion    uint64
	TargetState        string
	ScanPolicyRevision uint64
	EvidenceSHA256     string
}

// Observer получает только закрытые kind/action после durable commit.
type Observer interface {
	ObserveMutation(kind enum.Kind, action string)
}
