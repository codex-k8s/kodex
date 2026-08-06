// Package controlplane задаёт доменный порт хранения полного control-plane.
package controlplane

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
)

// Scope — проверенная доменная идентичность для одной транзакции PostgreSQL.
type Scope struct {
	OrganizationID string
	ProjectID      string
	ActorID        string
}

// Receipt хранит устойчивый результат одной семантической команды.
type Receipt struct {
	OrganizationID string
	ProjectID      string
	Scope          string
	KeyHash        string
	RequestHash    string
	Result         entity.Resource
	Payload        []byte
	CreatedAt      time.Time
}

// Audit хранит безопасную атрибуцию изменения состояния.
type Audit struct {
	ID              string
	OrganizationID  string
	ProjectID       string
	ActorID         string
	Action          string
	ResourceID      string
	ResourceKind    string
	ResourceVersion uint64
	Outcome         string
	CorrelationID   string
	PolicyRevision  uint64
	OccurredAt      time.Time
}

// ProtectedResourceHistory хранит immutable snapshot специализированной команды.
type ProtectedResourceHistory struct {
	Resource       entity.Resource
	Action         string
	SnapshotSHA256 string
	OccurredAt     time.Time
}

// TurnLease защищает одну попытку выполнения хода.
type TurnLease struct {
	TurnID              string
	TokenHash           string
	WorkloadID          string
	AuthorityGeneration uint64
	Attempt             uint32
	ExpiresAt           time.Time
	Fence               uint64
}

// ExpiredTurn связывает unlocked candidate агрегата с истёкшей арендой.
// Команда обязана повторно получить их через канонический owner-graph lock path.
type ExpiredTurn struct {
	Turn  entity.Resource
	Lease TurnLease
}

// SessionTurn связывает незавершённый ход с необязательной действующей арендой.
type SessionTurn struct {
	Turn    entity.Resource
	Lease   TurnLease
	Attempt TurnAttempt
}

// DelegationEdge — неизменяемая серверная связь исходного и целевого хода.
type DelegationEdge struct {
	ID                   string
	OrganizationID       string
	ProjectID            string
	ParentProcessRunID   string
	SourceSessionID      string
	SourceTurnID         string
	SourceAttempt        uint32
	SourceInputSHA256    string
	TargetSessionID      string
	TargetRoleID         string
	TargetTurnID         string
	TargetAttempt        uint32
	TargetInputSHA256    string
	RootInitiatorActorID string
	GrantGeneration      uint64
	CreatedAt            time.Time
}

// ScheduleOccurrence — назначенный сервером уникальный запуск расписания.
type ScheduleOccurrence struct {
	ID                              string
	ScheduleID                      string
	OrganizationID                  string
	ProjectID                       string
	ScheduledFor                    time.Time
	TargetResourceID                string
	TargetKind                      enum.Kind
	TargetVersion                   uint64
	EffectiveInputSHA256            string
	PromptProfileID                 string
	PromptRevision                  uint64
	RuntimeRevisionID               string
	SessionPolicy                   string
	RoomID                          string
	NotificationPolicy              string
	MaximumExecution                time.Duration
	Coalesce                        bool
	OverlapPolicy                   string
	MaximumAttempts                 uint32
	InitialBackoff                  time.Duration
	MaximumBackoff                  time.Duration
	DeadLetterAt                    time.Time
	State                           string
	Version                         uint64
	Attempt                         uint32
	ClaimantWorkloadID              string
	AuthorityGeneration             uint64
	TokenHash                       string
	ClaimKeySHA256                  string
	LeaseExpiresAt                  time.Time
	AvailableAt                     time.Time
	Outcome                         string
	ResultArtifactID                string
	RecoveryEvidenceSHA256          string
	RecoveryBlockedAt               time.Time
	ExecutionSessionID              string
	ExecutionSessionVersion         uint64
	ExecutionTurnID                 string
	ExecutionTurnVersion            uint64
	ExecutionProcessRunID           string
	ExecutionProcessVersion         uint64
	ExecutionRuntimeRevisionID      string
	ExecutionRuntimeRevisionVersion uint64
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}

// ScheduleOccurrenceCapability — server-issued one-time authority точной
// occurrence/attempt/input/generation и одного полного RPC метода. ID является
// server-owned JTI, TokenSHA256 — сохраняемым digest непрозрачного proof.
type ScheduleOccurrenceCapability struct {
	ID, OrganizationID, ProjectID, OccurrenceID string
	Attempt                                     uint32
	AuthorityGeneration                         uint64
	ImmutableInputSHA256, FullMethod            string
	WorkloadID, CallerSPIFFEID, TokenSHA256     string
	State                                       string
	IssuedAt, ExpiresAt, ConsumedAt, RevokedAt  time.Time
}

