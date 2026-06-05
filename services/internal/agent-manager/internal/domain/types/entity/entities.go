// Package entity contains agent-manager domain entities.
package entity

import (
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type VersionedBase struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Flow struct {
	VersionedBase
	Scope           value.ScopeRef
	Slug            string
	DisplayName     []value.LocalizedText
	Description     []value.LocalizedText
	IconObjectURI   string
	Status          enum.FlowStatus
	ActiveVersionID *uuid.UUID
}

type FlowVersion struct {
	ID               uuid.UUID
	FlowID           uuid.UUID
	Version          int64
	SourceRef        string
	DefinitionDigest string
	Status           enum.FlowVersionStatus
	Stages           []Stage
	Transitions      []StageTransition
	RoleBindings     []StageRoleBinding
	ActivatedAt      *time.Time
	CreatedAt        time.Time
}

type Stage struct {
	ID                    uuid.UUID
	FlowVersionID         uuid.UUID
	Slug                  string
	StageType             enum.StageType
	DisplayName           []value.LocalizedText
	IconObjectURI         string
	RequiredArtifactsJSON []byte
	AcceptancePolicyJSON  []byte
	Position              int32
}

type StageTransition struct {
	ID            uuid.UUID
	FlowVersionID uuid.UUID
	FromStageID   *uuid.UUID
	ToStageID     uuid.UUID
	ConditionJSON []byte
	FollowUpType  string
	Position      int32
}

type StageRoleBinding struct {
	ID                    uuid.UUID
	StageID               uuid.UUID
	RoleProfileID         uuid.UUID
	BindingKind           enum.StageRoleBindingKind
	LaunchPolicyJSON      []byte
	RequiredForAcceptance bool
}

type RoleProfile struct {
	VersionedBase
	Scope                    value.ScopeRef
	Slug                     string
	DisplayName              []value.LocalizedText
	IconObjectURI            string
	RoleKind                 enum.RoleKind
	RuntimeProfile           string
	AllowedMCPTools          []string
	ProviderAccountPolicyRef string
	Status                   enum.RoleStatus
}

type PromptTemplate struct {
	VersionedBase
	RoleProfileID   uuid.UUID
	PromptKind      enum.PromptKind
	ActiveVersionID *uuid.UUID
}

type PromptTemplateVersion struct {
	ID               uuid.UUID
	PromptTemplateID uuid.UUID
	RoleProfileID    uuid.UUID
	PromptKind       enum.PromptKind
	Version          int64
	SourceRef        string
	TemplateObject   value.ObjectRef
	TemplateDigest   string
	Status           enum.PromptVersionStatus
	ActivatedAt      *time.Time
	CreatedAt        time.Time
}

type AgentSession struct {
	VersionedBase
	Scope                 value.ScopeRef
	ProviderWorkItemRef   string
	FlowVersionID         *uuid.UUID
	CurrentStageID        *uuid.UUID
	LatestStateSnapshotID *uuid.UUID
	Status                enum.AgentSessionStatus
	CreatedByActorRef     string
}

type AgentRun struct {
	VersionedBase
	SessionID               uuid.UUID
	FlowVersionID           *uuid.UUID
	StageID                 *uuid.UUID
	RoleProfileID           uuid.UUID
	RoleProfileVersion      int64
	RoleProfileDigest       string
	PromptTemplateVersionID uuid.UUID
	PromptTemplateDigest    string
	RuntimeContext          value.RuntimeContextRef
	ProviderTarget          value.ProviderTargetRef
	GuidanceRefs            []value.GuidanceRef
	Status                  enum.AgentRunStatus
	ResultSummary           string
	FailureCode             string
	StartedAt               *time.Time
	FinishedAt              *time.Time
}

type AgentSessionStateSnapshot struct {
	ID           uuid.UUID
	SessionID    uuid.UUID
	RunID        *uuid.UUID
	SnapshotKind enum.AgentSessionSnapshotKind
	TurnIndex    *int64
	Object       value.ObjectRef
	CapturedAt   time.Time
	CreatedAt    time.Time
}

type AcceptanceResult struct {
	VersionedBase
	SessionID         uuid.UUID
	RunID             *uuid.UUID
	StageID           *uuid.UUID
	CheckKind         enum.AcceptanceCheckKind
	Status            enum.AcceptanceStatus
	TargetRef         string
	DetailsJSON       []byte
	GovernanceContext value.GovernanceContextRef
}

type FollowUpIntent struct {
	VersionedBase
	SessionID             uuid.UUID
	RunID                 *uuid.UUID
	FromStageID           *uuid.UUID
	ToStageID             *uuid.UUID
	AcceptanceResultID    *uuid.UUID
	ProviderTarget        value.ProviderTargetRef
	ProviderWorkItemType  string
	ProviderOperationRef  string
	InstructionBodyDigest string
	SafeTitle             string
	SafeSummary           string
	RoleHint              string
	StageHint             string
	IdempotencyKey        string
	GovernanceContext     value.GovernanceContextRef
	Status                enum.FollowUpIntentStatus
}

type AgentActivity struct {
	VersionedBase
	SessionID       uuid.UUID
	RunID           *uuid.UUID
	TurnID          string
	ToolUseID       string
	ActivityKind    enum.AgentActivityKind
	ToolName        string
	ToolCategory    string
	Status          enum.AgentActivityStatus
	StartedAt       time.Time
	FinishedAt      *time.Time
	DurationMs      *int64
	SafeSummary     string
	PayloadDigest   string
	BoundedError    string
	SafeRefsJSON    []byte
	SafeDetailsJSON []byte
	CorrelationID   string
	IdempotencyKey  string
}

type AgentActivitySummary struct {
	ID            uuid.UUID
	ActivityKind  enum.AgentActivityKind
	Status        enum.AgentActivityStatus
	ToolName      string
	ToolCategory  string
	SafeSummary   string
	PayloadDigest string
	BoundedError  string
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Version       int64
	UpdatedAt     time.Time
}

type AgentRunListItem struct {
	Run                 AgentRun
	HumanGateWaiting    bool
	HumanGateRequestRef string
	HumanGateReasonCode string
	FollowUpWaiting     bool
	LatestActivity      *AgentActivitySummary
	SortBucket          int32
	SortTime            time.Time
}

type AgentSessionListItem struct {
	Session              AgentSession
	LatestRunID          *uuid.UUID
	LatestRunStatus      enum.AgentRunStatus
	LatestRuntimeJobRef  string
	LatestRunSafeSummary string
	ActiveRunCount       int32
	HumanGateWaiting     bool
	HumanGateRequestRef  string
	HumanGateReasonCode  string
	LatestActivity       *AgentActivitySummary
	FollowUpWaiting      bool
	FollowUpRef          string
	SortBucket           int32
	SortTime             time.Time
}

type HumanGateRequest struct {
	VersionedBase
	SessionID                uuid.UUID
	RunID                    *uuid.UUID
	StageID                  *uuid.UUID
	AcceptanceResultID       *uuid.UUID
	ProviderTarget           value.ProviderTargetRef
	TargetRef                string
	RequestKind              string
	ReasonCode               string
	SafeSummary              string
	InteractionRequestRef    string
	InteractionResponseRef   string
	GovernanceGateRequestRef string
	GovernanceDecisionRef    string
	IdempotencyKey           string
	GovernanceContext        value.GovernanceContextRef
	Status                   enum.HumanGateStatus
	Outcome                  enum.HumanGateOutcome
	ResolvedAt               *time.Time
}

type SelfDeployPlan struct {
	VersionedBase
	Scope                   value.ScopeRef
	ProjectRef              string
	RepositoryRef           string
	ProviderSignalRef       string
	SourceRef               string
	MergeCommitSHA          string
	ServicesYAMLRef         string
	ServicesYAMLDigest      string
	AffectedServiceKeys     []string
	PathCategories          []enum.SelfDeployPathCategory
	ExpectedRuntimeJobTypes []enum.SelfDeployRuntimeJobType
	GovernanceContext       value.GovernanceContextRef
	SafeSummary             string
	PlanFingerprint         string
	IdempotencyKey          string
	Status                  enum.SelfDeployPlanStatus
	RuntimeBuildJobs        []SelfDeployRuntimeBuildJob
	RuntimeBuildStatus      enum.SelfDeployRuntimeBuildStatus
	RuntimeBuildFingerprint string
	RuntimeBuildErrorCode   string
	RuntimeBuildSummary     string
}

type SelfDeployRuntimeBuildJob struct {
	ServiceKey               string `json:"service_key"`
	ServiceRef               string `json:"service_ref"`
	RuntimeJobRef            string `json:"runtime_job_ref"`
	RuntimeJobStatus         string `json:"runtime_job_status"`
	BuildPlanItemFingerprint string `json:"build_plan_item_fingerprint"`
}

type CommandResult struct {
	Key            string
	CommandID      *uuid.UUID
	IdempotencyKey string
	Actor          value.Actor
	Operation      string
	AggregateType  enum.CommandAggregateType
	AggregateID    uuid.UUID
	ResultPayload  []byte
	CreatedAt      time.Time
}

type OutboxEvent struct {
	outboxlib.Event
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailureKind         string
	FailedPermanentlyAt *time.Time
	PublishedAt         *time.Time
	LastError           string
}
