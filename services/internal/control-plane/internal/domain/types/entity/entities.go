// Package entity содержит универсальную web-first предметную модель.
package entity

import "time"

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
	SecretName            string `json:"secret_name"`
	SecretKey             string `json:"secret_key"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
}

type RuntimeEnvironmentVersion struct {
	Ref, Digest       string
	Version, Revision int64
	Values            []RuntimeEnvironmentValue
	SecretDescriptors []RuntimeSecretDescriptor
	CreatedAt         time.Time
}

type RuntimeEnvironmentSet struct {
	Ref, ProjectRef, Name, Description, State string
	Version                                   int64
	CurrentVersion                            RuntimeEnvironmentVersion
	UpdatedAt                                 time.Time
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
	Title, Task, State, Source, ResultSummary, SafeErrorCode             string
	SafeErrorMessage, InitiatorName                                      string
	Target                                                               RunTarget
	Attempt                                                              int32
	GraphRevision, EventSequence, Version                                int64
	Input                                                                map[string]any
	Usage                                                                TokenUsage
	InputArtifactRefs, ArtifactRefs, GateRefs, NextActions               []string
	Incidents                                                            []Incident
	CreatedAt                                                            time.Time
	StartedAt, FinishedAt                                                *time.Time
}

type RunNode struct {
	Ref, RunRef, ParentNodeRef, Type, State, DisplayName, Role, AgentRef string
	TurnRef, InputSummary, ProgressSummary, CallbackSummary              string
	SafeErrorCode, SafeErrorMessage                                      string
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

type RunEvent struct {
	Ref, RunRef, Type, NodeRef, EdgeRef, GateRef, ArtifactRef, IncidentRef string
	Summary, Progress, RunState, NodeState                                 string
	Sequence, GraphRevision                                                int64
	OccurredAt                                                             time.Time
	Delta                                                                  RunEventDelta
}

type RunGraph struct {
	RunRef             string
	Revision, Sequence int64
	Nodes              []RunNode
	Edges              []RunEdge
}

type OwnerGate struct {
	Ref, ProjectRef, RunRef, NodeRef, Title, Prompt, ContextSummary string
	State, Decision, DecisionComment, RequestedByRef                string
	RequestedByName, ResolvedByName                                 string
	AllowedDecisions, ArtifactRefs, NextActions                     []string
	Version                                                         int64
	CreatedAt                                                       time.Time
	ResolvedAt                                                      *time.Time
}

type Artifact struct {
	Ref, ProjectRef, RunRef, SessionRef, NodeRef, FileName, MediaType, Digest string
	ScanState, PreviewState, Source                                           string
	SizeBytes, Revision, Version                                              int64
	Bindings, NextActions                                                     []string
	CreatedAt                                                                 time.Time
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
}

type IntegrationCapability struct {
	Key, Name, Description, Operation, Risk, ApprovalPolicy, ResourceKind string
	InputFields                                                           []IntegrationConfigurationField
}

type IntegrationConfigurationField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Help        string `json:"help"`
	ValueType   string `json:"valueType"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required"`
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
}

type AssistantPlanOperation struct {
	Key, Type, Summary, TargetKind, TargetRef string
	Input                                     map[string]any
}

type AssistantPlan struct {
	Ref, Summary, State string
	Version             int64
	Operations          []AssistantPlanOperation
	CreatedAt           time.Time
	AppliedAt           *time.Time
}

type AssistantTurn struct {
	Ref, Actor, ActorName, Content, State string
	ArtifactRefs                          []string
	CreatedAt                             time.Time
	CompletedAt                           *time.Time
}

type AssistantConversation struct {
	Ref, Title, ProjectRef, SessionRef, State string
	Version                                   int64
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