// ScheduledRun сохраняет одну неизменяемую попытку фактического исполнения.
type ScheduledRun struct {
	OccurrenceID                       string
	Attempt                            uint32
	SessionID                          string
	SessionVersion                     uint64
	TurnID                             string
	TurnVersion                        uint64
	ProcessRunID                       string
	ProcessVersion                     uint64
	RuntimeRevisionID                  string
	RuntimeRevisionVersion             uint64
	EffectiveInputSHA256               string
	State                              string
	Outcome                            string
	ResultArtifactID                   string
	CreatedAt                          time.Time
	FinishedAt                         time.Time
	ContinuationTurnID                 string
	ContinuationTurnVersion            uint64
	ContinuationRuntimeRevisionID      string
	ContinuationRuntimeRevisionVersion uint64
	ContinuationInputSHA256            string
	OwnerFeedbackSHA256                string
	CurrentSessionID                   string
	CurrentSessionVersion              uint64
	CurrentTurnID                      string
	CurrentTurnVersion                 uint64
	CurrentTurnAttempt                 uint32
	CurrentProcessRunID                string
	CurrentProcessVersion              uint64
	CurrentRuntimeRevisionID           string
	CurrentRuntimeRevisionVersion      uint64
	CurrentInputSHA256                 string
}

// ProviderPoolCursor фиксирует один bounded slot версионированного цикла.
type ProviderPoolCursor struct {
	RoleID         string
	PolicyRevision uint64
	SnapshotSHA256 string
	TotalWeight    uint64
}

// OutboxFailure — ограниченная безопасная проекция terminal-события.
type OutboxFailure struct {
	EventID        string
	OrderingKey    string
	EventSequence  uint64
	EventName      string
	AggregateID    string
	Attempts       uint32
	RepairCount    uint32
	LastErrorClass string
	OccurredAt     time.Time
	UpdatedAt      time.Time
}

// OutboxRepair фиксирует авторизованное повторное открытие exact predecessor.
type OutboxRepair struct {
	EventID            string
	ExpectedSequence   uint64
	ExpectedAttempts   uint32
	ReasonCode         string
	EvidenceSHA256     string
	ActorID            string
	CorrelationID      string
	PolicyRevision     uint64
	IdempotencyKeyHash string
	RequestHash        string
	RepairedAt         time.Time
}

// TurnAttempt сохраняет неизменяемую родословную каждой аренды выполнения.
type TurnAttempt struct {
	TurnID              string
	Attempt             uint32
	WorkloadID          string
	AuthorityGeneration uint64
	State               string
	InputSHA256         string
	LeaseFence          uint64
	StartedAt           time.Time
	FinishedAt          time.Time
	Outcome             string
}

type InteractionDeliveryWork struct {
	ID, OrganizationID, ProjectID, ActorID                   string
	SessionID                                                string
	SessionVersion                                           uint64
	TurnID                                                   string
	TurnVersion                                              uint64
	Attempt                                                  uint32
	RuntimeRevisionID                                        string
	RuntimeRevisionVersion                                   uint64
	ImmutableInputSHA256                                     string
	Kind, LifecycleState, Outcome                            string
	ArtifactID                                               string
	ArtifactVersion                                          uint64
	ArtifactSHA256                                           string
	ArtifactName, ArtifactStorageRef, ArtifactMediaType      string
	ArtifactSizeBytes                                        uint64
	InlinePayload                                            []byte
	NotificationRoomID, NotificationPolicy, ScheduledOutcome string
	Fence                                                    uint64
	LeaseExpiresAt                                           time.Time
}

// InteractionDeliveryReadbackGrant — durable owner receipt выданной capability.
type InteractionDeliveryReadbackGrant struct {
	ID, OrganizationID, ProjectID, ActorID, DeliveryID string
	ProducerID, Purpose, WorkloadID, CallerSPIFFEID    string
	Operation, Permission, CredentialSHA256            string
	Generation, KeysetRevision, KeysetHighWatermark    uint64
	KeysetSHA256                                       string
	IssuedAt, ExpiresAt                                time.Time
	Readiness                                          bool
}

