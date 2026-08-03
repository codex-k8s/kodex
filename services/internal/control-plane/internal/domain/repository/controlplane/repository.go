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
	Attempt                         uint32
	ClaimantWorkloadID              string
	AuthorityGeneration             uint64
	TokenHash                       string
	ClaimKeySHA256                  string
	LeaseExpiresAt                  time.Time
	AvailableAt                     time.Time
	Outcome                         string
	ResultArtifactID                string
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

// RuntimeExecution хранит immutable execution tuple и monotonic owner fence.
type RuntimeExecution struct {
	ID                                 string
	OrganizationID                     string
	ProjectID                          string
	ProcessID                          string
	SessionID                          string
	ThreadID                           string
	RoleID                             string
	TurnID                             string
	Attempt                            uint32
	RuntimeRevisionID                  string
	RuntimeRevisionVersion             uint64
	RuntimeRevisionSHA256              string
	ImmutableInputSHA256               string
	ResourceClass                      string
	ClusterAccessProfile               string
	WorkloadID                         string
	WorkloadSPIFFEID                   string
	GrantGeneration                    uint64
	Version                            uint64
	Fence                              uint64
	State                              string
	LeaseID                            string
	LeaseTokenSHA256                   string
	LeaseExpiresAt                     time.Time
	TerminalOutcome                    string
	TerminalReference                  string
	TerminalSHA256                     string
	ArchiveReference                   string
	ArchiveSHA256                      string
	RestoreProofReference              string
	RestoreProofSHA256                 string
	RestoreVerifierWorkload            string
	RestoreVerifierSPIFFEID            string
	RestoreVerifierGeneration          uint64
	CleanupAuthorizationID             string
	CleanupAuthorizationExpiresAt      time.Time
	CleanupAuthorizationState          string
	CleanupAuthorizationGeneration     uint64
	CleanupConsumedAt                  time.Time
	CleanupPVCName                     string
	CleanupPVCUID                      string
	CleanupPVCResourceVersion          string
	CleanupClaimedAt                   time.Time
	CleanupEligibleAt                  time.Time
	CleanupNotFoundAt                  time.Time
	CleanupDeletionProofSHA256         string
	RestoreSourceExecutionID           string
	RestoreSourceArchiveReference      string
	RestoreSourceArchiveSHA256         string
	RestoreSourceRuntimeRevisionSHA256 string
	RestoreSourceImmutableInputSHA256  string
	RestoreSourceProofSHA256           string
	CreatedAt                          time.Time
	UpdatedAt                          time.Time
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
	GetTurnAttemptForUpdate(context.Context, string, uint32) (TurnAttempt, error)
	GetRuntimeExecutionForUpdate(context.Context, string) (RuntimeExecution, error)
	GetRuntimeExecutionByTurn(context.Context, string, uint32) (RuntimeExecution, error)
	GetRuntimeExecutionByTurnForUpdate(context.Context, string, uint32) (RuntimeExecution, error)
	InsertRuntimeExecution(context.Context, RuntimeExecution) error
	UpdateRuntimeExecution(context.Context, RuntimeExecution, uint64, uint64) error
	NextExpiredRuntimeExecution(
		context.Context, string, string, string, uint32,
	) (RuntimeExecution, error)
	InsertRuntimeIncident(context.Context, RuntimeIncident) error
	GetIntegrationContinuationForUpdate(context.Context, string) (IntegrationContinuation, error)
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
	SaveScheduledRun(context.Context, ScheduledRun) error
	GetScheduledRunForUpdate(context.Context, string, uint32) (ScheduledRun, error)
	GetScheduledRunByCurrentTurnForUpdate(context.Context, string) (ScheduledRun, error)
	WaitScheduledRun(context.Context, string, uint32) error
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

// Repository — авторитетная граница PostgreSQL.
type Repository interface {
	Transact(context.Context, Scope, func(Transaction) error) error
	Get(context.Context, string, string, string, enum.Kind) (entity.Resource, error)
	List(context.Context, query.ResourceFilter) ([]entity.Resource, error)
	Search(context.Context, query.ResourceSearch) ([]entity.Resource, error)
	ListEligibleProjects(context.Context, string, string, string, int) ([]entity.Resource, error)
	ListAudit(context.Context, query.AuditFilter) ([]Audit, error)
	ListTombstones(context.Context, query.TombstoneFilter) ([]Tombstone, error)
	ListScheduleOccurrences(context.Context, query.ScheduleOccurrenceFilter) ([]ScheduleOccurrence, error)
	Diagnostics(context.Context, Scope) (Diagnostics, error)
	CacheEpoch(context.Context, string, string) (uint64, error)
	Check(context.Context) error
	Close()
}
