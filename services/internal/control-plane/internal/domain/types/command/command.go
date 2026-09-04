// Package command содержит закрытый реестр специализированных команд.
package command

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type Kind string

const (
	CompleteOnboarding              Kind = "COMPLETE_ONBOARDING"
	CreateProject                   Kind = "CREATE_PROJECT"
	UpdateProject                   Kind = "UPDATE_PROJECT"
	AddPlatformMembership           Kind = "ADD_PLATFORM_MEMBERSHIP"
	ChangePlatformMembership        Kind = "CHANGE_PLATFORM_MEMBERSHIP"
	RemovePlatformMembership        Kind = "REMOVE_PLATFORM_MEMBERSHIP"
	AddMembership                   Kind = "ADD_MEMBERSHIP"
	ChangeMembership                Kind = "CHANGE_MEMBERSHIP"
	RemoveMembership                Kind = "REMOVE_MEMBERSHIP"
	CreateAgent                     Kind = "CREATE_AGENT"
	UpdateAgent                     Kind = "UPDATE_AGENT"
	SetAgentEnabled                 Kind = "SET_AGENT_ENABLED"
	ArchiveAgent                    Kind = "ARCHIVE_AGENT"
	SetAgentAvatar                  Kind = "SET_AGENT_AVATAR"
	RemoveAgentAvatar               Kind = "REMOVE_AGENT_AVATAR"
	CreateInstructions              Kind = "CREATE_INSTRUCTION_DRAFT"
	ValidateInstructions            Kind = "VALIDATE_INSTRUCTION_DRAFT"
	PublishInstructions             Kind = "PUBLISH_INSTRUCTION_DRAFT"
	RollbackInstructions            Kind = "ROLLBACK_INSTRUCTIONS"
	PublishAgentRuntimeConfig       Kind = "PUBLISH_AGENT_RUNTIME_CONFIGURATION"
	CreateConfigOverlayDraft        Kind = "CREATE_CONFIG_OVERLAY_DRAFT"
	ValidateConfigOverlayDraft      Kind = "VALIDATE_CONFIG_OVERLAY_DRAFT"
	PublishConfigOverlayDraft       Kind = "PUBLISH_CONFIG_OVERLAY_DRAFT"
	RollbackConfigOverlay           Kind = "ROLLBACK_CONFIG_OVERLAY"
	CreateRuntimeEnvironment        Kind = "CREATE_RUNTIME_ENVIRONMENT_SET"
	CreateRuntimeEnvironmentDraft   Kind = "CREATE_RUNTIME_ENVIRONMENT_DRAFT"
	SaveRuntimeEnvironmentDraft     Kind = "SAVE_RUNTIME_ENVIRONMENT_DRAFT"
	ValidateRuntimeEnvironmentDraft Kind = "VALIDATE_RUNTIME_ENVIRONMENT_DRAFT"
	PublishRuntimeEnvironmentDraft  Kind = "PUBLISH_RUNTIME_ENVIRONMENT_DRAFT"
	DiscardRuntimeEnvironmentDraft  Kind = "DISCARD_RUNTIME_ENVIRONMENT_DRAFT"
	RebindRuntimeEnvironment        Kind = "REBIND_RUNTIME_ENVIRONMENT"
	RebindRuntimeSecret             Kind = "REBIND_RUNTIME_SECRET"
	BindInteractionIdentity         Kind = "BIND_INTERACTION_IDENTITY"
	RevokeInteractionIdentity       Kind = "REVOKE_INTERACTION_IDENTITY"
	PublishRuntimeEnvironment       Kind = "PUBLISH_RUNTIME_ENVIRONMENT_VERSION"
	RollbackRuntimeEnvironment      Kind = "ROLLBACK_RUNTIME_ENVIRONMENT"
	SetRuntimeEnvironmentEnabled    Kind = "SET_RUNTIME_ENVIRONMENT_ENABLED"
	DeleteRuntimeEnvironment        Kind = "DELETE_RUNTIME_ENVIRONMENT"
	BindAgentRuntimeEnvironment     Kind = "BIND_AGENT_RUNTIME_ENVIRONMENT"
	PromoteRoleImage                Kind = "PROMOTE_ROLE_IMAGE"
	ChangeAgentCapability           Kind = "CHANGE_AGENT_CAPABILITY"
	ChangeAgentGrant                Kind = "CHANGE_AGENT_GRANT"
	CreateWorkflow                  Kind = "CREATE_WORKFLOW"
	UpdateWorkflow                  Kind = "UPDATE_WORKFLOW_DRAFT"
	ValidateWorkflow                Kind = "VALIDATE_WORKFLOW_DRAFT"
	PublishWorkflow                 Kind = "PUBLISH_WORKFLOW_DRAFT"
	ArchiveWorkflow                 Kind = "ARCHIVE_WORKFLOW"
	LaunchRun                       Kind = "LAUNCH_RUN"
	AddSessionTurn                  Kind = "ADD_SESSION_TURN"
	CancelRun                       Kind = "CANCEL_RUN"
	RetryRun                        Kind = "RETRY_RUN"
	ResolveOwnerGate                Kind = "RESOLVE_OWNER_GATE"
	ChangeArtifactBinding           Kind = "CHANGE_ARTIFACT_BINDING"
	DeleteArtifact                  Kind = "DELETE_ARTIFACT"
	RestoreArtifact                 Kind = "RESTORE_ARTIFACT"
	PurgeArtifact                   Kind = "PURGE_ARTIFACT"
	CreateAttachmentSetDraft        Kind = "CREATE_ATTACHMENT_SET_DRAFT"
	AddAttachmentSetItems           Kind = "ADD_ATTACHMENT_SET_ITEMS"
	RemoveAttachmentSetItems        Kind = "REMOVE_ATTACHMENT_SET_ITEMS"
	FinalizeAttachmentSet           Kind = "FINALIZE_ATTACHMENT_SET"
	CreateSchedule                  Kind = "CREATE_SCHEDULE"
	UpdateSchedule                  Kind = "UPDATE_SCHEDULE"
	SetScheduleEnabled              Kind = "SET_SCHEDULE_ENABLED"
	ArchiveSchedule                 Kind = "ARCHIVE_SCHEDULE"
	DeleteSchedule                  Kind = "DELETE_SCHEDULE"
	CreateProviderAccount           Kind = "CREATE_PROVIDER_ACCOUNT"
	StartProviderDeviceAuth         Kind = "START_PROVIDER_DEVICE_AUTHORIZATION"
	AuthorizeProviderAPIKey         Kind = "AUTHORIZE_PROVIDER_API_KEY"
	RefreshProviderAuthorization    Kind = "REFRESH_PROVIDER_AUTHORIZATION"
	RevokeProviderAccount           Kind = "REVOKE_PROVIDER_ACCOUNT"
	DeleteProviderAccount           Kind = "DELETE_PROVIDER_ACCOUNT"
	SetProviderAccountEnabled       Kind = "SET_PROVIDER_ACCOUNT_ENABLED"
	CreateConnection                Kind = "CREATE_INTEGRATION_CONNECTION"
	UpdateConnection                Kind = "UPDATE_INTEGRATION_CONNECTION"
	DeleteConnection                Kind = "DELETE_INTEGRATION_CONNECTION"
	ConfigureConnectionCredential   Kind = "CONFIGURE_INTEGRATION_CONNECTION_CREDENTIAL"
	TestConnection                  Kind = "TEST_INTEGRATION_CONNECTION"
	SetConnectionEnabled            Kind = "SET_INTEGRATION_CONNECTION_ENABLED"
	ChangeIntegrationGrant          Kind = "CHANGE_INTEGRATION_GRANT"
	CreateAssistantConversation     Kind = "CREATE_ASSISTANT_CONVERSATION"
	UpdateAssistantConversation     Kind = "UPDATE_ASSISTANT_CONVERSATION_TITLE"
	AddAssistantTurn                Kind = "ADD_ASSISTANT_TURN"
	UpdateAssistantPlan             Kind = "UPDATE_ASSISTANT_PLAN_DRAFT"
	ValidateAssistantPlan           Kind = "VALIDATE_ASSISTANT_PLAN"
	ApplyAssistantPlan              Kind = "APPLY_ASSISTANT_PLAN"
	RejectAssistantPlan             Kind = "REJECT_ASSISTANT_PLAN"
	UpdateAssistantInstructions     Kind = "UPDATE_ASSISTANT_OWNER_INSTRUCTIONS"
	RecoverAssistant                Kind = "RECOVER_SYSTEM_ASSISTANT"
	ClaimExecution                  Kind = "CLAIM_EXECUTION"
	RenewExecution                  Kind = "RENEW_EXECUTION"
	ReportExecutionProgress         Kind = "REPORT_EXECUTION_PROGRESS"
	CompleteExecution               Kind = "COMPLETE_EXECUTION"
	DelegateExecution               Kind = "DELEGATE_EXECUTION"
	ProposeAssistantPlan            Kind = "PROPOSE_ASSISTANT_PLAN"
	ProposeAssistantMetadata        Kind = "PROPOSE_ASSISTANT_METADATA"
	ProposeRunMetadata              Kind = "PROPOSE_RUN_METADATA"
	RecordRunToolCall               Kind = "RECORD_RUN_TOOL_CALL"
	CompleteSessionSnapshot         Kind = "COMPLETE_SESSION_SNAPSHOT"
	CompleteSessionRestore          Kind = "COMPLETE_SESSION_RESTORE"
	CompleteSessionPVCDeletion      Kind = "COMPLETE_SESSION_PVC_DELETION"
	CompleteSessionObjectDeletion   Kind = "COMPLETE_SESSION_OBJECT_DELETION"
	FailSessionArchiveTask          Kind = "FAIL_SESSION_ARCHIVE_TASK"
	MaterializeOccurrence           Kind = "MATERIALIZE_SCHEDULE_OCCURRENCE"
	FailScheduleOccurrence          Kind = "FAIL_SCHEDULE_OCCURRENCE"
	CompleteConnectionTest          Kind = "COMPLETE_INTEGRATION_CONNECTION_TEST"
	CompleteIntegrationInvocation   Kind = "COMPLETE_INTEGRATION_INVOCATION"
	CompleteInteractionDelivery     Kind = "COMPLETE_INTERACTION_DELIVERY"
	AcceptInteractionMessage        Kind = "ACCEPT_INTERACTION_MESSAGE"
	CreateAccessRole                Kind = "CREATE_ACCESS_ROLE"
	CreateAccessRoleVersion         Kind = "CREATE_ACCESS_ROLE_VERSION"
	ArchiveAccessRole               Kind = "ARCHIVE_ACCESS_ROLE"
	CreateAccessBinding             Kind = "CREATE_ACCESS_BINDING"
	ChangeAccessBinding             Kind = "CHANGE_ACCESS_BINDING"
	RevokeAccessBinding             Kind = "REVOKE_ACCESS_BINDING"
	CreatePromptTemplateDraft       Kind = "CREATE_PROMPT_TEMPLATE_DRAFT"
	ValidatePromptTemplateDraft     Kind = "VALIDATE_PROMPT_TEMPLATE_DRAFT"
	PublishPromptTemplateDraft      Kind = "PUBLISH_PROMPT_TEMPLATE_DRAFT"
	RebindPromptTemplate            Kind = "REBIND_PROMPT_TEMPLATE_CONSUMERS"
	CreateRoleImageRevisionDraft    Kind = "CREATE_ROLE_IMAGE_REVISION_DRAFT"
	ValidateRoleImageRevision       Kind = "VALIDATE_ROLE_IMAGE_REVISION_DRAFT"
	PublishRoleImageRevision        Kind = "PUBLISH_ROLE_IMAGE_REVISION_DRAFT"
	RebindRoleImage                 Kind = "REBIND_ROLE_IMAGE_CONSUMERS"
	CreateIntegrationDefinition     Kind = "CREATE_INTEGRATION_DEFINITION_DRAFT"
	ValidateIntegrationDefinition   Kind = "VALIDATE_INTEGRATION_DEFINITION_DRAFT"
	PublishIntegrationDefinition    Kind = "PUBLISH_INTEGRATION_DEFINITION_DRAFT"
	RebindIntegrationDefinition     Kind = "REBIND_INTEGRATION_DEFINITION_CONSUMERS"
	CreateSystemSTTDraft            Kind = "CREATE_SYSTEM_STT_CONFIGURATION_DRAFT"
	ValidateSystemSTTDraft          Kind = "VALIDATE_SYSTEM_STT_CONFIGURATION_DRAFT"
	PublishSystemSTTDraft           Kind = "PUBLISH_SYSTEM_STT_CONFIGURATION_DRAFT"
	RebindSystemSTT                 Kind = "REBIND_SYSTEM_STT_CONSUMERS"
	DetachGitManagedConfiguration   Kind = "DETACH_GIT_MANAGED_CONFIGURATION"
	CopyGitManagedConfiguration     Kind = "COPY_GIT_MANAGED_CONFIGURATION"
)

