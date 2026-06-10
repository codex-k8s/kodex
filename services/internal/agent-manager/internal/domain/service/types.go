package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type CreateFlowInput struct {
	Meta          value.CommandMeta
	Scope         value.ScopeRef
	Slug          string
	DisplayName   []value.LocalizedText
	Description   []value.LocalizedText
	IconObjectURI string
}

type UpdateFlowInput struct {
	Meta          value.CommandMeta
	FlowID        uuid.UUID
	DisplayName   []value.LocalizedText
	Description   []value.LocalizedText
	IconObjectURI string
	Status        enum.FlowStatus
}

type CreateFlowVersionInput struct {
	Meta             value.CommandMeta
	FlowID           uuid.UUID
	SourceRef        string
	DefinitionDigest string
	Stages           []StageInput
	Transitions      []StageTransitionInput
	RoleBindings     []StageRoleBindingInput
}

type StageInput struct {
	Slug                  string
	StageType             enum.StageType
	DisplayName           []value.LocalizedText
	IconObjectURI         string
	RequiredArtifactsJSON []byte
	AcceptancePolicyJSON  []byte
	Position              int32
}

type StageTransitionInput struct {
	FromStageSlug *string
	ToStageSlug   string
	ConditionJSON []byte
	FollowUpType  string
	Position      int32
}

type StageRoleBindingInput struct {
	StageSlug             string
	RoleProfileID         uuid.UUID
	BindingKind           enum.StageRoleBindingKind
	LaunchPolicyJSON      []byte
	RequiredForAcceptance bool
}

type ActivateFlowVersionInput struct {
	Meta          value.CommandMeta
	FlowVersionID uuid.UUID
}

type CreateRoleProfileInput struct {
	Meta                     value.CommandMeta
	Scope                    value.ScopeRef
	Slug                     string
	DisplayName              []value.LocalizedText
	IconObjectURI            string
	RoleKind                 enum.RoleKind
	RuntimeProfile           string
	AllowedMCPTools          []string
	ProviderAccountPolicyRef string
}

type UpdateRoleProfileInput struct {
	Meta                     value.CommandMeta
	RoleProfileID            uuid.UUID
	DisplayName              []value.LocalizedText
	IconObjectURI            string
	RoleKind                 enum.RoleKind
	RuntimeProfile           string
	AllowedMCPTools          []string
	ProviderAccountPolicyRef string
	Status                   enum.RoleStatus
}

type CreatePromptTemplateInput struct {
	Meta          value.CommandMeta
	RoleProfileID uuid.UUID
	PromptKind    enum.PromptKind
}

type CreatePromptTemplateVersionInput struct {
	Meta           value.CommandMeta
	RoleProfileID  uuid.UUID
	PromptKind     enum.PromptKind
	SourceRef      string
	TemplateObject value.ObjectRef
	TemplateDigest string
}

type ActivatePromptTemplateVersionInput struct {
	Meta                    value.CommandMeta
	PromptTemplateVersionID uuid.UUID
}

type FlowList = query.FlowFilter
type RoleProfileList = query.RoleProfileFilter
type PromptTemplateList = query.PromptTemplateFilter
type PromptTemplateVersionList = query.PromptTemplateVersionFilter
type SelfDeployPlanList = query.SelfDeployPlanFilter

type FlowVersionResult struct {
	FlowVersion entity.FlowVersion
	Flow        entity.Flow
}

type StartAgentSessionInput struct {
	Meta                value.CommandMeta
	Scope               value.ScopeRef
	ProviderWorkItemRef string
	FlowVersionID       *uuid.UUID
	CurrentStageID      *uuid.UUID
	CreatedByActorRef   string
}

type StartAgentRunInput struct {
	Meta                    value.CommandMeta
	SessionID               uuid.UUID
	FlowVersionID           *uuid.UUID
	StageID                 *uuid.UUID
	RoleProfileID           uuid.UUID
	PromptTemplateVersionID uuid.UUID
	ProviderTarget          value.ProviderTargetRef
	GuidanceSelectionHints  []value.GuidanceSelectionHint
}

type RunnerRunState string

