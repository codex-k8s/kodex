package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

// BacklogOperationInput identifies a contract operation that is outside the current slice.
type BacklogOperationInput struct {
	Operation enum.Operation
}

// CommandMeta carries idempotency and audit metadata for mutating use-cases.
type CommandMeta struct {
	CommandID       *uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           value.Actor
	Reason          string
	RequestID       string
	RequestContext  value.RequestContext
}

// QueryMeta carries actor and safe request metadata for authoritative reads.
type QueryMeta struct {
	Actor          value.Actor
	RequestID      string
	RequestContext value.RequestContext
}

// Clock supplies deterministic time to use-cases.
type Clock interface {
	Now() time.Time
}

// IDGenerator supplies deterministic identifiers to use-cases.
type IDGenerator interface {
	New() uuid.UUID
}

type CreateRiskProfileInput struct {
	Scope       value.ExternalRef
	Slug        string
	DisplayName []value.LocalizedText
	Description []value.LocalizedText
	Meta        CommandMeta
}

type CreateRiskProfileVersionInput struct {
	RiskProfileID uuid.UUID
	Rules         []entity.RiskRule
	GatePolicies  []entity.GatePolicy
	ContentDigest string
	Meta          CommandMeta
}

type ActivateRiskProfileVersionInput struct {
	RiskProfileID  uuid.UUID
	ProfileVersion int64
	Meta           CommandMeta
}

type ArchiveRiskProfileInput struct {
	RiskProfileID uuid.UUID
	Meta          CommandMeta
}

type EvaluateRiskInput struct {
	Target            value.ExternalRef
	ProjectContext    value.ProjectContextRef
	ProviderContext   []byte
	AgentContext      []byte
	RuntimeContext    []byte
	EvidenceRefs      []value.EvidenceRef
	RiskProfileRef    string
	EvaluationSummary value.RiskEvaluationSummary
	Meta              CommandMeta
}

type ReevaluateRiskInput struct {
	RiskAssessmentID  uuid.UUID
	NewEvidenceRefs   []value.EvidenceRef
	Reason            string
	RiskProfileRef    string
	EvaluationSummary value.RiskEvaluationSummary
	Meta              CommandMeta
}

type RecordReviewSignalInput struct {
	RiskAssessmentID *uuid.UUID
	Target           value.ExternalRef
	RoleKind         enum.ReviewRoleKind
	AuthorRef        string
	Outcome          enum.ReviewSignalOutcome
	Severity         enum.SignalSeverity
	Confidence       enum.Confidence
	EvidenceRefs     []value.EvidenceRef
	Summary          string
	Meta             CommandMeta
}

type RequestGateInput struct {
	RiskAssessmentID       *uuid.UUID
	GatePolicyID           *uuid.UUID
	Target                 value.ExternalRef
	InteractionDeliveryRef value.InteractionDeliveryRef
	EvidenceRefs           []value.EvidenceRef
	EvidenceSummary        string
	Meta                   CommandMeta
}

type SelfDeployPlanGateInput struct {
	SelfDeployPlanRef       string
	ProjectContext          value.ProjectContextRef
	ProviderSignalRef       string
	SourceRef               string
	MergeCommitSHA          string
	ServicesYAMLRef         string
	ServicesYAMLDigest      string
	AffectedServiceKeys     []string
	PathCategories          []string
	ExpectedRuntimeJobTypes []string
	ChangedFilesSummaryRef  string
	SafeSummary             string
	PlanFingerprint         string
	EvidenceRefs            []value.EvidenceRef
	RiskProfileRef          string
	Meta                    CommandMeta
}

type SelfDeployPlanGateResult struct {
	Status            enum.SelfDeployPlanGateStatus
	RiskAssessment    entity.RiskAssessment
	GateRequest       entity.GateRequest
	GateDecision      *entity.GateDecision
	GovernanceSummary entity.GovernanceSummary
}

type SubmitGateDecisionInput struct {
	GateRequestID          uuid.UUID
	DecisionActorRef       string
	DecisionPolicyRef      string
	Outcome                enum.GateOutcome
	Reason                 string
	ConditionsSummary      string
	InteractionDeliveryRef value.InteractionDeliveryRef
	SourceRef              string
	Meta                   CommandMeta
}

type CancelGateInput struct {
	GateRequestID          uuid.UUID
	Reason                 string
	InteractionDeliveryRef value.InteractionDeliveryRef
	Meta                   CommandMeta
}