// RuntimeExecution хранит immutable execution tuple и monotonic owner fence.
type RuntimeExecution struct {
	ID                                     string
	OrganizationID                         string
	ProjectID                              string
	ProcessID                              string
	SessionID                              string
	ThreadID                               string
	RoleID                                 string
	TurnID                                 string
	ScheduleOccurrenceID                   string
	Attempt                                uint32
	RuntimeRevisionID                      string
	RuntimeRevisionVersion                 uint64
	RuntimeRevisionSHA256                  string
	ImmutableInputSHA256                   string
	ResourceClass                          string
	ClusterAccessProfile                   string
	WorkloadID                             string
	WorkloadSPIFFEID                       string
	GrantGeneration                        uint64
	Version                                uint64
	Fence                                  uint64
	State                                  string
	LeaseID                                string
	LeaseTokenSHA256                       string
	LeaseExpiresAt                         time.Time
	TerminalOutcome                        string
	TerminalReference                      string
	TerminalSHA256                         string
	ArchiveReference                       string
	ArchiveSHA256                          string
	ArchiveObjectKey                       string
	ArchiveVersionID                       string
	ArchiveKMSKeyARN                       string
	ArchiveObjectLockMode                  string
	ArchiveProvenanceSHA256                string
	RestoreProofReference                  string
	RestoreProofSHA256                     string
	RestoreVerifierWorkload                string
	RestoreVerifierSPIFFEID                string
	RestoreVerifierGeneration              uint64
	CleanupAuthorizationID                 string
	CleanupAuthorizationExpiresAt          time.Time
	CleanupAuthorizationState              string
	CleanupAuthorizationGeneration         uint64
	CleanupConsumedAt                      time.Time
	CleanupPVCName                         string
	CleanupPVCUID                          string
	CleanupPVCResourceVersion              string
	CleanupClaimedAt                       time.Time
	CleanupEligibleAt                      time.Time
	CleanupNotFoundAt                      time.Time
	CleanupDeletionProofSHA256             string
	RestoreSourceExecutionID               string
	RestoreSourceArchiveReference          string
	RestoreSourceArchiveSHA256             string
	RestoreSourceRuntimeRevisionSHA256     string
	RestoreSourceImmutableInputSHA256      string
	RestoreSourceProofReference            string
	RestoreSourceProofSHA256               string
	RestoreSourceVersion                   uint64
	RestoreSourceFence                     uint64
	RestoreSourceArchiveObjectKey          string
	RestoreSourceArchiveVersionID          string
	RestoreSourceArchiveKMSKeyARN          string
	RestoreSourceArchiveObjectLockMode     string
	RestoreSourceArchiveRetainUntil        time.Time
	RestoreSourceRetentionPolicyID         string
	RestoreSourceRetentionPolicyVersion    uint64
	RestoreSourceProvenanceSHA256          string
	EffectiveRuntimeSHA256                 string
	AgentSessionKey                        string
	AgentSessionID                         int64
	AgentSessionTurnID                     int64
	AgentRunID                             string
	AgentBindingSHA256                     string
	RetentionPolicyID                      string
	RetentionPolicyVersion                 uint64
	PVCRetentionSeconds                    uint64
	ArchiveRetentionSeconds                uint64
	ArchiveRetainUntil                     time.Time
	PVCCleanupEligibleAt                   time.Time
	CapacityObservationExpiresAt           time.Time
	RescheduleAfter                        time.Time
	RestoreAssignmentState                 string
	RestoreAssignmentGeneration            uint64
	RestoreTargetPVCName                   string
	RestoreTargetPVCUID                    string
	RestoreTargetPVCResourceVersion        string
	RehydrateProofReference                string
	RehydrateProofSHA256                   string
	CredentialSnapshotSHA256               string
	WorkloadTicketSHA256                   string
	RestoreOperationID                     string
	RestoreOperationGeneration             uint64
	RestoreSourceAuthoritySHA256           string
	ProviderBindingID                      string
	ProviderBindingVersion                 uint64
	ProviderBindingSHA256                  string
	CodexSessionID                         string
	CodexArchiveRelativePath               string
	CodexArchiveSHA256                     string
	CodexArchiveProvenance                 string
	CodexDeliveryRecoverySourceExecutionID string
	Materializations                       []RuntimeMaterialization
	WorkloadTicket                         string `json:"-"`
	ArchiveWorkloadTicket                  string `json:"-"`
	RestoreWorkloadTicket                  string `json:"-"`
	CreatedAt                              time.Time
	UpdatedAt                              time.Time
}

