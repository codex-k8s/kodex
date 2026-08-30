// Package entity содержит универсальную web-first предметную модель.
package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

type Project struct {
	Ref, Name, Purpose, Language, Lifecycle string
	Version                                 int64
	AgentCount, WorkflowCount               int32
	ActiveRunCount, PendingGateCount        int32
	CreatedAt, UpdatedAt                    time.Time
	NextActions                             []string
}

type SearchResult struct {
	Kind, Ref, ProjectRef, Title, Subtitle, State string
	UpdatedAt                                     time.Time
}

type User struct {
	Ref, DisplayName, EmailMasked string
	Active                        bool
}

type Membership struct {
	Ref, ProjectRef, Role string
	User                  User
	Permissions           []string
	NextActions           []string
	Active                bool
	Version               int64
}

type InstructionVersion struct {
	Ref, State, Content, Digest, ParentRef string
	VersionNumber                          int32
	Core                                   bool
	ValidationProblems                     []string
	CreatedAt                              time.Time
	PublishedAt                            *time.Time
}

type RuntimeSelection struct {
	Ref, Name, Provider, Model, RuntimeRevision string
	Ready                                       bool
}

type ProviderAccountCandidate struct {
	AccountRef string `json:"accountRef"`
	Weight     int32  `json:"weight"`
}

type ProviderAccountPolicyVersion struct {
	Ref, Mode, Digest string
	Version           int64
	AccountCandidates []ProviderAccountCandidate
	CreatedAt         time.Time
}

type AgentRuntimeConfiguration struct {
	Ref, AgentRef, RuntimeProfileRef, Provider, Model, Digest string
	Version                                                   int64
	ProviderPolicy                                            ProviderAccountPolicyVersion
	CreatedAt                                                 time.Time
}

type ConfigOverlayVersion struct {
	Ref, State, Content, Digest string
	Version, Revision           int64
	ValidationMessages          []string
	CreatedAt                   time.Time
	PublishedAt                 *time.Time
}

type RuntimeEnvironmentValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type RuntimeSecretDescriptor struct {
	Name                  string `json:"name"`
	SecretRef             string `json:"secret_ref"`
	Namespace             string `json:"namespace"`
	Revision              int64  `json:"revision"`
	SecretName            string `json:"secret_name"`
	SecretKey             string `json:"secret_key"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
}

type RuntimeSecretBinding struct {
	Name      string
	SecretRef string
}

type RuntimeEnvironmentTool struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
	UsageHint   string `json:"usage_hint,omitempty"`
}

type RuntimeEnvironmentImage struct {
	ArtifactRef      string `json:"artifact_ref"`
	RecipeRef        string `json:"recipe_ref"`
	Reference        string `json:"reference"`
	Digest           string `json:"digest"`
	RecipeGeneration int64  `json:"recipe_generation"`
}

type RuntimeEnvironmentVersion struct {
	Ref, Digest       string
	Version, Revision int64
	Image             RuntimeEnvironmentImage
	Tools             []RuntimeEnvironmentTool
	Values            []RuntimeEnvironmentValue
	SecretDescriptors []RuntimeSecretDescriptor
	Policy            runtimecontract.RuntimeEnvironmentPolicy
	CreatedAt         time.Time
}

type RuntimeEnvironmentSet struct {
	Ref, ProjectRef, Name, Description, State string
	Version                                   int64
	CurrentVersion                            RuntimeEnvironmentVersion
	UpdatedAt                                 time.Time
	Ready                                     bool
	ReadinessBlockers, NextActions            []string
}

type RuntimeEnvironmentReadiness struct {
	EnvironmentRef, PublishedVersionRef, PublishedVersionDigest string
	EnvironmentVersion                                          int64
	Ready                                                       bool
	Blockers                                                    []string
	ObservedAt                                                  time.Time
}

type AgentRuntimeEnvironmentBinding struct {
	Ref, AgentRef, EnvironmentRef, Digest string
	Version                               int64
}

type AgentRuntimeConfigurationView struct {
	Configuration       AgentRuntimeConfiguration
	PublishedOverlay    ConfigOverlayVersion
	DraftOverlay        *ConfigOverlayVersion
	EnvironmentBinding  AgentRuntimeEnvironmentBinding
	Environment         RuntimeEnvironmentSet
	SafeEffectiveConfig string
	AgentVersion        int64
}

type TemplateVariable struct {
	Name, Type, Description, Example, Source string
	Collection                               bool
	ItemType, RangeExample                   string
	ItemFields                               []TemplateVariableField
}

type TemplateVariableField struct{ Name, Type, Description string }

type ProviderDefinition struct {
	Key, Name, Description, DefaultModelID string
	AuthorizationMethods, ModelIDs         []string
	Available, Ready                       bool
	ReadinessBlockers                      []string
}

type ProviderAuthorization struct {
	Ref, Method, State, VerificationURI, UserCode, SafeFailureCode string
	ExpiresAt                                                      *time.Time
}

type ProviderAccount struct {
	Ref, DefinitionKey, Name, ExternalAccountMasked, State string
	Version                                                int64
	Enabled, Ready                                         bool
	Authorization                                          *ProviderAuthorization
	NextActions                                            []string
	CreatedAt, UpdatedAt                                   time.Time
}

type AgentAvatar struct {
	Source, ArtifactRef, ContentPath string
	ArtifactRevision                 int64
}

type Agent struct {
	Ref, ProjectRef, RoleDefinitionRef, RoleDefinitionName, SystemKey string
	Name, Purpose, RoleDescription, AvatarURL                         string
	State, RuntimeKey, RuntimeName, Provider, Model, RuntimeRevision  string
	Enabled, System                                                   bool
	Version                                                           int64
	Capabilities, IntegrationGrantRefs, KnowledgeArtifactRefs         []string
	DraftInstructions, PublishedInstructions                          *InstructionVersion
	PublishedInstructionVersions                                      []InstructionVersion
	CreatedAt, UpdatedAt                                              time.Time
	NextActions                                                       []string
	Avatar                                                            AgentAvatar
}

type WorkflowInputField struct {
	Key, Label, Type, Help, DefaultValue string
	Required                             bool
	Options                              []string
}

type WorkflowStep struct {
	Key, Name, AgentRef, Instructions, ExpectedResult string
	Position, ParallelGroup, TimeoutSeconds           int32
	Parallel, HumanGateAfter                          bool
	DependsOn, GateDecisions, RequiredCapabilityKeys  []string
}

type WorkflowVersion struct {
	Ref, Name, Purpose, CoordinatorAgentRef, Instructions, CompletionCriteria string
	VersionNumber                                                             int32
	Inputs                                                                    []WorkflowInputField
	Steps                                                                     []WorkflowStep
	AgentRefs                                                                 []string
	Concurrency                                                               int32
	TimeoutSeconds                                                            int64
	GateDecisions                                                             []string
	ResultSchema                                                              map[string]any
}

type Workflow struct {
	Ref, ProjectRef, Name, Purpose, CoordinatorAgentRef, State string
	Version                                                    int64
	Draft, Published                                           *WorkflowVersion
	CreatedAt, UpdatedAt                                       time.Time
	NextActions                                                []string
}

type RunTarget struct{ Type, Ref, Name string }

type TokenUsage struct {
	TotalTokens           int64 `json:"total_tokens"`
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	ModelContextWindow    int64 `json:"model_context_window"`
}

func (usage TokenUsage) Valid() bool {
	return usage.TotalTokens >= 0 && usage.InputTokens >= 0 && usage.CachedInputTokens >= 0 &&
		usage.CacheWriteInputTokens >= 0 && usage.OutputTokens >= 0 && usage.ReasoningOutputTokens >= 0 &&
		usage.ModelContextWindow >= 0 && usage.TotalTokens == usage.InputTokens+usage.OutputTokens &&
		usage.CachedInputTokens <= usage.InputTokens && usage.CacheWriteInputTokens <= usage.InputTokens &&
		usage.ReasoningOutputTokens <= usage.OutputTokens
}

type Run struct {
	Ref, ProjectRef, SessionRef, RootRunRef, ParentRunRef, RetryOfRunRef string
	InputAttachmentSetRef                                                string
	Title, Task, State, Source, ResultSummary, SafeErrorCode             string
	SafeErrorMessage, InitiatorName, TitleSource, ActivitySummary        string
	Target                                                               RunTarget
	Attempt                                                              int32
	GraphRevision, EventSequence, Version                                int64
	Input                                                                map[string]any
	Usage                                                                TokenUsage
	ArtifactRefs, GateRefs, NextActions                                  []string
	Incidents                                                            []Incident
	CreatedAt                                                            time.Time
	StartedAt, FinishedAt                                                *time.Time
}

type RunNode struct {
	Ref, RunRef, ParentNodeRef, Type, State, DisplayName, Role, AgentRef string
	TurnRef, InputSummary, ProgressSummary, CallbackSummary              string
	SafeErrorCode, SafeErrorMessage, MaterializationState                string
	Attempt                                                              int32
	IntegrationNames, ArtifactRefs, ChildRunRefs, NextActions            []string
	CreatedAt                                                            time.Time
	StartedAt, FinishedAt                                                *time.Time
}

type RunEdge struct{ Ref, RunRef, SourceNodeRef, TargetNodeRef, Type, Label string }

type RunDelta struct {
	Ref, State, ResultSummary, SafeErrorCode, SafeErrorMessage string
	Version, GraphRevision, EventSequence                      int64
	Usage                                                      TokenUsage
	ArtifactRefs, GateRefs, NextActions                        []string
	StartedAt, FinishedAt                                      *time.Time
}

type RunEventDelta struct {
	Run      *RunDelta
	Node     *RunNode
	Edge     *RunEdge
	Gate     *OwnerGate
	Artifact *Artifact
	Incident *Incident
}

type RunEventActor struct {
	Kind, Ref, Name string
}

type RunToolCall struct {
	Ref            string         `json:"ref"`
	Tool           string         `json:"tool"`
	SafeParameters map[string]any `json:"safeParameters"`
	CapabilityRef  string         `json:"capabilityRef,omitempty"`
	GrantRef       string         `json:"grantRef,omitempty"`
	State          string         `json:"state"`
	DurationMS     int64          `json:"durationMs"`
	SafeResult     string         `json:"safeResult"`
	AuditRef       string         `json:"auditRef"`
}

type RunEvent struct {
	Ref, RunRef, Type, NodeRef, EdgeRef, GateRef, ArtifactRef, IncidentRef string
	Summary, Progress, RunState, NodeState, MessageKind                    string
	Sequence, GraphRevision                                                int64
	OccurredAt                                                             time.Time
	Delta                                                                  RunEventDelta
	Actor                                                                  RunEventActor
	ToolCall                                                               *RunToolCall
}

type RunGraph struct {
	RunRef             string
	Revision, Sequence int64
	Nodes              []RunNode
	Edges              []RunEdge
}

type OwnerGate struct {
	Ref, ProjectRef, RunRef, NodeRef, Title, Prompt, ContextSummary string
	ResolutionAttachmentSetRef                                      string
	State, Decision, DecisionComment, RequestedByRef                string
	RequestedByName, ResolvedByName                                 string
	AllowedDecisions, NextActions                                   []string
	Version                                                         int64
	CreatedAt                                                       time.Time
	ResolvedAt                                                      *time.Time
	SourceAttachmentSetRef                                          string
	DecisionConsequences                                            []OwnerGateDecisionConsequence
	IntegrationIntent                                               *IntegrationIntent
}

type OwnerGateDecisionConsequence struct {
	Decision, SafeSummary                  string
	ExecutesExternalEffect, TerminalForRun bool
}

type IntegrationIntent struct {
	ConnectionRef, ConnectionName, DefinitionKey, CapabilityKey, Operation, EffectKey string
	ResourceKind, ResourceScopeDigest                                                 string
	ResourceScope                                                                     map[string]string
	EffectPreview                                                                     map[string]any
}

type Artifact struct {
	Ref, ProjectRef, RunRef, SessionRef, NodeRef, FileName, MediaType, Digest string
	ScanState, PreviewState, Source, LifecycleState                           string
	SizeBytes, Revision, Version                                              int64
	Bindings, NextActions                                                     []string
	CreatedAt                                                                 time.Time
	DeletedAt, PurgeAfter                                                     *time.Time
}

type ArtifactImpact struct {
	ArtifactRef, Action, Digest                       string
	ArtifactVersion                                   int64
	BindingCount, AttachmentCount, ActiveRuntimeCount int64
	Blockers                                          []string
	Permitted                                         bool
	ActiveRuns                                        []ArtifactImpactRun
	ActiveRunsTruncated                               bool
}

type ArtifactImpactRun struct {
	RunRef     string `json:"runRef"`
	Title      string `json:"title"`
	State      string `json:"state"`
	ProjectRef string `json:"projectRef"`
}

type AttachmentSetItem struct {
	ArtifactRef, DisplayName, MediaType, Digest, Source string
	ArtifactRevision, ArtifactVersion, Position         int64
	SizeBytes                                           int64
}

type AttachmentSet struct {
	Ref, FamilyRef, ProjectRef, State, Purpose, Source, ManifestDigest string
	Revision, Version, ItemCount, TotalSizeBytes                       int64
	Items                                                              []AttachmentSetItem
	CreatedAt                                                          time.Time
	FinalizedAt                                                        *time.Time
	Superseded                                                         bool
}

type Schedule struct {
	Ref, ProjectRef, Name, Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone string
	SessionPolicy, NotificationPolicy, State                                      string
	Target                                                                        RunTarget
	Input                                                                         map[string]any
	Enabled                                                                       bool
	Version                                                                       int64
	NextRunAt, LastRunAt                                                          *time.Time
	CreatedAt, UpdatedAt                                                          time.Time
	NextActions                                                                   []string
	CurrentRevision                                                               ScheduleRevision
	ContinueSessionRef                                                            string
}

type ScheduleRevision struct {
	Ref, Digest, Name, Preset, CronExpression, Timezone, SessionPolicy, NotificationPolicy string
	Revision                                                                               int64
	Target                                                                                 RunTarget
	Input                                                                                  map[string]any
	CreatedAt                                                                              time.Time
}

type ScheduleRunOccurrence struct {
	ScheduleRef, ScheduleRevisionRef string
	ScheduleRevision                 int64
	Run                              Run
}

type IntegrationCapability struct {
	Key, Name, Description, Operation, Risk, ApprovalPolicy, ResourceKind string
	InputFields                                                           []IntegrationConfigurationField
}

type IntegrationConfigurationField struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Help          string   `json:"help"`
	ValueType     string   `json:"valueType"`
	Placeholder   string   `json:"placeholder,omitempty"`
	Format        string   `json:"format,omitempty"`
	AllowedValues []string `json:"allowedValues,omitempty"`
	Minimum       *int64   `json:"minimum,omitempty"`
	Maximum       *int64   `json:"maximum,omitempty"`
	MaximumLength int32    `json:"maximumLength,omitempty"`
	Required      bool     `json:"required"`
}

type IntegrationDefinition struct {
	Key, Name, Description, Category, SchemaVersion, DefinitionVersion string
	Origin, Digest, Adapter, CredentialSecretKey                       string
	Optional, Enabled                                                  bool
	Capabilities                                                       []IntegrationCapability
	ConfigurationFields                                                []IntegrationConfigurationField
}

type IntegrationCredentialRevision struct {
	Ref, SecretRef, SecretUID, SecretResourceVersion, ContentSHA256 string
	Revision                                                        int64
	CreatedAt                                                       time.Time
}

type IntegrationGrant struct {
	Ref, CapabilityKey, TargetType, TargetRef, TargetName, ApprovalPolicy string
	Risk, ResourceKind, ResourceScopeDigest                               string
	ResourceScope                                                         map[string]string
	Enabled                                                               bool
	Version                                                               int64
}

type IntegrationConnection struct {
	Ref, DefinitionKey, DefinitionName, Name, State, MaskedCredentialsState string
	CredentialSecretKey                                                     string
	DefinitionVersion, DefinitionDigest                                     string
	LastTestSummary                                                         string
	Enabled                                                                 bool
	Version                                                                 int64
	PublicConfiguration                                                     map[string]any
	CredentialRevision                                                      *IntegrationCredentialRevision
	Capabilities                                                            []IntegrationCapability
	Grants                                                                  []IntegrationGrant
	LastTestedAt                                                            *time.Time
	CreatedAt, UpdatedAt                                                    time.Time
	NextActions                                                             []string
	LifecycleState                                                          string
}

type AssistantContextDescriptor struct {
	Route                             string
	EntityKind, EntityRef, EntityName string
	EntityVersion                     *int64
	AllowedOperations                 []string
}

type AssistantPlanTarget struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref,omitempty"`
	Name    string `json:"name"`
	Version *int64 `json:"version,omitempty"`
}