const (
	RunnerRunStateQueued    RunnerRunState = "queued"
	RunnerRunStateRunning   RunnerRunState = "running"
	RunnerRunStateCompleted RunnerRunState = "completed"
	RunnerRunStateFailed    RunnerRunState = "failed"
	RunnerRunStateStarted   RunnerRunState = "started"
	RunnerRunStateCancelled RunnerRunState = "cancelled"
	RunnerRunStateTimedOut  RunnerRunState = "timed_out"
)

type ReportAgentRunStateInput struct {
	Meta             value.CommandMeta
	RunID            uuid.UUID
	SessionID        uuid.UUID
	RuntimeSlotRef   string
	RuntimeJobRef    string
	State            RunnerRunState
	SafeSummary      *string
	FailureCode      *string
	DiagnosticDigest *string
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

type CreateSelfDeployPlanInput struct {
	Meta                    value.CommandMeta
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
}

type CreateSelfDeployPlanFromSignalInput struct {
	CreateSelfDeployPlanInput
}

type EnsureSelfDeployPlanGovernanceGateInput struct {
	Meta             value.CommandMeta
	SelfDeployPlanID uuid.UUID
}

type RecordSelfDeployPlanGateDecisionInput struct {
	Meta             value.CommandMeta
	SelfDeployPlanID uuid.UUID
	GateRequestRef   string
	GateDecisionRef  string
	Outcome          SelfDeployPlanGateDecisionOutcome
	SafeSummary      string
}

type SelfDeployPlanGateStatus string

const (
	SelfDeployPlanGateStatusPending  SelfDeployPlanGateStatus = "pending"
	SelfDeployPlanGateStatusApproved SelfDeployPlanGateStatus = "approved"
	SelfDeployPlanGateStatusRejected SelfDeployPlanGateStatus = "rejected"
	SelfDeployPlanGateStatusBlocked  SelfDeployPlanGateStatus = "blocked"
)

type SelfDeployPlanGateDecisionOutcome string

const (
	SelfDeployPlanGateDecisionOutcomeApprove               SelfDeployPlanGateDecisionOutcome = "approve"
	SelfDeployPlanGateDecisionOutcomeApproveWithConditions SelfDeployPlanGateDecisionOutcome = "approve_with_conditions"
	SelfDeployPlanGateDecisionOutcomeReject                SelfDeployPlanGateDecisionOutcome = "reject"
	SelfDeployPlanGateDecisionOutcomeRevise                SelfDeployPlanGateDecisionOutcome = "revise"
	SelfDeployPlanGateDecisionOutcomeRequestChanges        SelfDeployPlanGateDecisionOutcome = "request_changes"
	SelfDeployPlanGateDecisionOutcomeHold                  SelfDeployPlanGateDecisionOutcome = "hold"
	SelfDeployPlanGateDecisionOutcomeRollback              SelfDeployPlanGateDecisionOutcome = "rollback"
	SelfDeployPlanGateDecisionOutcomeEscalate              SelfDeployPlanGateDecisionOutcome = "escalate"
)

type SelfDeployPlanGatePreparationInput struct {
	Meta value.CommandMeta
	Plan entity.SelfDeployPlan
}

type SelfDeployPlanGatePreparationResult struct {
	Status            SelfDeployPlanGateStatus
	GovernanceContext value.GovernanceContextRef
	SafeSummary       string
}

type SelfDeploySignalStatus string

const (
	SelfDeploySignalStatusReady                        SelfDeploySignalStatus = "ready"
	SelfDeploySignalStatusNeedsServicesPolicyReconcile SelfDeploySignalStatus = "needs_services_policy_reconcile"
	SelfDeploySignalStatusProviderSignalNotFound       SelfDeploySignalStatus = "provider_signal_not_found"
	SelfDeploySignalStatusProviderSignalNotReady       SelfDeploySignalStatus = "provider_signal_not_ready"
	SelfDeploySignalStatusRepositoryBindingNotFound    SelfDeploySignalStatus = "repository_binding_not_found"
	SelfDeploySignalStatusServicesPolicyNotFound       SelfDeploySignalStatus = "services_policy_not_found"
	SelfDeploySignalStatusServicesPolicyNotReady       SelfDeploySignalStatus = "services_policy_not_ready"
	SelfDeploySignalStatusNotDeployRelevant            SelfDeploySignalStatus = "not_deploy_relevant"
	SelfDeploySignalStatusNeedsRepositoryChangeSummary SelfDeploySignalStatus = "needs_repository_change_summary"
)

type SelfDeploySignalLookupInput struct {
	Meta              value.CommandMeta
	ProjectID         uuid.UUID
	RepositoryID      *uuid.UUID
	ProviderSignalID  string
	ProviderSignalKey string
}

type SelfDeploySignalServicesYAML struct {
	Ref         string
	Digest      string
	Fingerprint string
	Version     int64
}

type SelfDeployGovernanceRequirement struct {
	GateRequired   bool
	RiskProfileRef string
	GatePolicyRef  string
}

type SelfDeploySignal struct {
	ProviderSignalRef         string
	ProviderSignalID          string
	ProviderSignalKey         string
	ProjectRef                string
	RepositoryRef             string
	SourceRef                 string
	MergeCommitSHA            string
	ServicesYAML              SelfDeploySignalServicesYAML
	AffectedServiceKeys       []string
	PathCategories            []enum.SelfDeployPathCategory
	ExpectedRuntimeJobTypes   []enum.SelfDeployRuntimeJobType
	GovernanceRequirement     SelfDeployGovernanceRequirement
	ProviderChangeFingerprint string
	ProjectSignalFingerprint  string
	SafeSummary               string
	Version                   int64
}

type SelfDeploySignalReadResult struct {
	Status     SelfDeploySignalStatus
	Signal     SelfDeploySignal
	SafeReason string
}

type SelfDeployBuildPlanStatus string

const (
	SelfDeployBuildPlanStatusReady                   SelfDeployBuildPlanStatus = "ready"
	SelfDeployBuildPlanStatusBuildPlanUnavailable    SelfDeployBuildPlanStatus = "build_plan_unavailable"
	SelfDeployBuildPlanStatusPolicyStale             SelfDeployBuildPlanStatus = "policy_stale"
	SelfDeployBuildPlanStatusServiceNotFound         SelfDeployBuildPlanStatus = "service_not_found"
	SelfDeployBuildPlanStatusInvalidInput            SelfDeployBuildPlanStatus = "invalid_input"
	SelfDeployBuildPlanStatusBuildContextUnavailable SelfDeployBuildPlanStatus = "build_context_unavailable"
)

type SelfDeployBuildPlanLookupInput struct {
	Meta                              value.CommandMeta
	ProjectID                         uuid.UUID
	RepositoryID                      uuid.UUID
	SourceRef                         string
	MergeCommitSHA                    string
	ProviderSignalRef                 string
	ProviderSignalID                  string
	ProviderSignalKey                 string
	AffectedServiceKeys               []string
	ExpectedServicesPolicyDigest      string
	ExpectedServicesPolicyFingerprint string
	ExpectedServicesPolicyVersion     *int64
}

type RuntimeJobAllowedSecretRef struct {
	SecretRef string
	Purpose   string
}

type RuntimeJobOutputRef struct {
	Kind string
	Ref  string
}

type SelfDeployBuildSourceSpec struct {
	SourceRef       string
	SourceCommitSHA string
}

type SelfDeployBuildImageSpec struct {
	ImageRef    string
	ImageTag    string
	ImageDigest string
}

type SelfDeployBuildContextSpec struct {
	BuildContextRef      string
	BuildContextDigest   string
	BuildPlanFingerprint string
}

type SelfDeployBuildDockerfileSpec struct {
	DockerfileRef    string
	DockerfileDigest string
	DockerfileTarget string
}

type SelfDeployBuildExecutionSpec struct {
	SelfDeployBuildSourceSpec
	SelfDeployBuildImageSpec
	SelfDeployBuildContextSpec
	SelfDeployBuildDockerfileSpec
	ServiceKey        string
	BuilderImageRef   string
	AllowedSecretRefs []RuntimeJobAllowedSecretRef
	OutputRefs        []RuntimeJobOutputRef
}

type SelfDeployBuildPlanItem struct {
	ServiceKey          string
	ServiceRef          string
	BuildExecutionSpec  SelfDeployBuildExecutionSpec
	PlanItemFingerprint string
}

type SelfDeployBuildPlan struct {
	ProjectRef          string
	RepositoryRef       string
	ProviderSignalRef   string
	SourceRef           string
	MergeCommitSHA      string
	ServicesYAML        SelfDeploySignalServicesYAML
	AffectedServiceKeys []string
	BuildItems          []SelfDeployBuildPlanItem
	PlanFingerprint     string
	SafeSummary         string
	Version             int64
}

type SelfDeployBuildPlanReadResult struct {
	Status     SelfDeployBuildPlanStatus
	Plan       SelfDeployBuildPlan
	SafeReason string
}

const (
	RuntimeModeFullEnv = "full_env"

	RuntimeJobRunnerModeCodexAgent = "codex_agent"

	RuntimeSlotStatusReady  = "ready"
	RuntimeSlotStatusFailed = "failed"

	RuntimeWorkspaceMaterializationStatusCompleted = "completed"
	RuntimeWorkspaceMaterializationStatusFailed    = "failed"
	RuntimeWorkspaceMaterializationStatusCancelled = "cancelled"

	WorkspaceSourceKindCode             = "code"
	WorkspaceSourceKindDocumentation    = "documentation"
	WorkspaceSourceKindGuidancePackage  = "guidance_package"
	WorkspaceSourceKindGeneratedContext = "generated_context"

	WorkspaceSourceAccessRead  = "read"
	WorkspaceSourceAccessWrite = "write"
)

type WorkspacePolicySnapshot struct {
	ProjectID             uuid.UUID
	CodeSources           []WorkspaceCodeSource
	DocumentationSources  []WorkspaceDocumentationSource
	GuidancePackageRefs   []string
	ActivePolicyOverrides []PolicyOverrideRef
	PolicyVersion         int64
}

type WorkspaceCodeSource struct {
	RepositoryID  uuid.UUID
	Provider      string
	ProviderOwner string
	ProviderName  string
	DefaultBranch string
	LocalPath     string
	AccessMode    string
}

type WorkspaceDocumentationSource struct {
	DocumentationSourceID uuid.UUID
	RepositoryID          *uuid.UUID
	ScopeType             string
	ScopeID               string
	LocalPath             string
	AccessMode            string
}

type PolicyOverrideRef struct {
	ID string
}

type RuntimeWorkspacePolicy struct {
	ProjectID               uuid.UUID
	PolicyDigest            string
	PolicyVersion           int64
	Sources                 []RuntimeWorkspaceSource
	ActivePolicyOverrideIDs []string
}

type RuntimeWorkspaceSource struct {
	SourceID      string
	Kind          string
	RepositoryID  *uuid.UUID
	Provider      string
	ProviderOwner string
	ProviderName  string
	SourceRef     string
	CommitSHA     string
	LocalPath     string
	AccessMode    string
	Digest        string
	MetadataJSON  string
}

type RuntimePlacementConstraints struct {
	ProjectID             uuid.UUID
	RepositoryIDs         []uuid.UUID
	ServiceKeys           []string
	RuntimeProfile        string
	PreferredFleetScopeID *uuid.UUID
	RequiredCapabilities  []string
	MetadataJSON          string
}

type RuntimePreparationInput struct {
	Meta                 value.CommandMeta
	AgentRunID           uuid.UUID
	RuntimeProfile       string
	RuntimeMode          string
	WorkspacePolicy      RuntimeWorkspacePolicy
	PlacementConstraints RuntimePlacementConstraints
}

type RuntimePreparationResult struct {
	SlotRef                        string
	SlotStatus                     string
	WorkspaceRef                   string
	WorkspaceMaterializationStatus string
	ContextRef                     string
	ContextDigest                  string
	MaterializationFingerprint     string
	DiagnosticSummary              string
}

type RuntimeJobInput struct {
	Meta          value.CommandMeta
	AgentRunID    uuid.UUID
	SlotRef       string
	ExecutionSpec AgentRunExecutionSpec
}

type SelfDeployBuildJobInput struct {
	Meta                  value.CommandMeta
	ProjectID             uuid.UUID
	RepositoryID          uuid.UUID
	PlanID                uuid.UUID
	ServiceKey            string
	ServiceRef            string
	PlanFingerprint       string
	PlanItemFingerprint   string
	BuildExecutionSpec    SelfDeployBuildExecutionSpec
	GovernanceApprovalRef string
	GovernanceGateRef     string
}

type AgentRunExecutionSpec struct {
	AgentRunID                         uuid.UUID
	SlotID                             uuid.UUID
	ExpectedMaterializationID          uuid.UUID
	ExpectedMaterializationFingerprint string
	WorkspaceRef                       string
	WorkspaceMountRef                  string
	WorkspacePVCRef                    string
	ContextRef                         string
	ContextDigest                      string
	RunnerProfileRef                   string
	RunnerImageRef                     string
	RunnerMode                         string
	AllowedSecretRefs                  []AgentRunExecutionRef
	ReportingTargetRefs                []AgentRunExecutionRef
	CodexSessionExecutionSpec          *CodexSessionExecutionSpec
}

type AgentRunExecutionRef struct {
	Kind string
	Ref  string
}

type CodexSessionExecutionSpec struct {
	CodexSessionExecutionInputRefs
	CodexSessionExecutionIORefs
}

type CodexSessionExecutionInputRefs struct {
	InstructionObjectRef    string
	InstructionObjectDigest string
	ResultSchemaRef         string
	ResultSchemaDigest      string
	SessionSnapshotRef      string
	WorkspaceSnapshotRef    string
	HookEndpointRef         string
	CallbackRefs            []AgentRunExecutionRef
}

type CodexSessionExecutionIORefs struct {
	TimeoutSeconds    int32
	RunnerProfileRef  string
	RunnerMode        string
	OutputRefs        []AgentRunExecutionRef
	ResultRefs        []AgentRunExecutionRef
	AllowedSecretRefs []AgentRunExecutionRef
}

type CodexSessionExecutionConfig struct {
	ResultSchemaRef    string
	ResultSchemaDigest string
	HookEndpointRef    string
	TimeoutSeconds     int32
}

type RuntimeJobResult struct {
	JobRef            string
	Status            string
	DiagnosticSummary string
}

type GetAgentRunRuntimeStatusInput struct {
	Meta  value.QueryMeta
	RunID uuid.UUID
}

type AgentRunRuntimeStatusResult struct {
	Run           entity.AgentRun
	RuntimeStatus AgentRunRuntimeStatus
}

type RuntimeObservationState string

const (
	RuntimeObservationStateNotCreated  RuntimeObservationState = "not_created"
	RuntimeObservationStateStoredRef   RuntimeObservationState = "stored_ref"
	RuntimeObservationStateLive        RuntimeObservationState = "live"
	RuntimeObservationStateUnavailable RuntimeObservationState = "unavailable"
	RuntimeObservationStateConflict    RuntimeObservationState = "conflict"
)

type RuntimeJobStatus string

const (
	RuntimeJobStatusPending   RuntimeJobStatus = "pending"
	RuntimeJobStatusClaimed   RuntimeJobStatus = "claimed"
	RuntimeJobStatusRunning   RuntimeJobStatus = "running"
	RuntimeJobStatusSucceeded RuntimeJobStatus = "succeeded"
	RuntimeJobStatusFailed    RuntimeJobStatus = "failed"
	RuntimeJobStatusCancelled RuntimeJobStatus = "cancelled"
	RuntimeJobStatusTimedOut  RuntimeJobStatus = "timed_out"
)

type RuntimeJobReadInput struct {
	Meta       value.QueryMeta
	AgentRunID uuid.UUID
	JobRef     string
}

type RuntimeJobReadResult struct {
	JobRef           string
	AgentRunID       uuid.UUID
	CommandRef       string
	Status           RuntimeJobStatus
	Version          int64
	CreatedAt        *time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	UpdatedAt        *time.Time
	SafeErrorCode    string
	SafeErrorSummary string
	SafeSummary      string
}

type AgentRunRuntimeStatus struct {
	RunID                uuid.UUID
	RunStatus            enum.AgentRunStatus
	RuntimeContext       value.RuntimeContextRef
	ObservationState     RuntimeObservationState
	RuntimeJobRef        string
	RuntimeJobStatus     RuntimeJobStatus
	RuntimeJobCommandRef string
	RuntimeJobVersion    int64
	RuntimeJobCreatedAt  *time.Time
	RuntimeJobStartedAt  *time.Time
	RuntimeJobFinishedAt *time.Time
	RuntimeJobUpdatedAt  *time.Time
	SafeErrorCode        string
	SafeSummary          string
	RunStartedAt         *time.Time
	RunFinishedAt        *time.Time
	RunUpdatedAt         time.Time
	RunVersion           int64
	HumanGateWaiting     bool
	HumanGateRequestRef  string
	HumanGateReasonCode  string
	FollowUpWaiting      bool
}

type ProviderOperationStatus string

const (
	ProviderOperationStatusSucceeded       ProviderOperationStatus = "succeeded"
	ProviderOperationStatusFailed          ProviderOperationStatus = "failed"
	ProviderOperationStatusRetryableFailed ProviderOperationStatus = "retryable_failed"
	ProviderOperationStatusDenied          ProviderOperationStatus = "denied"
	ProviderOperationStatusInProgress      ProviderOperationStatus = "in_progress"
)

type FollowUpDispatchKind string

const (
	FollowUpDispatchKindCreateIssue        FollowUpDispatchKind = "create_issue"
	FollowUpDispatchKindUpdateIssue        FollowUpDispatchKind = "update_issue"
	FollowUpDispatchKindCreateComment      FollowUpDispatchKind = "create_comment"
	FollowUpDispatchKindUpdateComment      FollowUpDispatchKind = "update_comment"
	FollowUpDispatchKindUpdatePullRequest  FollowUpDispatchKind = "update_pull_request"
	FollowUpDispatchKindCreateReviewSignal FollowUpDispatchKind = "create_review_signal"
)

const (
	ProviderOperationTypeCreateIssue         = "create_issue"
	ProviderOperationTypeUpdateIssue         = "update_issue"
	ProviderOperationTypeCreateComment       = "create_comment"
	ProviderOperationTypeUpdateComment       = "update_comment"
	ProviderOperationTypeUpdatePullRequest   = "update_pull_request"
	ProviderOperationTypeCreateReviewSignal  = "create_review_signal"
	ProviderRiskLevelLow                     = "low"
	ProviderRiskLevelMedium                  = "medium"
	ProviderRiskLevelHigh                    = "high"
	ProviderRiskLevelCritical                = "critical"
	ProviderReviewSignalKindComment          = "comment"
	ProviderReviewSignalKindApproval         = "approval"
	ProviderReviewSignalKindChangesRequested = "changes_requested"
)

type ProviderReviewSignalKind string

type ProviderCommandTarget struct {
	ProviderSlug         string
	RepositoryFullName   string
	ProviderRepositoryID string
	WorkItemKind         string
	Number               int64
	ProviderObjectID     string
	WebURL               string
}

type ProviderOperationPolicyContext struct {
	ProjectID         string
	RepositoryID      string
	Stage             string
	RoleID            string
	RoleKey           string
	OperationType     string
	TargetRef         string
	ChangedFields     []string
	RiskTags          []string
	RiskLevel         string
	ApprovalRequired  bool
	PolicyVersion     string
	PolicySnapshotRef string
}

type ProviderApprovalGateReference struct {
	ApprovalID       string
	GateType         string
	Decision         string
	DecidedByActorID string
	DecidedAt        string
	EvidenceRef      string
	PolicyVersion    string
}

type DispatchFollowUpIntentInput struct {
	Meta                   value.CommandMeta
	FollowUpIntentID       uuid.UUID
	DispatchKind           FollowUpDispatchKind
	OperationPolicyContext ProviderOperationPolicyContext
	ApprovalGateRef        ProviderApprovalGateReference
	CreateIssue            *FollowUpCreateIssueCommand
	UpdateIssue            *FollowUpUpdateIssueCommand
	CreateComment          *FollowUpCreateCommentCommand
	UpdateComment          *FollowUpUpdateCommentCommand
	UpdatePullRequest      *FollowUpUpdatePullRequestCommand
	CreateReviewSignal     *FollowUpCreateReviewSignalCommand
}

type FollowUpCreateIssueCommand struct {
	ProjectID              uuid.UUID
	RepositoryID           uuid.UUID
	ProviderSlug           string
	ExternalAccountID      uuid.UUID
	RepositoryTarget       ProviderCommandTarget
	SafeBodyHint           string
	Labels                 []string
	AssigneeProviderLogins []string
	Milestone              string
	WatermarkJSON          []byte
}

type FollowUpUpdateIssueCommand struct {
	ExternalAccountID       uuid.UUID
	Target                  ProviderCommandTarget
	SafeTitle               *string
	SafeBodyHint            *string
	Labels                  *ProviderStringListPatch
	AssigneeProviderLogins  *ProviderStringListPatch
	Milestone               *string
	State                   *string
	ProviderWorkItemType    *string
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
}

type FollowUpCreateCommentCommand struct {
	ExternalAccountID uuid.UUID
	Target            ProviderCommandTarget
	SafeBodyHint      string
}

type FollowUpUpdateCommentCommand struct {
	ExternalAccountID       uuid.UUID
	Target                  ProviderCommandTarget
	ProviderCommentID       string
	SafeBodyHint            string
	ExpectedProviderVersion string
}

type FollowUpUpdatePullRequestCommand struct {
	ExternalAccountID       uuid.UUID
	Target                  ProviderCommandTarget
	SafeTitle               *string
	SafeBodyHint            *string
	Labels                  *ProviderStringListPatch
	AssigneeProviderLogins  *ProviderStringListPatch
	Milestone               *string
	State                   *string
	BaseBranch              *string
	MaintainerCanModify     *bool
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
}

type FollowUpCreateReviewSignalCommand struct {
	ExternalAccountID uuid.UUID
	Target            ProviderCommandTarget
	Kind              ProviderReviewSignalKind
	SafeBodyHint      *string
	InlineComments    []ProviderReviewInlineComment
}

type ProviderStringListPatch struct {
	Values []string
}

type ProviderReviewInlineComment struct {
	Path                       string
	Body                       string
	Line                       *int64
	StartLine                  *int64
	Side                       string
	StartSide                  string
	InReplyToProviderCommentID string
}

type ProviderCreateIssueInput struct {
	Meta                   value.CommandMeta
	ProjectID              uuid.UUID
	RepositoryID           uuid.UUID
	ProviderSlug           string
	RepositoryTarget       ProviderCommandTarget
	Title                  string
	Body                   string
	Labels                 []string
	AssigneeProviderLogins []string
	Milestone              string
	WorkItemType           string
	WatermarkJSON          []byte
	OperationPolicyContext ProviderOperationPolicyContext
	ApprovalGateRef        ProviderApprovalGateReference
	ExternalAccountID      uuid.UUID
}

type ProviderUpdateIssueInput struct {
	Meta                    value.CommandMeta
	Target                  ProviderCommandTarget
	Title                   *string
	Body                    *string
	Labels                  *ProviderStringListPatch
	AssigneeProviderLogins  *ProviderStringListPatch
	Milestone               *string
	State                   *string
	WorkItemType            *string
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
	OperationPolicyContext  ProviderOperationPolicyContext
	ApprovalGateRef         ProviderApprovalGateReference
	ExternalAccountID       uuid.UUID
}

type ProviderCreateCommentInput struct {
	Meta                   value.CommandMeta
	Target                 ProviderCommandTarget
	Body                   string
	OperationPolicyContext ProviderOperationPolicyContext
	ApprovalGateRef        ProviderApprovalGateReference
	ExternalAccountID      uuid.UUID
}

type ProviderUpdateCommentInput struct {
	Meta                    value.CommandMeta
	Target                  ProviderCommandTarget
	ProviderCommentID       string
	Body                    string
	ExpectedProviderVersion string
	OperationPolicyContext  ProviderOperationPolicyContext
	ApprovalGateRef         ProviderApprovalGateReference
	ExternalAccountID       uuid.UUID
}

type ProviderUpdatePullRequestInput struct {
	Meta                    value.CommandMeta
	Target                  ProviderCommandTarget
	Title                   *string
	Body                    *string
	Labels                  *ProviderStringListPatch
	AssigneeProviderLogins  *ProviderStringListPatch
	Milestone               *string
	State                   *string
	BaseBranch              *string
	MaintainerCanModify     *bool
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
	OperationPolicyContext  ProviderOperationPolicyContext
	ApprovalGateRef         ProviderApprovalGateReference
	ExternalAccountID       uuid.UUID
}

type ProviderCreateReviewSignalInput struct {
	Meta                   value.CommandMeta
	Target                 ProviderCommandTarget
	Kind                   ProviderReviewSignalKind
	Body                   string
	InlineComments         []ProviderReviewInlineComment
	OperationPolicyContext ProviderOperationPolicyContext
	ApprovalGateRef        ProviderApprovalGateReference
	ExternalAccountID      uuid.UUID
}

type ProviderCommandResult struct {
	ProviderOperationRef string
	ResultRef            string
	ProviderObjectID     string
	ProviderVersion      string
	Target               ProviderCommandTarget
	Status               ProviderOperationStatus
	ErrorCode            string
	ErrorMessage         string
}

type RecordRunStateInput struct {
	Meta           value.CommandMeta
	RunID          uuid.UUID
	Status         enum.AgentRunStatus
	RuntimeContext *value.RuntimeContextRef
	ProviderTarget *value.ProviderTargetRef
	ResultSummary  *string
	FailureCode    *string
	ReasonCode     *string
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

type RecordSessionStateSnapshotInput struct {
	Meta         value.CommandMeta
	SessionID    uuid.UUID
	RunID        *uuid.UUID
	SnapshotKind enum.AgentSessionSnapshotKind
	TurnIndex    *int64
	Object       value.ObjectRef
	CapturedAt   time.Time
}

type SessionSnapshotResult struct {
	Snapshot entity.AgentSessionStateSnapshot
	Session  entity.AgentSession
}

type AgentRunList = query.AgentRunFilter
type AgentSessionSummaryList = query.AgentSessionFilter
type AgentRunSummaryList = query.AgentRunSummaryFilter

type RequestAcceptanceInput struct {
	Meta              value.CommandMeta
	SessionID         uuid.UUID
	RunID             *uuid.UUID
	StageID           *uuid.UUID
	CheckKinds        []enum.AcceptanceCheckKind
	TargetRef         string
	GovernanceContext value.GovernanceContextRef
}

type RecordAcceptanceResultInput struct {
	Meta               value.CommandMeta
	AcceptanceResultID uuid.UUID
	Status             enum.AcceptanceStatus
	TargetRef          string
	DetailsJSON        []byte
	GovernanceContext  value.GovernanceContextRef
}

type AcceptanceResultList = query.AcceptanceResultFilter

type CreateFollowUpIntentInput struct {
	Meta                  value.CommandMeta
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
	GovernanceContext     value.GovernanceContextRef
}

type RecordAgentActivityInput struct {
	Meta            value.CommandMeta
	SessionID       uuid.UUID
	RunID           *uuid.UUID
	TurnID          string
	ToolUseID       string
	ActivityKind    enum.AgentActivityKind
	ToolName        string
	ToolCategory    string
	Status          enum.AgentActivityStatus
	StartedAt       *time.Time
	FinishedAt      *time.Time
	DurationMs      *int64
	SafeSummary     string
	PayloadDigest   string
	BoundedError    string
	SafeRefsJSON    []byte
	SafeDetailsJSON []byte
	CorrelationID   string
}

type AgentActivityList = query.AgentActivityFilter

type RequestHumanGateInput struct {
	Meta                     value.CommandMeta
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
	GovernanceGateRequestRef string
	GovernanceContext        value.GovernanceContextRef
}

type HumanGateInteractionActorRef struct {
	Kind string
	Ref  string
}

type HumanGateInteractionExternalRef struct {
	Kind string
	Ref  string
}

type HumanGateInteractionAction struct {
	ActionKey        string
	LabelTemplateRef string
	Terminal         bool
}

type HumanGateInteractionRequestInput struct {
	Meta                     value.CommandMeta
	HumanGateRequestID       uuid.UUID
	Scope                    value.ScopeRef
	SourceOwnerRef           string
	IngressRef               string
	PromptSummary            string
	TargetRefs               []HumanGateInteractionActorRef
	ContextRefs              []HumanGateInteractionExternalRef
	AllowedActions           []HumanGateInteractionAction
	RiskClass                string
	ReminderPolicyRef        string
	GovernanceGateRequestRef string
}

type HumanGateInteractionRequestResult struct {
	InteractionRequestRef string
	Status                string
	SafeSummary           string
	Version               int64
}

type RecordHumanGateDecisionInput struct {
	Meta                           value.CommandMeta
	HumanGateRequestID             uuid.UUID
	Status                         enum.HumanGateStatus
	Outcome                        enum.HumanGateOutcome
	SafeSummary                    string
	InteractionRequestRef          string
	InteractionResponseRef         string
	InteractionResponseFingerprint string
	InteractionRequestVersion      int64
	GovernanceGateRequestRef       string
	GovernanceDecisionRef          string
	GovernanceContext              value.GovernanceContextRef
}

type HumanGateList = query.HumanGateFilter