// Backup — безопасный owner read model runtime archive. Поля storage locator,
// credential, private reference/evidence и worker grant в этот тип не входят.
type Backup struct {
	ID, OrganizationID, ProjectID, SessionID                string
	RestoreOperationID                                      string
	SourceRuntimeRevisionSHA256, SourceImmutableInputSHA256 string
	ArchiveSHA256, ProvenanceSHA256                         string
	RuntimeState, State                                     string
	Version, SourceVersion, SourceFence                     uint64
	Restorable                                              bool
	CreatedAt, AvailableAt, RetainUntil, UpdatedAt          time.Time
}

// RuntimeRestoreOperation закрепляет server-owned source/target lineage.
// Текущее состояние выводится из target RuntimeExecution тем же read path.
type RuntimeRestoreOperation struct {
	ID, OrganizationID, ProjectID, OwnerActorID          string
	BackupID, SessionID, TargetTurnID, TargetExecutionID string
	ArchiveSHA256, ProvenanceSHA256                      string
	SourceAuthoritySHA256                                string
	SourceVersion, SourceFence                           uint64
	Generation, ConsumedGeneration, RevokedGeneration    uint64
	TargetAttempt                                        uint32
	TargetExecutionVersion, TargetTurnVersion            uint64
	TargetExecutionState, TargetRestoreAssignmentState   string
	TargetTurnState                                      string
	CreatedAt, UpdatedAt                                 time.Time
}

// CodexLineage — последняя подтверждённая control-plane terminal lineage.
// Credential revision намеренно не является частью logical account identity.
type CodexLineage struct {
	ExecutionID, ProviderBindingID, SessionID, ArchiveRelativePath, ArchiveSHA256, ArchiveProvenance string
	TerminalOutcome, TerminalReference                                                               string
}

type RuntimeMaterialization struct {
	Kind, ArtifactID, SHA256, RelativePath, MediaType, StorageRef string
	ArtifactVersion, SizeBytes                                    uint64
}

// ResourceRetentionPolicy — owner-managed и versioned политика, которую claim
// копирует в immutable execution snapshot. Последующие переходы не читают
// текущую конфигурацию процесса.
type ResourceRetentionPolicy struct {
	ID                      string
	Version                 uint64
	PVCRetentionSeconds     uint64
	ArchiveRetentionSeconds uint64
	EffectiveFrom           time.Time
	RetiredAt               time.Time
}

// RuntimeRetentionHold — owner-managed запрет необратимого cleanup.
type RuntimeRetentionHold struct {
	ID             string
	OrganizationID string
	ProjectID      string
	SessionID      string
	Kind           string
	State          string
	Version        uint64
	ActorID        string
	ReasonCode     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ReleasedAt     time.Time
}

// PinnedIntegrationResource хранит exact owner version/projection без secret value.
type PinnedIntegrationResource struct {
	ResourceID       string `json:"resourceId"`
	Version          uint64 `json:"version"`
	ProjectionSHA256 string `json:"projectionSha256"`
}

// RuntimeIncident — append-only watchdog evidence exact execution/fence.
type RuntimeIncident struct {
	ID             string
	OrganizationID string
	ProjectID      string
	ExecutionID    string
	ExecutionFence uint64
	Kind           string
	EvidenceSHA256 string
	WorkloadID     string
	OccurredAt     time.Time
	Version        uint64
	State          string
	ReasonCode     string
	UpdatedAt      time.Time
}

// RuntimeIncidentHistory фиксирует каждое специализированное owner action.
type RuntimeIncidentHistory struct {
	IncidentID   string
	Version      uint64
	State        string
	Action       string
	ReasonCode   string
	OccurredAt   time.Time
	OwnerActorID string
}

// OwnerSessionState — verifying-side durable current OIDC session fence.
type OwnerSessionState struct {
	OrganizationID         string
	ActorID                string
	SessionID              string
	CredentialDigestSHA256 string
	CurrentRevision        uint64
	RevokedAt              time.Time
	UpdatedAt              time.Time
}

// GatewayPublicTLSMaterial — публичные метаданные exact leaf без private key.
type GatewayPublicTLSMaterial struct {
	Generation        uint64
	CertificateSHA256 string
	NotBefore         time.Time
	NotAfter          time.Time
}