const CommitProviderCredentialRefresh Kind = "COMMIT_PROVIDER_CREDENTIAL_REFRESH"

type Command struct {
	Kind      Kind
	Principal value.Principal
	Mutation  value.Mutation
	Payload   any
}

type ProjectInput struct{ Ref, Name, Purpose, Language string }
type PlatformMembershipInput struct {
	MembershipRef, UserRef, Role string
	Active                       bool
}
type MembershipInput struct {
	ProjectRef, MembershipRef, UserRef string
	Permissions                        []string
	Active                             bool
}
type AgentInput struct {
	Ref, ProjectRef, RoleDefinitionRef, Name, Purpose, RoleDescription, AvatarURL, RuntimeRef, Instructions string
	Enabled                                                                                                 bool
}
type AgentBindingInput struct {
	AgentRef, BindingRef string
	Enabled              bool
}
type AgentAvatarInput struct{ AgentRef, ArtifactRef string }
type AgentRuntimeConfigurationInput struct {
	AgentRef, RuntimeProfileRef, Model, ProviderPolicyMode string
	ProviderAccounts                                       []entity.ProviderAccountCandidate
}
type ConfigOverlayInput struct {
	AgentRef, Content, PublishedOverlayRef string
}
type RuntimeEnvironmentInput struct {
	Ref, ProjectRef, Name, Description, PublishedVersionRef, ImageArtifactRef string
	Values                                                                    []entity.RuntimeEnvironmentValue
	SecretBindings                                                            []entity.RuntimeSecretBinding
	Tools                                                                     []entity.RuntimeEnvironmentTool
	Policy                                                                    runtimecontract.RuntimeEnvironmentPolicy
}
type RuntimeEnvironmentDraftInput struct {
	DraftRef, ProjectRef, EnvironmentRef string
	ExpectedEnvironmentVersion           int64
	Specification                        entity.RuntimeEnvironmentDraftSpecification
}
type RuntimeSecretRebindInput struct {
	SecretRef  string
	Revision   int64
	Selections []entity.RuntimeSecretRebindSelection
}