type ExpireGateInput struct {
	GateRequestID          uuid.UUID
	Reason                 string
	InteractionDeliveryRef value.InteractionDeliveryRef
	Meta                   CommandMeta
}

type BuildReleaseDecisionPackageInput struct {
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
	Meta                    CommandMeta
}

type GetReleaseDecisionPackageInput struct {
	ReleaseDecisionPackageID uuid.UUID
	Meta                     QueryMeta
}

type RecordReleaseRuntimeEvidenceInput struct {
	ReleaseDecisionPackageID uuid.UUID
	RuntimeRefs              []byte
	EvidenceRefs             []value.EvidenceRef
	IntegrationRefs          []value.ReleaseIntegrationRef
	Meta                     CommandMeta
}

type RecordReleaseAgentEvidenceInput struct {
	ReleaseDecisionPackageID uuid.UUID
	AgentContext             []byte
	EvidenceRefs             []value.EvidenceRef
	IntegrationRefs          []value.ReleaseIntegrationRef
	Meta                     CommandMeta
}

type RequestReleaseDecisionInput struct {
	ReleaseDecisionPackageID uuid.UUID
	RequestGateIfRequired    bool
	Meta                     CommandMeta
}

type SubmitReleaseDecisionInput struct {
	ReleaseDecisionPackageID uuid.UUID
	GateDecisionID           *uuid.UUID
	Outcome                  enum.ReleaseDecisionOutcome
	DecisionActorRef         string
	DecisionPolicyRef        string
	Reason                   string
	ConditionsSummary        string
	Meta                     CommandMeta
}

type GetReleaseDecisionInput struct {
	ReleaseDecisionID uuid.UUID
	Meta              QueryMeta
}

type RecordBlockingSignalInput struct {
	Target     value.ExternalRef
	SourceType enum.BlockingSignalSourceType
	SourceRef  string
	Severity   enum.SignalSeverity
	Summary    string
	Meta       CommandMeta
}

type ResolveBlockingSignalInput struct {
	BlockingSignalID  uuid.UUID
	TerminalStatus    enum.BlockingSignalStatus
	ResolutionSummary string
	Meta              CommandMeta
}

type RecordReleaseSafetyStateInput struct {
	ReleaseDecisionPackageID uuid.UUID
	CurrentState             enum.ReleaseSafetyStateKind
	RuntimeJobRef            string
	LastStateReason          string
	Meta                     CommandMeta
}

type GetReleaseSafetyStateInput struct {
	ReleaseDecisionPackageID uuid.UUID
	Meta                     QueryMeta
}

type GetGovernanceSummaryInput struct {
	Scope entity.GovernanceSummaryScope
	Meta  QueryMeta
}

type ListRiskProfilesInput struct {
	Filter query.RiskProfileFilter
}

type ListRiskRulesInput struct {
	Filter query.RuleFilter
}

type ListGatePoliciesInput struct {
	Filter query.GatePolicyFilter
}

type GetRiskAssessmentInput struct {
	RiskAssessmentID uuid.UUID
	Meta             QueryMeta
}

type ListRiskAssessmentsInput struct {
	Filter query.RiskAssessmentFilter
	Meta   QueryMeta
}

type ListRiskFactorsInput struct {
	Filter query.RiskFactorFilter
	Meta   QueryMeta
}

type ListReviewSignalsInput struct {
	Filter query.ReviewSignalFilter
	Meta   QueryMeta
}

type ListGateRequestsInput struct {
	Filter query.GateRequestFilter
	Meta   QueryMeta
}

type ListGateDecisionsInput struct {
	Filter query.GateDecisionFilter
	Meta   QueryMeta
}

type GetGateRequestInput struct {
	GateRequestID uuid.UUID
	Meta          QueryMeta
}

type GetGateDecisionInput struct {
	GateDecisionID uuid.UUID
	GateRequestID  uuid.UUID
	Meta           QueryMeta
}

type ListReleaseDecisionPackagesInput struct {
	Filter query.ReleaseDecisionPackageFilter
	Meta   QueryMeta
}

type ListReleaseDecisionsInput struct {
	Filter query.ReleaseDecisionFilter
	Meta   QueryMeta
}

type ListBlockingSignalsInput struct {
	Filter query.BlockingSignalFilter
	Meta   QueryMeta
}