// GatewayPublicTLSState — durable APPLIED/PENDING/PREVIOUS served protocol.
type GatewayPublicTLSState struct {
	OrganizationID   string
	ProjectID        string
	WorkloadID       string
	Applied          GatewayPublicTLSMaterial
	Pending          GatewayPublicTLSMaterial
	Previous         GatewayPublicTLSMaterial
	OverlapExpiresAt time.Time
	UpdatedAt        time.Time
}

// IntegrationContinuation хранит typed approval, execution result и rejoin tuple.
type IntegrationContinuation struct {
	ID                                 string
	OrganizationID                     string
	ProjectID                          string
	ProcessID                          string
	SessionID                          string
	SessionVersion                     uint64
	ThreadID                           string
	RoleID                             string
	TurnID                             string
	TurnVersion                        uint64
	Attempt                            uint32
	RuntimeRevisionID                  string
	RuntimeRevisionVersion             uint64
	RuntimeRevisionSHA256              string
	ImmutableInputSHA256               string
	GrantGeneration                    uint64
	InvocationID                       string
	ApprovalID                         string
	IntegrationID                      string
	IntegrationVersion                 uint64
	IntegrationSHA256                  string
	CredentialBindings                 []PinnedIntegrationResource
	RequestSHA256                      string
	ApprovalState                      string
	ExecutionState                     string
	ContinuationState                  string
	Version                            uint64
	Fence                              uint64
	ApprovalExpiresAt                  time.Time
	DecisionReference                  string
	DecisionSHA256                     string
	ResultReference                    string
	ResultSHA256                       string
	ErrorCode                          string
	ErrorReference                     string
	ErrorSHA256                        string
	ContinuationTurnID                 string
	ContinuationTurnVersion            uint64
	ContinuationAttempt                uint32
	ContinuationRuntimeRevisionID      string
	ContinuationRuntimeRevisionVersion uint64
	ContinuationInputSHA256            string
	CreatedAt                          time.Time
	UpdatedAt                          time.Time
}

// Tombstone — безопасное авторитетное представление удалённого агрегата.
type Tombstone struct {
	ResourceID       string
	Kind             enum.Kind
	Version          uint64
	ProjectionSHA256 string
	DeletedAt        time.Time
}

// Diagnostics содержит только ограниченные безопасные операционные счётчики.
type Diagnostics struct {
	SchemaVersion              uint64
	PendingOutboxEvents        uint64
	TerminalOutboxEvents       uint64
	OldestPendingAge           time.Duration
	ActiveTurnLeases           uint64
	QueuedScheduleOccurrences  uint64
	RuntimePrincipalStatus     string
	RuntimePrincipalGeneration uint64
}

// MemoryProjection — локальная перестраиваемая проекция pgvector.
type MemoryProjection struct {
	ResourceID       string
	OrganizationID   string
	ProjectID        string
	ResourceVersion  uint64
	ContentSHA256    string
	ModelID          string
	ModelRevision    uint64
	ModelSHA256      string
	Embedding        []float32
	ProjectionSHA256 string
	UpdatedAt        time.Time
}

// MemorySearch описывает ограниченный полнотекстовый поиск и необязательный
// векторный запрос в области организации и роли.
type MemorySearch struct {
	OrganizationID      string
	ProjectID           string
	Scope               string
	RoleID              string
	Query               string
	QueryEmbedding      []float32
	ModelID             string
	ModelRevision       uint64
	ModelSHA256         string
	AfterID             string
	AfterTextRank       float32
	AfterVectorDistance float32
	AfterVectorUsed     bool
	Limit               int
	CanReadProject      bool
	ActorRoleIDs        []string
	ParentID            string
	States              []enum.State
	GenericOrder        bool
}

type MemorySearchHit struct {
	Resource             entity.Resource
	TextRank             float32
	VectorDistance       float32
	VectorProjectionUsed bool
}