type RuntimeEnvironmentBindingInput struct {
	AgentRef, EnvironmentRef, VersionRef string
}

type RuntimeEnvironmentRebindInput struct {
	EnvironmentRef, VersionRef string
	Consumers                  []entity.RuntimeEnvironmentConsumer
}
type RuntimeEnvironmentLifecycleInput struct {
	EnvironmentRef string
	Enabled        bool
}
type RoleImagePromotionInput struct {
	RecipeRef, ImageArtifactRef, ExpectedProvenanceSHA256 string
}
type WorkflowInput struct {
	Ref, ProjectRef, Name, Purpose, CoordinatorAgentRef string
	Draft                                               *entity.WorkflowVersion
}
type LaunchRunInput struct {
	ProjectRef, Title, TitleSource, Task, SessionRef, Source, AttachmentSetRef, AttachmentPurpose string
	Target                                                                                        entity.RunTarget
	Input                                                                                         map[string]any
}
type SessionTurnInput struct {
	SessionRef, RunRef, NodeRef, Task, AttachmentSetRef string
}
type RunCommandInput struct{ RunRef, Reason string }
type GateResolutionInput struct {
	GateRef, Decision, Comment, AttachmentSetRef string
}
type ArtifactBindingInput struct {
	ArtifactRef, AgentRef string
	Enabled               bool
}
type ArtifactLifecycleInput struct{ ArtifactRef, ImpactDigest string }
type AttachmentSetDraftInput struct {
	ProjectRef, Purpose, AttachmentSetRef string
	ArtifactRefs                          []string
	InsertAfterPosition                   int64
}
type ScheduleInput struct {
	Ref, ProjectRef, Name, Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone, SessionPolicy, NotificationPolicy string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy, AutomationText                                        string
	Target                                                                                                           entity.RunTarget
	TargetVersion                                                                                                    int64
	TargetDigest                                                                                                     string
	Input, PromptInputs                                                                                              map[string]any
	Enabled                                                                                                          bool
}
type ProviderAccountInput struct {
	AccountRef, DefinitionKey, Name, AuthorizationRef, AuthorizationMethod string
	AuthorizationState, MaterializerAttemptRef, VerificationURI, UserCode  string
	ExternalAccountMasked, SafeFailureCode                                 string
	AuthorizationExpiresAt                                                 *time.Time
	Credential                                                             *entity.ProviderCredentialDescriptor
	Enabled                                                                bool
}
type ConnectionInput struct {
	Ref, DefinitionKey, Name, MaterializationRef string
	PublicConfiguration                          map[string]any
	CredentialRevision                           *entity.IntegrationCredentialRevision
	Enabled                                      bool
}
type IntegrationGrantInput struct {
	ConnectionRef, CapabilityKey, AgentRef, WorkflowRef string
	Enabled                                             bool
}
type AssistantConversationInput struct {
	ProjectRef string
	Context    entity.AssistantContextDescriptor
}
type AssistantConversationTitleInput struct{ ConversationRef, Title string }
type AssistantTurnInput struct {
	ConversationRef, Content, AttachmentSetRef string
}
type AssistantPlanInput struct {
	PlanRef  string
	Revision int64
}
type AssistantPlanDraftInput struct {
	PlanRef, Summary string
	Operations       []entity.AssistantPlanOperation
}
type AssistantInstructionsInput struct{ Instructions string }
type LeaseInput struct {
	WorkloadInstance, LeaseRef, Fence string
	Generation                        int64
	Limit                             int32
	Progress                          string
}
type ProviderCredentialRefreshInput struct {
	LeaseRef, Fence, PreviousCredentialRevisionRef, PreviousContentSHA256 string
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256           string
	Generation                                                            int64
}
type CompleteExecutionInput struct {
	LeaseRef, Fence, ResultSummary, SafeErrorCode string
	CodexSessionID, ArchiveRelativePath           string
	ArchiveSHA256                                 string
	ArchiveSizeBytes                              int64
	Generation                                    int64
	Success                                       bool
	Usage                                         entity.TokenUsage
	Artifacts                                     []CompletedArtifact
}
type CompletedArtifact struct {
	FileName, MediaType, SHA256 string
	SizeBytes                   int64
	Content                     []byte
	Prepared                    *PreparedArtifact
}
type PreparedArtifact struct {
	Ref, ObjectKey, ObjectVersion, ObjectETag, MediaType, Digest, ScanState, PreviewState string
	SizeBytes                                                                             int64
}
type DelegateInput struct {
	LeaseRef, Fence, TargetAgentRef, WorkflowStepKey, Task string
	Generation                                             int64
	Input                                                  map[string]any
}
type ProposeAssistantPlanInput struct {
	LeaseRef, Fence, Summary string
	Generation               int64
	Operations               []entity.AssistantPlanOperation
}
type ProposeAssistantMetadataInput struct {
	LeaseRef, Fence, Title string
	Generation             int64
}
type ProposeRunMetadataInput struct {
	LeaseRef, Fence, Title, ActivitySummary string
	Generation                              int64
}
type RunToolCallInput struct {
	LeaseRef, Fence, CallRef, Tool, CapabilityRef, GrantRef, State, SafeResult string
	Generation, DurationMS                                                     int64
	SafeParameters                                                             map[string]any
}
type SessionArchiveTaskInput struct {
	TaskRef, LeaseRef, Fence, SafeErrorCode            string
	ObjectKey, ObjectVersion, ObjectETag, ObjectDigest string
	RestoredSourceSHA256, PVCName                      string
	Generation, ObjectSizeBytes, SourceSizeBytes       int64
	FormatVersion                                      uint32
}
type WarmRuntimeInput struct{ WorkloadInstance, RuntimeRevision, State, SafeErrorCode string }
type OccurrenceInput struct {
	OccurrenceRef, LeaseRef, Fence, SafeErrorCode string
	Generation                                    int64
	Retryable                                     bool
}
type IntegrationInvocationInput struct {
	InvocationRef, LeaseRef, Fence, ResultSummary, SafeErrorCode          string
	ReceiptRef, EffectKey, InputDigest, ProviderEffectRef, ResponseDigest string
	Generation                                                            int64
	Success                                                               bool
	UnknownOutcome                                                        bool
}
type IntegrationConnectionTestInput struct {
	TestRef, LeaseRef, Fence, ResultSummary, SafeErrorCode string
	Generation                                             int64
	Success                                                bool
}
type InteractionDeliveryInput struct {
	ExternalTeamRef, ExternalChannelRef                                             string
	DeliveryRef, LeaseRef, Fence, ExternalPostRef, ExternalThreadRef, SafeErrorCode string
	Generation                                                                      int64
	Success                                                                         bool
	UnknownOutcome, ConfirmedNoEffect                                               bool
}
type InteractionMessageInput struct {
	ConnectionRef, ExternalEventRef, ExternalPostRef, ExternalRootPostRef string
	ExternalChannelRef, ExternalUserDigest, Message, Decision             string
	ExternalTeamRef, GateRef, RunRef                                      string
	ExpectedGateVersion                                                   int64
}