type AssistantPlanOperation struct {
	Key                string              `json:"ref"`
	Type               string              `json:"type"`
	Action             string              `json:"action"`
	Title              string              `json:"title"`
	Summary            string              `json:"summary"`
	Target             AssistantPlanTarget `json:"target"`
	Parameters         map[string]any      `json:"parameters"`
	Before             map[string]any      `json:"before"`
	After              map[string]any      `json:"after"`
	ExpectedVersion    *int64              `json:"expectedVersion,omitempty"`
	Selected           bool                `json:"selected"`
	Permitted          bool                `json:"permitted"`
	UnavailableReason  string              `json:"unavailableReason,omitempty"`
	ValidationProblems []string            `json:"validationProblems"`
	// Input и плоские target-поля оставлены только для внутреннего перехода
	// существующих специализированных command adapters на explicit projection.
	Input                 map[string]any `json:"-"`
	TargetKind, TargetRef string         `json:"-"`
}

type AssistantPlan struct {
	Ref, ConversationRef, ProjectRef, Summary, State, ContentDigest string
	Version, Revision                                               int64
	ValidatedRevision                                               *int64
	Operations                                                      []AssistantPlanOperation
	ValidationProblems                                              []string
	CreatedAt                                                       time.Time
	ValidatedAt, AppliedAt                                          *time.Time
}