// Transaction выражает одну транзакцию команды без утечки pgx.
type Transaction interface {
	CurrentTime(context.Context) (time.Time, error)
	GetReceipt(context.Context, string, string, string) (Receipt, error)
	SaveReceipt(context.Context, Receipt) error
	Get(context.Context, string, string, string) (entity.Resource, error)
	GetForUpdate(context.Context, string, string, string) (entity.Resource, error)
	GetForUpdateIncludingDeleted(context.Context, string, string, string) (entity.Resource, error)
	ProjectHasLiveResources(context.Context, string, string) (bool, error)
	Insert(context.Context, entity.Resource) error
	Update(context.Context, entity.Resource, uint64) error
	AppendAudit(context.Context, Audit) error
	AppendEvent(context.Context, event.Change) error
	ActorPermissions(context.Context, string, string, string) ([]string, error)
	ActorRoleIDs(context.Context, string, string, string) ([]string, error)
	ListSnapshotResources(context.Context, string, string) ([]entity.Resource, error)
	LatestRuntimeRevision(context.Context, string, string) (entity.Resource, error)
	ExpiredClaimedTurnCandidates(context.Context, string, string, string, int, time.Time) ([]ExpiredTurn, error)
	OpenSessionTurns(context.Context, string, string, string) ([]SessionTurn, error)
	SessionHasLiveRuntimeExecution(context.Context, string, string, string) (bool, error)
	SessionHasUnverifiedRuntimeArchive(context.Context, string, string, string) (bool, error)
	SessionHasActiveRuntimeCleanup(context.Context, string, string, string) (bool, error)
	SessionBlocksRuntimeCleanup(context.Context, string, string, string) (bool, error)
	LatestSessionRuntimeArchiveForRestore(context.Context, string, string, string) (RuntimeExecution, error)
	NextQueuedTurn(context.Context, string, string, string) (entity.Resource, error)
	SaveTurnLease(context.Context, TurnLease) error
	RenewTurnLease(context.Context, TurnLease, time.Time) (TurnLease, error)
	ValidateTurnLease(
		context.Context,
		string,
		string,
		string,
		uint64,
		uint32,
		time.Time,
	) (TurnLease, error)
	GetTurnLeaseForUpdate(context.Context, string) (TurnLease, error)
	DeleteTurnLease(context.Context, string, uint64) error
	SaveTurnAttempt(context.Context, TurnAttempt) error
	FinishTurnAttempt(context.Context, TurnAttempt) error
	EnqueueInteractionDelivery(context.Context, InteractionDeliveryWork) error
	ClaimInteractionDelivery(context.Context, string, string, string, string, time.Duration) (InteractionDeliveryWork, error)
	CompleteInteractionDelivery(context.Context, string, string, string, uint64, string, string) error
	SaveInteractionDeliveryReadbackGrant(context.Context, InteractionDeliveryReadbackGrant) error
	ValidateInteractionDeliveryReadbackGrant(context.Context, string, string, string, string, string, uint64) (bool, error)
	GetTurnAttemptForUpdate(context.Context, string, uint32) (TurnAttempt, error)
	GetRuntimeExecutionForUpdate(context.Context, string) (RuntimeExecution, error)
	GetRuntimeExecutionByTurn(context.Context, string, uint32) (RuntimeExecution, error)
	GetRuntimeExecutionByTurnForUpdate(context.Context, string, uint32) (RuntimeExecution, error)
	GetCurrentResourceRetentionPolicy(context.Context, string, string) (ResourceRetentionPolicy, error)
	InsertRuntimeExecution(context.Context, RuntimeExecution) error
	UpdateRuntimeExecution(context.Context, RuntimeExecution, uint64, uint64) error
	InsertRuntimeRestoreOperation(context.Context, RuntimeRestoreOperation) error
	GetRuntimeRestoreOperation(context.Context, string) (RuntimeRestoreOperation, error)
	GetRuntimeRestoreOperationByBackup(context.Context, string) (RuntimeRestoreOperation, error)
	AdvanceRuntimeRestoreOperation(context.Context, RuntimeRestoreOperation, uint64) error
	ConsumeRuntimeRestoreOperation(context.Context, string, uint64, string, uint32, string, time.Time) error
	RevokeRuntimeRestoreOperation(context.Context, string, uint64, time.Time) error
	AuthorizeRuntimeRestoreEffect(context.Context, string, string, uint64, string, string, string, time.Time) (bool, error)
	NextExpiredRuntimeExecution(
		context.Context, string, string, string, uint32,
	) (RuntimeExecution, error)
	InsertRuntimeIncident(context.Context, RuntimeIncident) error
	AdmitOwnerSession(context.Context, OwnerSessionState) (OwnerSessionState, error)
	RequireOwnerSession(context.Context, OwnerSessionState, bool) error
	RevokeOwnerSession(context.Context, OwnerSessionState) (OwnerSessionState, error)
	PrepareGatewayPublicTLS(context.Context, GatewayPublicTLSState, GatewayPublicTLSMaterial, uint64, string, time.Time) (GatewayPublicTLSState, error)
	ConfirmGatewayPublicTLS(context.Context, GatewayPublicTLSState, uint64, string, time.Time, time.Time) (GatewayPublicTLSState, error)
	CheckGatewayPublicTLS(context.Context, GatewayPublicTLSState, uint64, string, time.Time) (GatewayPublicTLSState, error)
	GetIntegrationContinuationForUpdate(context.Context, string) (IntegrationContinuation, error)
	AdmitContinuationGrantVerifierState(context.Context, uint64, uint64, uint64, string, uint64) error
	GetIntegrationContinuation(context.Context, string) (IntegrationContinuation, error)
	GetIntegrationContinuationByContinuationTurn(context.Context, string) (IntegrationContinuation, error)
	IntegrationContinuationBlocksCleanup(context.Context, string, string, string) (bool, error)
	NextExpiredIntegrationContinuation(
		context.Context, string, string, string, uint32,
	) (IntegrationContinuation, error)
	InsertIntegrationContinuation(context.Context, IntegrationContinuation) error
	UpdateIntegrationContinuation(context.Context, IntegrationContinuation, uint64, uint64) error
	SaveDelegationEdge(context.Context, DelegationEdge) error
	GetDelegationEdgeByTargetTurn(context.Context, string, string, string) (DelegationEdge, error)
	DueSchedules(context.Context, string, string, int, time.Time) ([]entity.Resource, error)
	NextAutomationProject(context.Context, string, string) (string, error)
	SaveScheduleOccurrence(context.Context, ScheduleOccurrence) error
	HasOpenScheduleOccurrence(context.Context, string, string, string) (bool, error)
	HasBlockingScheduleExecution(
		context.Context,
		string,
		string,
		string,
		string,
	) (bool, error)
	SkipOverlappedScheduleOccurrences(
		context.Context,
		string,
		string,
		time.Time,
		int,
	) ([]ScheduleOccurrence, error)
	ExpiredScheduleOccurrenceCandidates(
		context.Context,
		string,
		string,
		time.Time,
	) ([]ScheduleOccurrence, error)
	NextScheduleOccurrence(context.Context, string, string, time.Time) (ScheduleOccurrence, error)
	UpdateScheduleOccurrence(context.Context, ScheduleOccurrence, uint32, string) error
	GetScheduleOccurrence(context.Context, string, string, string) (ScheduleOccurrence, error)
	GetScheduleOccurrenceForUpdate(context.Context, string, string, string) (ScheduleOccurrence, error)
	GetScheduleOccurrenceByCurrentTurn(context.Context, string, string, string) (ScheduleOccurrence, error)
	GetScheduleOccurrenceByClaimKey(context.Context, string, string, string) (ScheduleOccurrence, error)
	InsertScheduleOccurrenceCapability(context.Context, ScheduleOccurrenceCapability) error
	GetScheduleOccurrenceCapabilityForUpdate(context.Context, string) (ScheduleOccurrenceCapability, error)
	GetScheduleOccurrenceCapabilityByOccurrenceForUpdate(context.Context, string, uint32, string, uint64) (ScheduleOccurrenceCapability, error)
	UpdateScheduleOccurrenceCapability(context.Context, ScheduleOccurrenceCapability, string) error
	SaveScheduledRun(context.Context, ScheduledRun) error
	GetScheduledRunForUpdate(context.Context, string, uint32) (ScheduledRun, error)
	GetScheduledRunByCurrentTurnForUpdate(context.Context, string) (ScheduledRun, error)
	WaitScheduledRun(context.Context, ScheduledRun) error
	SuspendScheduledRun(context.Context, ScheduledRun, string, uint32) error
	ContinueScheduledRun(context.Context, ScheduledRun) error
	RebindScheduledRun(context.Context, ScheduledRun, string, uint32) error
	FinishScheduledRun(context.Context, ScheduledRun) error
	AuthorizeProject(context.Context, string, string, string, string, string) (entity.Resource, error)
	NextProofRevision(context.Context) (uint64, error)
	SaveMemoryProjection(context.Context, MemoryProjection) error
	SearchMemory(context.Context, MemorySearch) ([]MemorySearchHit, error)
	HasActiveChildProcesses(context.Context, string, string, string) (bool, error)
	NextOwnerGateDelivery(context.Context, string, string, time.Time) (entity.Resource, error)
	OwnerGateByDeliveryClaimKey(context.Context, string, string, string) (entity.Resource, error)
	NextExpiredOwnerGateCandidate(context.Context, string, string) (entity.Resource, error)
	ListTerminalOutbox(context.Context, string, string, string, int) ([]OutboxFailure, error)
	RepairTerminalOutbox(context.Context, OutboxRepair) (OutboxFailure, error)
	ActiveProviderSessions(context.Context, string, string, string) (uint64, error)
	NextProviderPoolSlot(context.Context, ProviderPoolCursor) (uint64, error)
	ProcessHasOpenWork(context.Context, string, string, string, string, string) (bool, error)
	ActiveWorkClaimsForUpdate(context.Context, string, string, string, string) ([]entity.Resource, error)
	ActiveProcessTurnCandidates(context.Context, string, string, string) ([]entity.Resource, error)
	ActiveOwnerGateForProcess(context.Context, string, string, string) (entity.Resource, error)
}

