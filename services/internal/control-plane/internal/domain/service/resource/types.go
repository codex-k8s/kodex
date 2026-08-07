package resource

import (
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
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

type CreateProjectInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Name           string
	Spec           entity.ProjectSpec
}

type UpdateInput struct {
	Principal           value.Principal
	IdempotencyKey      string
	ResourceID          string
	ExpectedVersion     uint64
	Name                string
	Spec                entity.Spec
	Administrative      bool
	DetachGitManagement bool
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

type UpdateProjectInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ProjectID       string
	ExpectedVersion uint64
	Name            string
	Spec            entity.ProjectSpec
}

type DeleteProjectInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ProjectID       string
	ExpectedVersion uint64
}

type GetInput struct {
	Principal       value.Principal
	ResourceID      string
	Kind            enum.Kind
	ExpectedVersion uint64
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

type DetachAccessResourceInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ResourceID      string
	ExpectedVersion uint64
	ExpectedKind    enum.Kind
}

type CopyAccessResourceInput struct {
	Principal             value.Principal
	IdempotencyKey        string
	SourceResourceID      string
	ExpectedSourceVersion uint64
	ExpectedKind          enum.Kind
	Name                  string
}

type ManageProtectedConfigurationInput struct {
	Principal            value.Principal
	FullMethod           string
	IdempotencyKey       string
	Kind                 enum.Kind
	Action               string
	ResourceID           string
	ExpectedVersion      uint64
	Name                 string
	Spec                 entity.Spec
	TargetVersion        uint64
	TargetSHA256         string
	ReferenceKeys        []string
	ProviderReceipt      value.ProviderEffectReceipt
	GitReceipt           value.GitReconciliationReceipt
	SemanticIntentSHA256 string
	InstructionObject    domainobjectstore.Object
}

type ProtectedResourceHistoryInput struct {
	Principal     value.Principal
	ResourceID    string
	Kind          enum.Kind
	BeforeVersion uint64
	Limit         int
}

type CompareInstructionVersionsInput struct {
	Principal        value.Principal
	InstructionSetID string
	LeftVersion      uint64
	RightVersion     uint64
}

type CompareInstructionVersionsResult struct {
	Left             domainrepo.ProtectedResourceHistory
	Right            domainrepo.ProtectedResourceHistory
	ContentEqual     bool
	ComparisonSHA256 string
}

type BindScheduleConfigurationInput struct {
	Principal               value.Principal
	IdempotencyKey          string
	ScheduleID              string
	ExpectedVersion         uint64
	AgentStableKey          string
	InstructionSetStableKey string
	ProviderPoolStableKey   string
}

type CreateScheduleFromOwnerSelectionsInput struct {
	Principal               value.Principal
	IdempotencyKey          string
	Name                    string
	AgentStableKey          string
	InstructionSetStableKey string
	ProviderPoolStableKey   string
	RoomStableKey           string
	PromptArtifactName      string
	Spec                    entity.ScheduleSpec
}

type ResolveLegacyConfigurationCutoverInput struct {
	Principal                   value.Principal
	IdempotencyKey              string
	LegacyRoleID                string
	ExpectedLegacyRoleVersion   uint64
	ExpectedLegacyPromptVersion uint64
	InstructionContent          string
}

type ResolveLegacyConfigurationCutoverResult struct {
	Cutover domainrepo.LegacyConfigurationCutover
	Agent   entity.Resource
}

type ManageRunInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ProcessRunID    string
	ExpectedVersion uint64
	Action          string
	ReasonCode      string
}

type ManageRunResult struct {
	ProcessRun    entity.Resource
	SuccessorTurn entity.Resource
}

type RunDetailResult struct {
	ProcessRun      entity.Resource
	Session         entity.Resource
	Turn            entity.Resource
	RuntimeRevision entity.Resource
	Runtime         *domainrepo.RuntimeExecution
	Incidents       []domainrepo.RuntimeIncident
}

type RunLineageResult struct {
	RootProcessRunID                              string
	RootSessionID, RootTurnID, ParentProcessRunID string
	CurrentSessionID                              string
	CurrentSessionVersion                         uint64
	CurrentTurnID                                 string
	CurrentTurnVersion                            uint64
	CurrentAttempt                                uint32
	RuntimeRevisionID                             string
	RuntimeRevisionVersion                        uint64
	ImmutableInputSHA256                          string
	Processes                                     []domainrepo.RunGraphNode
	Attempts                                      []domainrepo.RunGraphNode
	Complete                                      bool
}