type InteractionIdentityInput struct {
	IdentityRef, ConnectionRef, ExternalTeamRef, ExternalChannelRef, ExternalUserDigest, SubjectRef string
}

type ManagedConfigurationInput struct {
	ConfigurationRef, ProjectRef, Name, Kind, ContentFormat, Content, RevisionRef, ImpactDigest string
	Consumers                                                                                   []entity.ManagedConfigurationConsumer
}

type Result struct {
	InteractionIdentity     *entity.InteractionIdentity
	EnvironmentBindings     []entity.AgentRuntimeEnvironmentBinding
	RuntimeEnvironmentDraft *entity.RuntimeEnvironmentDraft
	Project                 *entity.Project
	Membership              *entity.Membership
	Agent                   *entity.Agent
	RuntimeConfiguration    *entity.AgentRuntimeConfigurationView
	RuntimeEnvironment      *entity.RuntimeEnvironmentSet
	RuntimeEnvironments     []entity.RuntimeEnvironmentSet
	Workflow                *entity.Workflow
	Run                     *entity.Run
	Graph                   *entity.RunGraph
	Gate                    *entity.OwnerGate
	Artifact                *entity.Artifact
	AttachmentSet           *entity.AttachmentSet
	Schedule                *entity.Schedule
	Connection              *entity.IntegrationConnection
	Conversation            *entity.AssistantConversation
	Plan                    *entity.AssistantPlan
	PlanReceipt             *entity.AssistantPlanReceipt
	Assistant               *entity.SystemAssistant
	Event                   *entity.RunEvent
	CreatedRefs             []string
	Duplicate               bool
	Runtime                 map[string]any
	RuntimeItems            []map[string]any
	AccessRole              *entity.AccessRole
	AccessBinding           *entity.AccessBinding
	ProviderAccount         *entity.ProviderAccount
	PromotionReceipt        *entity.RoleImagePromotionReceipt
	ManagedConfiguration    *entity.ManagedConfigurationSet
	ManagedRevision         *entity.ManagedConfigurationRevision
}