// ProtectedTransaction — узкий порт owner-конфигурации Issue #234. Он не
// расширяет legacy Transaction и не превращает generic CRUD в protected API.
type ProtectedTransaction interface {
	GetByStableKeyForUpdate(context.Context, string, string, enum.Kind, string) (entity.Resource, error)
	GetByNameForUpdate(context.Context, string, string, enum.Kind, string) (entity.Resource, error)
	AppendProtectedResourceHistory(context.Context, ProtectedResourceHistory) error
	GetProtectedResourceHistoryVersion(context.Context, string, uint64) (ProtectedResourceHistory, error)
	GetInstructionHistoryContentVersion(context.Context, string, uint64) (ProtectedResourceHistory, error)
	GetRuntimeIncidentForUpdate(context.Context, string) (RuntimeIncident, error)
	UpdateRuntimeIncident(context.Context, RuntimeIncident, uint64) error
	AppendRuntimeIncidentHistory(context.Context, RuntimeIncidentHistory) error
}

// ImageTransaction materialизует только специализированный image lifecycle.
// Отдельный порт не расширяет generic CRUD и сохраняет закрытый реестр команд.
type ImageTransaction interface {
	NextImageBuild(context.Context, string, string, time.Time) (entity.Resource, error)
	NextImageAdmission(context.Context, string, string, time.Time) (entity.Resource, error)
	NextImagePromotion(context.Context, string, string, uint64, string, time.Time) (entity.Resource, error)
	PromotedImageArtifactBySpec(context.Context, string, string, string, string, uint64, string) (entity.Resource, error)
	ImageBuildsForRecipeForUpdate(context.Context, string, string, string) ([]entity.Resource, error)
	ImageArtifactsForRecipeForUpdate(context.Context, string, string, string) ([]entity.Resource, error)
}