type ManageWorkspaceMappingInput struct {
	Principal          value.Principal
	IdempotencyKey     string
	Action             string
	MappingID          string
	ExpectedVersion    uint64
	ExpectedGeneration uint64
	ProviderReceipt    value.ProviderEffectReceipt
	Name               string
}

type ManageWorkspaceBackupInput struct {
	Principal          value.Principal
	IdempotencyKey     string
	Action             string
	BackupID           string
	ExpectedVersion    uint64
	Scope              string
	Name               string
	TerminalReasonCode string
	RetainUntil        time.Time
}

type ManageWorkspaceRestoreInput struct {
	Principal             value.Principal
	IdempotencyKey        string
	Action                string
	RestoreID             string
	ExpectedVersion       uint64
	BackupID              string
	ExpectedBackupVersion uint64
	MembershipSHA256      string
	Name                  string
	TerminalReasonCode    string
}

type ReconcileWorkspaceRecoveryInput struct {
	Principal          value.Principal
	IdempotencyKey     string
	ResourceID         string
	ExpectedVersion    uint64
	ExpectedAttempt    uint32
	ExpectedGeneration uint64
	Outcome            string
	TerminalReasonCode string
}

type ManageRuntimeIncidentInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	IncidentID      string
	ExpectedVersion uint64
	Action          string
	ReasonCode      string
}

type ManageRuntimeIncidentResult struct {
	Incident          domainrepo.RuntimeIncident
	SuccessorTurn     entity.Resource
	ReleasedExecution *RuntimeExecution
}

type GetRuntimeIncidentInput struct {
	Principal  value.Principal
	IncidentID string
}

type ListRuntimeIncidentHistoryInput struct {
	Principal     value.Principal
	IncidentID    string
	BeforeVersion uint64
	Limit         int
}

type ListAuditInput struct {
	Principal value.Principal
	Filter    query.AuditFilter
}

type ListRuntimeIncidentsInput struct {
	Principal value.Principal
	Filter    query.RuntimeIncidentFilter
}

type OwnerSessionInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	ExpectedRevision uint64
}

type PrepareGatewayPublicTLSInput struct {
	Principal                    value.Principal
	IdempotencyKey               string
	Generation                   uint64
	CertificateSHA256            string
	PredecessorGeneration        uint64
	PredecessorCertificateSHA256 string
	NotBefore                    time.Time
	NotAfter                     time.Time
}

type ConfirmGatewayPublicTLSInput struct {
	Principal         value.Principal
	IdempotencyKey    string
	Generation        uint64
	CertificateSHA256 string
}

type CheckGatewayPublicTLSInput struct {
	Principal         value.Principal
	Generation        uint64
	CertificateSHA256 string
}

type ListTombstonesInput struct {
	Principal value.Principal
	Filter    query.TombstoneFilter
}

type DiagnosticsInput struct {
	Principal value.Principal
}

type ListOutboxFailuresInput struct {
	Principal    value.Principal
	AfterEventID string
	Limit        int
}

type RepairOutboxEventInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	EventID          string
	ExpectedSequence uint64
	ExpectedAttempts uint32
	ReasonCode       string
	EvidenceSHA256   string
}

type EnqueueTurnInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	SessionID        string
	SourceRef        string
	PromptArtifactID string
	ProcessRunID     string
	InputArtifactIDs []string
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

type ReportRuntimeProgressInput struct {
	Principal                                                       value.Principal
	IdempotencyKey, TurnID, LeaseToken, ExecutionID, Kind, Markdown string
	ExpectedTurnVersion, ExpectedExecutionVersion, ExpectedFence    uint64
	Attempt                                                         uint32
	AuthorityGeneration                                             uint64
	Sequence                                                        uint32
}

type ReportRuntimeProgressResult struct {
	DeliveryID string
	Turn       entity.Resource
	Execution  RuntimeExecution
}

type RuntimeMaterializationResult struct {
	OrganizationID, ProjectID                string
	ExecutionVersion, Fence, GrantGeneration uint64
	Materialization                          domainrepo.RuntimeMaterialization
}