type AssistantPlanOperationReceipt struct {
	OperationRef, ResourceRef, Outcome, AuditRef string
}

type AssistantPlanConflict struct {
	OperationRef, TargetRef, Field string
	Expected, Actual               any
}

type AssistantPlanReceipt struct {
	Ref, PlanRef, Outcome          string
	PlanRevision                   int64
	Operations                     []AssistantPlanOperationReceipt
	Conflicts                      []AssistantPlanConflict
	AuditRefs, CreatedResourceRefs []string
	CreatedAt                      time.Time
}

type AssistantTurn struct {
	Ref, Actor, ActorName, Content, State, AttachmentSetRef string
	CreatedAt                                               time.Time
	CompletedAt                                             *time.Time
}

type AssistantConversation struct {
	Ref, Title, ProjectRef, SessionRef, State string
	TitleSource                               string
	Version, TitleRevision                    int64
	Context                                   AssistantContextDescriptor
	Turns                                     []AssistantTurn
	LatestPlan                                *AssistantPlan
	CreatedAt, UpdatedAt                      time.Time
}

type SystemAssistant struct {
	Ref, StableKey, Name, Purpose, CorePromptRevision, OwnerInstructions  string
	RuntimeState, RuntimeRevision, DesiredRuntimeRevision, WarmSessionRef string
	Ready, System, Deletable                                              bool
	Version                                                               int64
	ResourceLimits                                                        map[string]any
	LastHeartbeatAt                                                       *time.Time
	UpdatedAt                                                             time.Time
	NextActions                                                           []string
}

type AuditEvent struct {
	Ref, ProjectRef, ActorRef, ActorName, Executor, Source                            string
	Action, ResourceKind, ResourceRef, ResourceName, Outcome, Summary, CorrelationRef string
	OccurredAt                                                                        time.Time
}

type Incident struct {
	Ref, ProjectRef, RunRef, Category, Severity, State string
	SafeSummary, SafeNextStep                          string
	CoreAffected                                       bool
	CreatedAt                                          time.Time
}
