// Package entity contains governance-manager domain entities.
package entity

import (
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

// VersionedBase stores common aggregate metadata.
type VersionedBase struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RiskProfile describes risk and gate policy for an external scope.
type RiskProfile struct {
	VersionedBase
	Scope         value.ExternalRef
	Slug          string
	DisplayName   []value.LocalizedText
	Description   []value.LocalizedText
	Status        enum.RiskProfileStatus
	ActiveVersion *int64
}

// RiskProfileVersion is an immutable set of risk rules and gate policies.
type RiskProfileVersion struct {
	RiskProfileID  uuid.UUID
	ProfileVersion int64
	Status         enum.RiskProfileVersionStatus
	Rules          []RiskRule
	GatePolicies   []GatePolicy
	ContentDigest  string
	CreatedAt      time.Time
	ActivatedAt    *time.Time
}

// RiskRule is a versioned rule that contributes a minimum risk class.
type RiskRule struct {
	ID                   uuid.UUID
	RiskProfileID        uuid.UUID
	ProfileVersion       int64
	RuleKind             enum.RiskRuleKind
	MatcherJSON          []byte
	MinRiskClass         enum.RiskClass
	RequiredGatePolicyID *uuid.UUID
	ReasonTemplate       []value.LocalizedText
	Status               enum.RuleStatus
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// GatePolicy defines actor and signal requirements for a gate.
type GatePolicy struct {
	ID                     uuid.UUID
	RiskProfileID          *uuid.UUID
	ProfileVersion         int64
	GateKind               enum.GateKind
	MinRiskClass           enum.RiskClass
	RequiredActorPolicyRef string
	RequiredSignalKinds    []string
	TimeoutPolicyRef       string
	Status                 enum.RuleStatus
}

// RequiredGate describes a gate required by a risk assessment.
type RequiredGate struct {
	GatePolicyID uuid.UUID
	GateKind     enum.GateKind
	MinRiskClass enum.RiskClass
	Reason       string
}

// RiskAssessment records risk state for one target.
type RiskAssessment struct {
	VersionedBase
	Target             value.ExternalRef
	ProjectContext     value.ProjectContextRef
	ProviderContext    []byte
	AgentContext       []byte
	RuntimeContext     []byte
	RiskProfileID      *uuid.UUID
	RiskProfileVersion *int64
	EvaluationSummary  value.RiskEvaluationSummary
	EvidenceRefs       []value.EvidenceRef
	InitialRiskClass   enum.RiskClass
	EffectiveRiskClass enum.RiskClass
	Status             enum.RiskAssessmentStatus
	Explanation        string
	RequiredGates      []RequiredGate
}

// RiskFactor explains one reason for a risk class.
type RiskFactor struct {
	ID               uuid.UUID
	RiskAssessmentID uuid.UUID
	SourceType       enum.RiskFactorSourceType
	SourceRef        string
	RiskClass        enum.RiskClass
	Summary          string
	CreatedAt        time.Time
}

// ReviewSignal records a role-driven signal used as evidence.
type ReviewSignal struct {
	ID                uuid.UUID
	RiskAssessmentID  *uuid.UUID
	Target            value.ExternalRef
	RoleKind          enum.ReviewRoleKind
	AuthorRef         string
	Outcome           enum.ReviewSignalOutcome
	Severity          enum.SignalSeverity
	Confidence        enum.Confidence
	EvidenceRefs      []value.EvidenceRef
	Summary           string
	SourceFingerprint string
	CreatedAt         time.Time
}

// GateRequest describes a concrete governance gate.
type GateRequest struct {
	VersionedBase
	RiskAssessmentID       *uuid.UUID
	GatePolicyID           *uuid.UUID
	Target                 value.ExternalRef
	InteractionDeliveryRef value.InteractionDeliveryRef
	EvidenceRefs           []value.EvidenceRef
	EvidenceSummary        string
	Status                 enum.GateRequestStatus
	TerminalActorRef       string
	TerminalReason         string
	TerminalAt             *time.Time
}

// GateDecision records the final governance decision for a gate.
type GateDecision struct {
	ID                uuid.UUID
	GateRequestID     uuid.UUID
	DecisionActorRef  string
	DecisionPolicyRef string
	Outcome           enum.GateOutcome
	Reason            string
	ConditionsSummary string
	SourceRef         string
	DecidedAt         time.Time
}

// ReleaseDecisionPackage is a bounded evidence package for release decisions.
type ReleaseDecisionPackage struct {
	VersionedBase
	ReleaseCandidateRef     string
	ProjectContext          value.ProjectContextRef
	RepositoryRefs          []string
	RiskAssessmentID        *uuid.UUID
	ProviderRefs            []byte
	RuntimeRefs             []byte
	AgentContext            []byte
	ReviewSignalIDs         []uuid.UUID
	EvidenceRefs            []value.EvidenceRef
	IntegrationRefs         []value.ReleaseIntegrationRef
	KnownLimitationsSummary string
	Status                  enum.ReleaseDecisionPackageStatus
}

// ReleaseDecision records the release go/no-go decision for one package.
type ReleaseDecision struct {
	VersionedBase
	ReleaseDecisionPackageID uuid.UUID
	GateDecisionID           *uuid.UUID
	Outcome                  enum.ReleaseDecisionOutcome
	DecisionActorRef         string
	DecisionPolicyRef        string
	Reason                   string
	ConditionsSummary        string
	Status                   enum.ReleaseDecisionStatus
	DecidedAt                time.Time
}

// ReleaseSafetyState records current safety-loop state for one release package.
type ReleaseSafetyState struct {
	VersionedBase
	ReleaseDecisionPackageID uuid.UUID
	CurrentState             enum.ReleaseSafetyStateKind
	RuntimeJobRef            string
	BlockingSignalCount      int32
	LastStateReason          string
}

// BlockingSignal records a bounded signal that blocks transition or release.
type BlockingSignal struct {
	VersionedBase
	Target     value.ExternalRef
	SourceType enum.BlockingSignalSourceType
	SourceRef  string
	Severity   enum.SignalSeverity
	Summary    string
	Status     enum.BlockingSignalStatus
	ResolvedAt *time.Time
}

// GovernanceSummary is a bounded read model prepared for owner/staff UI.
type GovernanceSummary struct {
	Scope              GovernanceSummaryScope
	PendingDecisions   []GovernanceDecisionSummary
	CompletedDecisions []GovernanceDecisionSummary
	EvidenceSummaries  []GovernanceEvidenceSummary
	Diagnostics        []string
	Status             GovernanceSummaryStatus
}

// GovernanceSummaryScope bounds a summary request to a safe local or owner-domain ref.
type GovernanceSummaryScope struct {
	Target                   value.ExternalRef
	ProjectContext           value.ProjectContextRef
	ReleaseCandidateRef      string
	ReleaseDecisionPackageID *uuid.UUID
	IntegrationRef           value.ReleaseIntegrationRef
}

// GovernanceDecisionSummary is one typed decision or state summary.
type GovernanceDecisionSummary struct {
	Kind                     enum.GovernanceDecisionSummaryKind
	Attention                enum.GovernanceDecisionAttention
	ID                       string
	ParentID                 string
	Target                   value.ExternalRef
	ProjectContext           value.ProjectContextRef
	ReleaseCandidateRef      string
	ReleaseDecisionPackageID string
	RiskClass                enum.RiskClass
	ReviewOutcome            enum.ReviewSignalOutcome
	GateRequestStatus        enum.GateRequestStatus
	GateOutcome              enum.GateOutcome
	ReleasePackageStatus     enum.ReleaseDecisionPackageStatus
	ReleaseDecisionStatus    enum.ReleaseDecisionStatus
	ReleaseDecisionOutcome   enum.ReleaseDecisionOutcome
	BlockingSignalStatus     enum.BlockingSignalStatus
	Severity                 enum.SignalSeverity
	RequiredGateCount        int32
	SafeSummary              string
	EvidenceRefs             []value.EvidenceRef
	IntegrationRefs          []value.ReleaseIntegrationRef
	ProviderRefs             []byte
	RuntimeRefs              []byte
	AgentContext             []byte
	Version                  int64
	CreatedAt                time.Time
	UpdatedAt                time.Time
	ObservedAt               string
}

// GovernanceEvidenceSummary is one safe evidence/ref summary.
type GovernanceEvidenceSummary struct {
	SourceKind      string
	SourceRef       string
	Status          string
	Outcome         string
	SafeSummary     string
	ErrorCode       string
	Digest          string
	ObservedAt      string
	Version         string
	EvidenceRefs    []value.EvidenceRef
	IntegrationRefs []value.ReleaseIntegrationRef
}

// GovernanceSummaryStatus is the operator-ready rollup for live governance state.
type GovernanceSummaryStatus struct {
	Attention                 enum.GovernanceDecisionAttention
	MaxRiskClass              enum.RiskClass
	PendingDecisionCount      int32
	BlockedDecisionCount      int32
	CompletedDecisionCount    int32
	PendingGateCount          int32
	PendingRequiredGateCount  int32
	ActiveBlockingSignalCount int32
	EvidenceCount             int32
	DiagnosticCount           int32
	SummaryCode               string
	NextActionCode            string
}

// CommandResult stores the idempotent command trace.
type CommandResult struct {
	Key            string
	CommandID      *uuid.UUID
	IdempotencyKey string
	Actor          value.Actor
	Operation      string
	AggregateType  string
	AggregateID    uuid.UUID
	ResultPayload  []byte
	CreatedAt      time.Time
}

// OutboxEvent is a service-local event waiting for platform event-log publication.
type OutboxEvent struct {
	outboxlib.Event
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailureKind         string
	FailedPermanentlyAt *time.Time
	PublishedAt         *time.Time
	LastError           string
}