type ResourceRetentionPolicyInput struct {
	Principal               value.Principal
	IdempotencyKey          string
	ExpectedVersion         uint64
	PVCRetentionSeconds     uint64
	ArchiveRetentionSeconds uint64
	ReasonCode              string
}

type RuntimeRetentionHoldInput struct {
	Principal              value.Principal
	IdempotencyKey         string
	SessionID              string
	ExpectedSessionVersion uint64
	HoldID                 string
	ExpectedHoldVersion    uint64
	Kind                   string
	ReasonCode             string
}

type RenewTurnInput struct {
	Principal           value.Principal
	IdempotencyKey      string
	TurnID              string
	LeaseToken          string
	ExpectedVersion     uint64
	Attempt             uint32
	AuthorityGeneration uint64
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

type ManageSessionInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	Action          string
	SessionID       string
	ExpectedVersion uint64
	Name            string
	AgentStableKey  string
	ConversationID  string
	ArchiveRef      string
	ReasonCode      string
}

type BindSessionMCPInput struct {
	Principal                 value.Principal
	IdempotencyKey            string
	SessionID                 string
	AgentSessionKey           string
	AgentSessionID            int64
	AgentSessionVersion       uint64
	AgentSessionBindingSHA256 string
	ImmutableSecretRef        string
	ProviderContentVersion    string
	ContentSHA256             string
}

type ManageConversationLifecycleInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Kind           string
	Action         string
	ResourceID     string
}

type ManageMemoryRecordInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	Action          string
	MemoryRecordID  string
	ExpectedVersion uint64
	Scope           string
	RoleID          string
	Title           string
	Content         string
	ContentSHA256   string
	Provenance      string
	Importance      uint32
}

type ManageWorkClaimInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	Action          string
	WorkClaimID     string
	ExpectedVersion uint64
	ProcessRunID    string
	TurnID          string
	Summary         string
	Domains         []string
	ResourceKeys    []string
	TTL             time.Duration
}

type ClaimDueSchedulesInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Limit          int
}

type ClaimDueSchedulesResult struct {
	Occurrences []ScheduleOccurrence
}

type ScheduleOccurrence struct {
	ScheduleID           string        `json:"scheduleId"`
	ScheduledFor         time.Time     `json:"scheduledFor"`
	OccurrenceID         string        `json:"occurrenceId"`
	TargetResourceID     string        `json:"targetResourceId"`
	TargetKind           enum.Kind     `json:"targetKind"`
	TargetVersion        uint64        `json:"targetVersion"`
	EffectiveInputSHA256 string        `json:"effectiveInputSha256"`
	PromptProfileID      string        `json:"promptProfileId"`
	PromptRevision       uint64        `json:"promptRevision"`
	RuntimeRevisionID    string        `json:"runtimeRevisionId"`
	SessionPolicy        string        `json:"sessionPolicy"`
	RoomID               string        `json:"roomId,omitempty"`
	NotificationPolicy   string        `json:"notificationPolicy"`
	MaximumExecution     time.Duration `json:"maximumExecutionDuration"`
	Coalesce             bool          `json:"coalesce"`
	State                string        `json:"state"`
	Attempt              uint32        `json:"attempt"`
	AvailableAt          time.Time     `json:"availableAt"`
	Outcome              string        `json:"outcome,omitempty"`
}

type ManageScheduleInput struct {
	Principal           value.Principal
	IdempotencyKey      string
	Action              string
	ScheduleID          string
	ExpectedVersion     uint64
	Name                string
	Spec                entity.ScheduleSpec
	DetachGitManagement bool
}

type RunScheduleNowInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ScheduleID      string
	ExpectedVersion uint64
}

type ClaimScheduleOccurrenceInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ScheduleOccurrenceClaimDisposition string

const (
	ScheduleOccurrenceClaimReserved     ScheduleOccurrenceClaimDisposition = "RESERVED"
	ScheduleOccurrenceClaimMaterialized ScheduleOccurrenceClaimDisposition = "MATERIALIZED"
	ScheduleOccurrenceClaimRetired      ScheduleOccurrenceClaimDisposition = "RETIRED"
)

type ScheduleOccurrenceResult struct {
	Occurrence                    domainrepo.ScheduleOccurrence
	MaterializationCapability     string
	MaterializationIdempotencyKey string
	CapabilityExpiresAt           time.Time
	ProjectID                     string
	Disposition                   ScheduleOccurrenceClaimDisposition
}

