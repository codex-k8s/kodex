// Package command содержит закрытый реестр специализированных команд.
package command

import (
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type Kind string

const (
	CompleteOnboarding            Kind = "COMPLETE_ONBOARDING"
	CreateProject                 Kind = "CREATE_PROJECT"
	UpdateProject                 Kind = "UPDATE_PROJECT"
	AddPlatformMembership         Kind = "ADD_PLATFORM_MEMBERSHIP"
	ChangePlatformMembership      Kind = "CHANGE_PLATFORM_MEMBERSHIP"
	RemovePlatformMembership      Kind = "REMOVE_PLATFORM_MEMBERSHIP"
	AddMembership                 Kind = "ADD_MEMBERSHIP"
	ChangeMembership              Kind = "CHANGE_MEMBERSHIP"
	RemoveMembership              Kind = "REMOVE_MEMBERSHIP"
	CreateAgent                   Kind = "CREATE_AGENT"
	UpdateAgent                   Kind = "UPDATE_AGENT"
	SetAgentEnabled               Kind = "SET_AGENT_ENABLED"
	ArchiveAgent                  Kind = "ARCHIVE_AGENT"
	CreateInstructions            Kind = "CREATE_INSTRUCTION_DRAFT"
	ValidateInstructions          Kind = "VALIDATE_INSTRUCTION_DRAFT"
	PublishInstructions           Kind = "PUBLISH_INSTRUCTION_DRAFT"
	RollbackInstructions          Kind = "ROLLBACK_INSTRUCTIONS"
	ChangeAgentCapability         Kind = "CHANGE_AGENT_CAPABILITY"
	ChangeAgentGrant              Kind = "CHANGE_AGENT_GRANT"
	CreateWorkflow                Kind = "CREATE_WORKFLOW"
	UpdateWorkflow                Kind = "UPDATE_WORKFLOW_DRAFT"
	ValidateWorkflow              Kind = "VALIDATE_WORKFLOW_DRAFT"
	PublishWorkflow               Kind = "PUBLISH_WORKFLOW_DRAFT"
	ArchiveWorkflow               Kind = "ARCHIVE_WORKFLOW"
	LaunchRun                     Kind = "LAUNCH_RUN"
	AddSessionTurn                Kind = "ADD_SESSION_TURN"
	CancelRun                     Kind = "CANCEL_RUN"
	RetryRun                      Kind = "RETRY_RUN"
	ResolveOwnerGate              Kind = "RESOLVE_OWNER_GATE"
	ChangeArtifactBinding         Kind = "CHANGE_ARTIFACT_BINDING"
	CreateSchedule                Kind = "CREATE_SCHEDULE"
	UpdateSchedule                Kind = "UPDATE_SCHEDULE"
	SetScheduleEnabled            Kind = "SET_SCHEDULE_ENABLED"
	CreateConnection              Kind = "CREATE_INTEGRATION_CONNECTION"
	TestConnection                Kind = "TEST_INTEGRATION_CONNECTION"
	SetConnectionEnabled          Kind = "SET_INTEGRATION_CONNECTION_ENABLED"
	ChangeIntegrationGrant        Kind = "CHANGE_INTEGRATION_GRANT"
	CreateAssistantConversation   Kind = "CREATE_ASSISTANT_CONVERSATION"
	AddAssistantTurn              Kind = "ADD_ASSISTANT_TURN"
	ApplyAssistantPlan            Kind = "APPLY_ASSISTANT_PLAN"
	UpdateAssistantInstructions   Kind = "UPDATE_ASSISTANT_OWNER_INSTRUCTIONS"
	RecoverAssistant              Kind = "RECOVER_SYSTEM_ASSISTANT"
	ClaimExecution                Kind = "CLAIM_EXECUTION"
	RenewExecution                Kind = "RENEW_EXECUTION"
	ReportExecutionProgress       Kind = "REPORT_EXECUTION_PROGRESS"
	CompleteExecution             Kind = "COMPLETE_EXECUTION"
	DelegateExecution             Kind = "DELEGATE_EXECUTION"
	ProposeAssistantPlan          Kind = "PROPOSE_ASSISTANT_PLAN"
	DeliverCallback               Kind = "DELIVER_CALLBACK"
	ReportWarmRuntime             Kind = "REPORT_WARM_RUNTIME"
	MaterializeOccurrence         Kind = "MATERIALIZE_SCHEDULE_OCCURRENCE"
	CompleteOccurrence            Kind = "COMPLETE_SCHEDULE_OCCURRENCE"
	CompleteConnectionTest        Kind = "COMPLETE_INTEGRATION_CONNECTION_TEST"
	CompleteIntegrationInvocation Kind = "COMPLETE_INTEGRATION_INVOCATION"
)

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
type WorkflowInput struct {
	Ref, ProjectRef, Name, Purpose, CoordinatorAgentRef string
	Draft                                               *entity.WorkflowVersion
}
type LaunchRunInput struct {
	ProjectRef, Title, Task, SessionRef, Source string
	Target                                      entity.RunTarget
	Input                                       map[string]any
	ArtifactRefs                                []string
}
type SessionTurnInput struct {
	SessionRef, RunRef, NodeRef, Task string
	ArtifactRefs                      []string
}
type RunCommandInput struct{ RunRef, Reason string }
type GateResolutionInput struct{ GateRef, Decision, Comment string }
type ArtifactBindingInput struct {
	ArtifactRef, AgentRef string
	Enabled               bool
}
type ScheduleInput struct {
	Ref, ProjectRef, Name, Preset, CronExpression, Timezone, SessionPolicy, NotificationPolicy string
	Target                                                                                     entity.RunTarget
	Input                                                                                      map[string]any
	Enabled                                                                                    bool
}
type ConnectionInput struct {
	Ref, DefinitionKey, Name string
	PublicConfiguration      map[string]any
	Enabled                  bool
}
type IntegrationGrantInput struct {
	ConnectionRef, CapabilityKey, AgentRef, WorkflowRef string
	Enabled                                             bool
}
type AssistantConversationInput struct{ Title, ProjectRef string }
type AssistantTurnInput struct {
	ConversationRef, Content string
	ArtifactRefs             []string
}
type AssistantPlanInput struct{ PlanRef string }
type AssistantInstructionsInput struct{ Instructions string }
type LeaseInput struct {
	WorkloadInstance, LeaseRef, Fence string
	Generation                        int64
	Limit                             int32
	Progress                          string
}
type CompleteExecutionInput struct {
	LeaseRef, Fence, ResultSummary, SafeErrorCode string
	Generation                                    int64
	Success                                       bool
	Artifacts                                     []CompletedArtifact
}
type CompletedArtifact struct {
	FileName, MediaType, SHA256 string
	SizeBytes                   int64
	Content                     []byte
}
type DelegateInput struct {
	LeaseRef, Fence, TargetAgentRef, Task string
	Generation                            int64
	Input                                 map[string]any
}
type ProposeAssistantPlanInput struct {
	LeaseRef, Fence, Summary string
	Generation               int64
	Operations               []entity.AssistantPlanOperation
}
type CallbackInput struct{ ChildRunRef, CallbackEdgeRef string }
type WarmRuntimeInput struct{ WorkloadInstance, RuntimeRevision, State, SafeErrorCode string }
type OccurrenceInput struct {
	OccurrenceRef, LeaseRef, Fence, Outcome string
	Generation                              int64
}
type IntegrationInvocationInput struct {
	InvocationRef, LeaseRef, Fence, ResultSummary, SafeErrorCode string
	Generation                                                   int64
	Success                                                      bool
}
type IntegrationConnectionTestInput struct {
	TestRef, LeaseRef, Fence, ResultSummary, SafeErrorCode string
	Generation                                             int64
	Success                                                bool
}

type Result struct {
	Project      *entity.Project
	Membership   *entity.Membership
	Agent        *entity.Agent
	Workflow     *entity.Workflow
	Run          *entity.Run
	Graph        *entity.RunGraph
	Gate         *entity.OwnerGate
	Artifact     *entity.Artifact
	Schedule     *entity.Schedule
	Connection   *entity.IntegrationConnection
	Conversation *entity.AssistantConversation
	Plan         *entity.AssistantPlan
	Assistant    *entity.SystemAssistant
	Event        *entity.RunEvent
	CreatedRefs  []string
	Duplicate    bool
	Runtime      map[string]any
	RuntimeItems []map[string]any
}
