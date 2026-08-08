package entity

import (
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

const (
	LegacyMigrationPrepared  = "PREPARED"
	LegacyMigrationCommitted = "COMMITTED"
	LegacyMigrationAborted   = "ABORTED"
	LegacyVerificationOK     = "VERIFIED"
	LegacyVerificationDrift  = "DRIFTED"

	LegacyDispositionMaterialize     = "MATERIALIZE"
	LegacyDispositionArchiveTerminal = "ARCHIVE_TERMINAL"
	LegacyDispositionRejectNonempty  = "REJECT_NONEMPTY"
)

// LegacySourceTables — закрытый inventory source schema Issue #247.
var LegacySourceTables = []string{
	"matter_codex_agent_delegation_callback_deliveries",
	"matter_codex_agent_delegation_callback_manifests",
	"matter_codex_agent_delegations",
	"matter_codex_agent_flows",
	"matter_codex_agent_profiles",
	"matter_codex_agent_prompt_templates",
	"matter_codex_agent_role_runtime_variables",
	"matter_codex_agent_roles",
	"matter_codex_agent_runs",
	"matter_codex_agent_session_turns",
	"matter_codex_agent_sessions",
	"matter_codex_audit_events",
	"matter_codex_automation_audit_events",
	"matter_codex_automation_schedules",
	"matter_codex_chat_participants",
	"matter_codex_chat_repositories",
	"matter_codex_chats",
	"matter_codex_cluster_admin_bindings",
	"matter_codex_cluster_bot_bindings",
	"matter_codex_cluster_delivery_fences",
	"matter_codex_cluster_dependencies",
	"matter_codex_cluster_prompt_templates",
	"matter_codex_cluster_revocations",
	"matter_codex_cluster_runtime_variable_bindings",
	"matter_codex_cluster_session_bindings",
	"matter_codex_cluster_subjects",
	"matter_codex_credentials",
	"matter_codex_github_accounts",
	"matter_codex_interaction_capabilities",
	"matter_codex_mattermost_bot_identities",
	"matter_codex_memory_embeddings",
	"matter_codex_memory_record_versions",
	"matter_codex_memory_records",
	"matter_codex_openai_accounts",
	"matter_codex_owner_attention_requests",
	"matter_codex_policy_revisions",
	"matter_codex_process_runs",
	"matter_codex_process_turns",
	"matter_codex_project_repositories",
	"matter_codex_project_runtime_variables",
	"matter_codex_projects",
	"matter_codex_repositories",
	"matter_codex_role_capabilities",
	"matter_codex_role_relationship_policies",
	"matter_codex_runtime_agent_binding_discoveries",
	"matter_codex_runtime_agent_binding_outbox",
	"matter_codex_schedule_occurrences",
	"matter_codex_scheduled_runs",
	"matter_codex_thread_contexts",
	"matter_codex_work_claims",
}

type LegacySourceDisposition struct {
	SourceTable, Disposition, SourceSHA256, TerminalStateSHA256 string
	RowCount                                                    uint64
}

type LegacyOperationSource struct {
	SourceTable, SourceRef, SourceSHA256, LocalRef string
	SourceRevision                                 uint64
}

type LegacyProjectInput struct {
	Source                          LegacyOperationSource
	Name, Slug, Description, Locale string
}

type LegacyTeamInput struct {
	Source                           LegacyOperationSource
	Name, StableKey, ExternalTeamRef string
	RoleDefinitionRefs               []string
}

type LegacyChatInput struct {
	Source                                                                     LegacyOperationSource
	Name, StableKey, RoomType, ExternalChannelRef, WorkPolicy, DefaultAgentRef string
}

type LegacyArtifactInput struct {
	Source                                                               LegacyOperationSource
	Name, ArtifactKind, Direction, StorageRef, StorageVersion, MediaType string
	SHA256, RetentionPolicyRef, ScanEvidenceSHA256, ScannerWorkloadID    string
	SizeBytes, ScanPolicyRevision                                        uint64
	ScannedAt                                                            time.Time
}

type LegacyCredentialBindingInput struct {
	Source                                                      LegacyOperationSource
	Name, Purpose, SecretRef, ImmutableSecretRef, PrincipalRef  string
	ContentVersion, ContentSHA256                               string
	Revision, ObservedUsage, ObservedLimit, ObservationRevision uint64
	ProviderCapabilities                                        []string
	ObservedAt                                                  time.Time
}

type LegacyRepositoryWorkspaceInput struct {
	Source                                            LegacyOperationSource
	Name, RepositoryRef, WorkspaceMode, DefaultBranch string
	CredentialBindingRef, SnapshotArtifactRef         string
}

type LegacyRoleDefinitionInput struct {
	Source                                           LegacyOperationSource
	Name, StableKey, Description, RoleImageRecipeRef string
	Capabilities, AllowedRoleRefs                    []string
}

type LegacyInstructionSetInput struct {
	Source                                                                                LegacyOperationSource
	Name, StableKey, Locale, Content, ContentSHA256, ValidationSHA256, ContentArtifactRef string
}

type LegacyProviderReferenceInput struct {
	Source                                                                    LegacyOperationSource
	Name, StableKey, Provider, ServerReference, ReferenceSHA256               string
	MaskedLabel, MaskedStatus, ReceiptID, ReceiptSHA256, CredentialBindingRef string
	ReferenceVersion, ReferenceGeneration, ReceiptVersion                     uint64
	Capabilities                                                              []string
	ObservedAt                                                                time.Time
}

type LegacyProviderPoolBinding struct {
	ProviderReferenceRef string
	Weight               uint32
}

type LegacyProviderPoolInput struct {
	Source                                             LegacyOperationSource
	Name, StableKey, Policy, EligibilitySnapshotSHA256 string
	PolicyRevision                                     uint64
	ObservationMaxAge                                  time.Duration
	Bindings                                           []LegacyProviderPoolBinding
}

type LegacyRoleImageRecipeInput struct {
	Source                                              LegacyOperationSource
	Name                                                string
	Input                                               RoleImageRecipeInput
	Generation, PolicyRevision, RuntimeContractRevision uint64
	SpecSHA256, PolicySHA256, RuntimeContractSHA256     string
}

type LegacyImageBuildInput struct {
	Source                                                                       LegacyOperationSource
	Name, RecipeRef, ImmutableBuildSHA256, TerminalState, TerminalEvidenceSHA256 string
	StagingReference, ManifestDigest, ProvenanceSHA256                           string
	Attempt                                                                      uint32
}

type LegacyImageArtifactInput struct {
	Source                                                            LegacyOperationSource
	Name, RecipeRef, ImageBuildRef, ManifestDigest, PromotedReference string
	AdmissionReceiptSHA256, AdmissionReceiptManifestDigest            string
	SignatureSHA256, PromotionReadbackSHA256                          string
	SBOMSHA256, VulnerabilityEvidenceSHA256, SignatureIdentity        string
	AdmissionRevision                                                 uint64
	PromotedAt                                                        time.Time
}

type LegacyAgentInput struct {
	Source                                                                                   LegacyOperationSource
	Name, StableKey, RoleDefinitionRef, InstructionSetRef, ProviderPoolRef                   string
	RoleImageRecipeRef                                                                       string
	Capabilities                                                                             []string
	Enabled                                                                                  bool
	BotIdentityRef, BotUsername, BotTeamRef, BotMaskedStatus, BotReceiptID, BotReceiptSHA256 string
	BotReceiptVersion, BotProviderRevision, BotProviderGeneration                            uint64
}

type LegacyAgentAssignmentInput struct {
	Source                  LegacyOperationSource
	Name, AgentRef, RoomRef string
	AssignmentGeneration    uint64
}

type LegacyScheduleInput struct {
	Source                                                                  LegacyOperationSource
	Name, StableKey, CronExpression, Timezone, OverlapPolicy, MisfirePolicy string
	AgentRef, AssignmentRef, InstructionSetRef, ProviderPoolRef, RoomRef    string
	RoleImageRecipeRef, EffectiveInputSHA256                                string
	Calendar, DeliveryPolicy, SessionPolicy, NotificationPolicy             string
	MisfireGrace, InitialBackoff, MaximumBackoff, DeadLetterAfter           time.Duration
	MaximumExecutionDuration                                                time.Duration
	MaximumAttempts                                                         uint32
	Coalesce                                                                bool
	NextRunAt                                                               time.Time
	State                                                                   enum.State
}

type LegacyRuntimeComponent struct {
	LocalRef, ProjectionSHA256 string
}

type LegacyRuntimeRevisionInput struct {
	Source                                                                      LegacyOperationSource
	Name                                                                        string
	SessionRef, ChatRef, AgentRef, AssignmentRef, RoleDefinitionRef             string
	InstructionSetRef, ProviderPoolRef, ProviderCredentialRef                   string
	RoleImageRecipeRef, ImageBuildRef, ImageArtifactRef, PromptArtifactRef      string
	ManifestSHA256, ImageReference, EffectiveRuntimeSHA256, ProviderAccountName string
	CodexModel, CodexSandbox, CodexApprovalPolicy, AuthorityPolicySHA256        string
	AuthorityPolicyRevision                                                     uint64
	Components                                                                  []LegacyRuntimeComponent
	CreatedAt                                                                   time.Time
}

type LegacySessionInput struct {
	Source                                                              LegacyOperationSource
	Name, AgentRef, ProviderPoolRef, AssignmentRef, ChatRef, ArchiveRef string
	LastTurnSequence                                                    uint64
	State                                                               enum.State
}

type LegacyTurnInput struct {
	Source                                                                 LegacyOperationSource
	Name, SessionRef, SourceTurnRef, PromptArtifactRef, RuntimeRevisionRef string
	PredecessorTurnRef, ParentTurnRef, ProcessRunRef, EffectiveInputSHA256 string
	Outcome, ResultArtifactRef                                             string
	Sequence                                                               uint64
	Attempt                                                                uint32
	State                                                                  enum.State
}

type LegacyTurnAttemptInput struct {
	Source                                                            LegacyOperationSource
	TurnRef, ImmutableInputSHA256, RuntimeRevisionRef, State, Outcome string
	Attempt                                                           uint32
	StartedAt, FinishedAt                                             time.Time
}

type LegacyProcessRunInput struct {
	Source                                                                        LegacyOperationSource
	Name, RootSessionRef, RootTurnRef, RootAttemptRef, RuntimeRevisionRef         string
	ParentProcessRef, LaunchingTurnRef, LaunchingAttemptRef, ImmutableInputSHA256 string
	DelegationRef, TargetSessionRef, TargetTurnRef, TargetAttemptRef              string
	LegacyPolicySHA256, PlaybookRef, RootTriggerRef, Outcome                      string
	LegacyPolicyRevision                                                          uint64
	State                                                                         enum.State
}

type LegacyDelegationEdgeInput struct {
	Source                                                              LegacyOperationSource
	ParentProcessRef, ParentSessionRef, ParentTurnRef, ParentAttemptRef string
	ChildRoleRef, ChildSessionRef, ChildTurnRef, ChildAttemptRef        string
	ChildProcessRef                                                     string
	GrantGeneration                                                     uint64
}

type LegacyCallbackManifestInput struct {
	Source                                            LegacyOperationSource
	DelegationRef, CallbackProcessRef, ManifestSHA256 string
	Destinations                                      []string
}

type LegacyCallbackDeliveryInput struct {
	Source                                                         LegacyOperationSource
	CallbackManifestRef, Destination, ReceiptSHA256, TerminalState string
	DeliveredAt                                                    time.Time
}

type LegacyMemoryRecordInput struct {
	Source                                   LegacyOperationSource
	Name, MemoryKind, Content, ContentSHA256 string
	SourceVersion                            uint64
	State                                    enum.State
}

// LegacyGraphOperation повторяет Proto oneof типизированными domain values.
type LegacyGraphOperation struct {
	TargetID            string
	Project             *LegacyProjectInput             `json:"project,omitempty"`
	Team                *LegacyTeamInput                `json:"team,omitempty"`
	Chat                *LegacyChatInput                `json:"chat,omitempty"`
	Artifact            *LegacyArtifactInput            `json:"artifact,omitempty"`
	CredentialBinding   *LegacyCredentialBindingInput   `json:"credentialBinding,omitempty"`
	RepositoryWorkspace *LegacyRepositoryWorkspaceInput `json:"repositoryWorkspace,omitempty"`
	RoleDefinition      *LegacyRoleDefinitionInput      `json:"roleDefinition,omitempty"`
	InstructionSet      *LegacyInstructionSetInput      `json:"instructionSet,omitempty"`
	ProviderReference   *LegacyProviderReferenceInput   `json:"providerReference,omitempty"`
	ProviderPool        *LegacyProviderPoolInput        `json:"providerPool,omitempty"`
	RoleImageRecipe     *LegacyRoleImageRecipeInput     `json:"roleImageRecipe,omitempty"`
	ImageBuild          *LegacyImageBuildInput          `json:"imageBuild,omitempty"`
	ImageArtifact       *LegacyImageArtifactInput       `json:"imageArtifact,omitempty"`
	Agent               *LegacyAgentInput               `json:"agent,omitempty"`
	AgentAssignment     *LegacyAgentAssignmentInput     `json:"agentAssignment,omitempty"`
	Schedule            *LegacyScheduleInput            `json:"schedule,omitempty"`
	RuntimeRevision     *LegacyRuntimeRevisionInput     `json:"runtimeRevision,omitempty"`
	Session             *LegacySessionInput             `json:"session,omitempty"`
	Turn                *LegacyTurnInput                `json:"turn,omitempty"`
	TurnAttempt         *LegacyTurnAttemptInput         `json:"turnAttempt,omitempty"`
	ProcessRun          *LegacyProcessRunInput          `json:"processRun,omitempty"`
	DelegationEdge      *LegacyDelegationEdgeInput      `json:"delegationEdge,omitempty"`
	CallbackManifest    *LegacyCallbackManifestInput    `json:"callbackManifest,omitempty"`
	CallbackDelivery    *LegacyCallbackDeliveryInput    `json:"callbackDelivery,omitempty"`
	MemoryRecord        *LegacyMemoryRecordInput        `json:"memoryRecord,omitempty"`
}

type LegacyGraphPlan struct {
	PlanID, SourceRootReference, SourceRootSHA256, SourceSnapshotSHA256 string
	Dispositions                                                        []LegacySourceDisposition
	Operations                                                          []LegacyGraphOperation
}

type LegacyOperationReceipt struct {
	Ordinal                                                      uint32
	OperationKind, InputSHA256, TargetID                         string
	TargetKind                                                   string
	TargetVersion                                                uint64
	TargetState                                                  enum.State
	ProjectionSHA256, ProvenanceSHA256, ProvenanceEvidenceSHA256 string
	AuditIDs, EventIDs                                           []string
	EventSequences                                               []uint64
}

type LegacyGraphDrift struct {
	Ordinal   uint32
	Predicate string
}

type LegacyGraphMigration struct {
	PlanID, State, VerificationState, SemanticSHA256, SourceSnapshotSHA256, ProjectID string
	OperationCount, ArchivedSourceCount                                               uint32
	PreparedAt, TerminalAt                                                            time.Time
	OperationReceipts                                                                 []LegacyOperationReceipt
	Drift                                                                             []LegacyGraphDrift
}