type MaterializeScheduleOccurrenceInput struct {
	Principal                 value.Principal
	IdempotencyKey            string
	OccurrenceID              string
	ProjectID                 string
	ExpectedAttempt           uint32
	MaterializationCapability string
}

type MaterializeScheduleOccurrenceResult struct {
	Occurrence           domainrepo.ScheduleOccurrence
	CompletionCapability string
}

type CompleteScheduleOccurrenceInput struct {
	Principal            value.Principal
	IdempotencyKey       string
	OccurrenceID         string
	CompletionCapability string
	ExpectedAttempt      uint32
	TerminalState        string
	Outcome              string
	ResultArtifactID     string
	ProjectID            string
}

type ResolveScheduleRecoveryInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ScheduleID      string
	OccurrenceID    string
	ExpectedVersion uint64
	ExpectedAttempt uint32
	Action          string
	EvidenceSHA256  string
	ReasonCode      string
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
	Principal        value.Principal
	IdempotencyKey   string
	Name             string
	ParentProcessID  string
	PlaybookRef      string
	PolicyRevision   uint64
	RootTriggerRef   string
	RootSessionID    string
	RootTurnID       string
	RootAttempt      uint32
	InputArtifactID  string
	LaunchingTurnID  string
	LaunchingAttempt uint32
}

type CancelProcessInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ProcessRunID    string
	ExpectedVersion uint64
	ReasonCode      string
}

type CompleteProcessInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	ProcessRunID     string
	ExpectedVersion  uint64
	TerminalState    enum.State
	Outcome          string
	ResultArtifactID string
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
	Principal       value.Principal
	IdempotencyKey  string
	OwnerGateID     string
	ExpectedVersion uint64
	Decision        string
	Reason          string
}

type RecordOwnerGateDeliveryInput struct {
	Principal             value.Principal
	IdempotencyKey        string
	OwnerGateID           string
	ExpectedVersion       uint64
	DeliveryID            string
	DeliveryPayloadSHA256 string
	DeliveryClaimToken    string
	DeliveryFence         uint64
	MattermostPostID      string
	MattermostChannelID   string
	MattermostRootPostID  string
	ProviderReceiptSHA256 string
}

type ClaimOwnerGateDeliveryInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ClaimOwnerGateDeliveryResult struct {
	OwnerGate  entity.Resource
	ClaimToken string
	ExpiresAt  time.Time
}

type ClaimInteractionDeliveryInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ClaimInteractionDeliveryResult struct {
	Work               domainrepo.InteractionDeliveryWork
	LeaseToken         string
	ReadbackCredential string
}

type IssueInteractionDeliveryReadbackInput struct {
	Principal      value.Principal
	IdempotencyKey string
	DeliveryID     string
	Readiness      bool
}

type IssueInteractionDeliveryReadbackResult struct {
	DeliveryID string
	Credential string
	ExpiresAt  time.Time
}

type ValidateInteractionDeliveryReadbackInput struct {
	Principal        value.Principal
	IdempotencyKey   string
	GrantID          string
	DeliveryID       string
	OrganizationID   string
	ProjectID        string
	CredentialSHA256 string
	Generation       uint64
}

type RecordInteractionDeliveryInput struct {
	Principal             value.Principal
	IdempotencyKey        string
	DeliveryID            string
	Fence                 uint64
	LeaseToken            string
	ProviderReceiptSHA256 string
}