// Repository — авторитетная граница PostgreSQL.
type Repository interface {
	Transact(context.Context, Scope, func(Transaction) error) error
	Get(context.Context, string, string, string, enum.Kind) (entity.Resource, error)
	GetIncludingDeleted(context.Context, string, string, string, enum.Kind) (entity.Resource, error)
	List(context.Context, query.ResourceFilter) ([]entity.Resource, error)
	Search(context.Context, query.ResourceSearch) ([]entity.Resource, error)
	ListEligibleProjects(context.Context, string, string, string, int) ([]entity.Resource, error)
	ListAudit(context.Context, query.AuditFilter) ([]Audit, error)
	ListRuntimeIncidents(context.Context, query.RuntimeIncidentFilter) ([]RuntimeIncident, error)
	ListBackups(context.Context, string, string, string, string, int) ([]Backup, error)
	GetBackup(context.Context, string, string, string, string) (Backup, error)
	GetRuntimeRestoreOperation(context.Context, string, string, string, string) (RuntimeRestoreOperation, error)
	ListRuntimeRestoreOperations(context.Context, string, string, string, string, string, int) ([]RuntimeRestoreOperation, error)
	ListTombstones(context.Context, query.TombstoneFilter) ([]Tombstone, error)
	ListScheduleOccurrences(context.Context, query.ScheduleOccurrenceFilter) ([]ScheduleOccurrence, error)
	Diagnostics(context.Context, Scope) (Diagnostics, error)
	CacheEpoch(context.Context, string, string) (uint64, error)
	Check(context.Context) error
	Close()
}

// ProtectedRepository — typed read/history boundary Issue #234.
type ProtectedRepository interface {
	ListProtectedResourceHistory(context.Context, string, string, string, string, uint64, int) ([]ProtectedResourceHistory, error)
	GetRuntimeIncident(context.Context, string, string, string, string) (RuntimeIncident, error)
	ListRuntimeIncidentHistory(context.Context, string, string, string, string, uint64, int) ([]RuntimeIncidentHistory, error)
}