type ExpireOwnerGateInput struct {
	Principal      value.Principal
	IdempotencyKey string
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

type GetRuntimeRevisionInput struct {
	Principal         value.Principal
	RuntimeRevisionID string
	ExpectedVersion   uint64
}

type RecordMemoryEmbeddingInput struct {
	Principal               value.Principal
	IdempotencyKey          string
	MemoryRecordID          string
	ExpectedResourceVersion uint64
	ContentSHA256           string
}

type RecordMemoryEmbeddingResult struct {
	MemoryRecordID   string
	ResourceVersion  uint64
	ProjectionSHA256 string
}

type SearchMemoryInput struct {
	Principal           value.Principal
	Query               string
	Scope               string
	RoleID              string
	AfterID             string
	AfterTextRank       float32
	AfterVectorDistance float32
	AfterVectorUsed     bool
	Limit               int
}

type MemorySearchHit struct {
	Resource             entity.Resource
	TextRank             float32
	VectorDistance       float32
	VectorProjectionUsed bool
}

type (
	RuntimeExecution        = domainrepo.RuntimeExecution
	IntegrationContinuation = domainrepo.IntegrationContinuation
)

type RuntimeExecutionInput struct {
	Principal               value.Principal
	IdempotencyKey          string
	ExecutionID             string
	ExpectedVersion         uint64
	ExpectedFence           uint64
	ExpectedGrantGeneration uint64
	LeaseToken              string
}

type RecordRuntimeIncidentInput struct {
	RuntimeExecutionInput
	Kind           string
	IncidentID     string
	EvidenceSHA256 string
}

type CompleteRuntimeExecutionInput struct {
	RuntimeExecutionInput
	Outcome, TerminalReference, TerminalSHA256                            string
	ScheduledOutcome                                                      string
	Outputs                                                               []RuntimeOutput
	CodexSessionID, ArchiveRelativePath, ArchiveSHA256, ArchiveProvenance string
}

type RuntimeOutput struct {
	Kind, ArtifactID, ArtifactSHA256, ArtifactName, ArtifactMediaType string
	ArtifactVersion                                                   uint64
	ArtifactPayload                                                   []byte
	ArtifactStorageRef                                                string
	ArtifactSizeBytes                                                 uint64
	Sequence, Total                                                   uint32
}

type RuntimeOutputMetadata struct {
	Kind, Name, MediaType, SHA256 string
	SizeBytes                     uint64
	Sequence, Total               uint32
}

type RuntimeOutputAuthorization struct {
	OrganizationID, ProjectID string
	ExecutionVersion, Fence   uint64
	GrantGeneration           uint64
}

type RegisterRuntimeOutputInput struct {
	Principal                value.Principal
	IdempotencyKey           string
	ExecutionID              string
	ExpectedExecutionVersion uint64
	ExpectedExecutionFence   uint64
	ExpectedGrantGeneration  uint64
	Output                   RuntimeOutputMetadata
	StorageRef               string
}

type CancelRuntimeExecutionInput struct {
	RuntimeExecutionInput
	ReasonCode string
}

type RuntimeArchiveInput struct {
	RuntimeExecutionInput
	ArchiveReference        string
	ArchiveSHA256           string
	ArchiveObjectKey        string
	ArchiveVersionID        string
	ArchiveKMSKeyARN        string
	ArchiveObjectLockMode   string
	ArchiveRetainUntil      time.Time
	ArchiveProvenanceSHA256 string
}

type RuntimeRestoreInput struct {
	RuntimeExecutionInput
	ArchiveSHA256         string
	RestoreProofReference string
	RestoreProofSHA256    string
}

type RuntimeRestoreTargetInput struct {
	RuntimeExecutionInput
	ExpectedAssignmentGeneration uint64
	PVCName                      string
	PVCUID                       string
	PVCResourceVersion           string
}

type RuntimeRestoreEffectInput struct {
	RuntimeExecutionInput
	RestoreOperationID           string
	RestoreOperationGeneration   uint64
	RestoreSourceAuthoritySHA256 string
	Effect                       string
}

type RuntimeRehydrateInput struct {
	RuntimeExecutionInput
	AssignmentGeneration uint64
	PVCName              string
	PVCUID               string
	PVCResourceVersion   string
	ProofReference       string
	ProofSHA256          string
}

type RuntimeCleanupInput struct {
	RuntimeExecutionInput
	ArchiveSHA256             string
	RestoreProofSHA256        string
	ExpectedCleanupGeneration uint64
	PVCName                   string
	PVCUID                    string
	PVCResourceVersion        string
}

type RuntimeCleanupAuthorizationInput struct {
	RuntimeExecutionInput
	CleanupAuthorizationID         string
	CleanupAuthorizationGeneration uint64
	ArchiveSHA256                  string
	RestoreProofSHA256             string
	PVCName                        string
	PVCUID                         string
	PVCResourceVersion             string
	ObservedNotFoundAt             time.Time
	DeletionProofSHA256            string
}

type PinnedIntegrationResource = domainrepo.PinnedIntegrationResource

type AdmitRuntimeExecutionResult struct {
	Execution  RuntimeExecution
	LeaseToken string
}

type RetryRuntimeExecutionResult struct {
	Previous RuntimeExecution
	Turn     entity.Resource
	Restore  *domainrepo.RuntimeRestoreOperation
}

type ListBackupsInput struct {
	Principal value.Principal
	AfterID   string
	Limit     int
}

type GetBackupInput struct {
	Principal value.Principal
	BackupID  string
}

type RestoreBackupInput struct {
	Principal             value.Principal
	IdempotencyKey        string
	BackupID              string
	ExpectedBackupVersion uint64
	ExpectedSourceVersion uint64
	ArchiveSHA256         string
	ProvenanceSHA256      string
	Scope                 string
	ScopeID               string
}

type GetRestoreOperationInput struct {
	Principal   value.Principal
	OperationID string
}

type ListRestoreOperationsInput struct {
	Principal value.Principal
	BackupID  string
	AfterID   string
	Limit     int
}

type ManageRuntimeActionInput struct {
	Principal      value.Principal
	IdempotencyKey string
	SessionID      string
	TurnID         string
	Action         string
}

type ManageRuntimeActionResult struct {
	Turn      entity.Resource
	Execution *RuntimeExecution
}

type IntegrationExecutionBinding struct {
	OrganizationID         string
	ProjectID              string
	ProcessID              string
	SessionID              string
	SessionVersion         uint64
	ThreadID               string
	RoleID                 string
	TurnID                 string
	TurnVersion            uint64
	Attempt                uint32
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	RuntimeRevisionSHA256  string
	ImmutableInputSHA256   string
	GrantGeneration        uint64
	Fence                  uint64
	Integration            PinnedIntegrationResource
	CredentialBindings     []PinnedIntegrationResource
}

type IntegrationSessionContext struct {
	OrganizationID         string
	ProjectID              string
	OwnerActorID           string
	ProcessID              string
	SessionID              string
	SessionVersion         uint64
	ThreadID               string
	TurnID                 string
	TurnVersion            uint64
	Attempt                uint32
	InputSHA256            string
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	RuntimeRevisionSHA256  string
	RuntimeManifestSHA256  string
	RoleID                 string
	RoleVersion            uint64
	RoleCapabilities       []string
	GrantGeneration        uint64
	Integrations           []IntegrationSessionBinding
}

type IntegrationSessionBinding struct {
	IntegrationID      string
	IntegrationVersion uint64
	ProjectionSHA256   string
	DefinitionRef      string
	DefinitionVersion  uint64
	Capabilities       []string
	EndpointRef        string
	CredentialBindings []IntegrationCredentialBinding
}

type IntegrationCredentialBinding struct {
	CredentialBindingID      string
	CredentialBindingVersion uint64
	ProjectionSHA256         string
	Purpose                  string
	SecretRef                string
	PrincipalRef             string
	CredentialRevision       uint64
	ExpiresAt                time.Time
}

type SuspendIntegrationInput struct {
	Principal          value.Principal
	IdempotencyKey     string
	InvocationID       string
	ApprovalID         string
	IntegrationID      string
	IntegrationVersion uint64
	IntegrationSHA256  string
	CredentialBindings []PinnedIntegrationResource
	RequestSHA256      string
	ApprovalExpiresAt  time.Time
}

type IntegrationDecisionInput struct {
	Principal         value.Principal
	IdempotencyKey    string
	ContinuationID    string
	ExpectedVersion   uint64
	ExpectedFence     uint64
	InvocationID      string
	ApprovalID        string
	RequestSHA256     string
	DecisionReference string
	DecisionSHA256    string
}

type IntegrationExecutionInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ContinuationID  string
	ExpectedVersion uint64
	ExpectedFence   uint64
	InvocationID    string
	RequestSHA256   string
	ResultReference string
	ResultSHA256    string
	ErrorCode       string
	ErrorReference  string
	ErrorSHA256     string
}

type GetIntegrationContinuationInput struct {
	Principal value.Principal
}

type AcknowledgeIntegrationContinuationInput struct {
	Principal           value.Principal
	IdempotencyKey      string
	ExpectedVersion     uint64
	ExpectedFence       uint64
	ExpectedInputSHA256 string
}

// Observer получает только закрытые вид и действие после устойчивой фиксации.
type Observer interface {
	ObserveMutation(kind enum.Kind, action string)
	ObserveScheduleMaintenance(effect string)
}
