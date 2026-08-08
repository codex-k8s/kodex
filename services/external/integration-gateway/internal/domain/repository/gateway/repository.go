package gateway

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
)

type Scope struct {
	TenantID  string
	ProjectID string
	ActorID   string
}

type SessionAdmission struct {
	Session     entity.TransportSession
	Connections []entity.Connection
	Grants      []entity.Grant
	Audit       entity.AuditEvent
}

type InvocationReservation struct {
	Invocation     entity.Invocation
	Approval       *entity.Approval
	Continuation   entity.ContinuationEffect
	ReceiptKeyHash string
	RequestHash    string
	Audit          entity.AuditEvent
}

type Decision struct {
	ApprovalID          string
	ExpectedVersion     uint64
	ExpectedRequestHash string
	Approve             bool
	ActorID             string
	ReasonCode          string
	ReceiptKeyHash      string
	RequestHash         string
	DecidedAt           time.Time
	Audit               entity.AuditEvent
}

type Cancellation struct {
	InvocationID               string
	ExpectedTransportSessionID string
	ActorID                    string
	ReasonCode                 string
	ReceiptKeyHash             string
	RequestHash                string
	CancelledAt                time.Time
	Audit                      entity.AuditEvent
}

type ExecutionClaim struct {
	Invocation    entity.Invocation
	Attempt       entity.ExecutionAttempt
	Tool          entity.Tool
	Connection    entity.Connection
	ProviderReady bool
}

type ContinuationClaim struct {
	Effect     entity.ContinuationEffect
	Invocation entity.Invocation
	Approval   entity.Approval
	Attempt    *entity.ExecutionAttempt
	Result     *entity.Result
}

type ContinuationCompletion struct {
	InvocationID             string
	Action                   enum.ContinuationAction
	LeaseID                  string
	LeaseFence               uint64
	State                    ContinuationState
	EncryptedTransitionGrant []byte
	TransitionGrantExpiresAt time.Time
}

type ContinuationState struct {
	ID                string
	Version           uint64
	Fence             uint64
	ApprovalState     string
	ExecutionState    string
	ContinuationState string
}

type ContinuationRetry struct {
	InvocationID string
	Action       enum.ContinuationAction
	LeaseID      string
	LeaseFence   uint64
	Backoff      time.Duration
}

type ToolBinding struct {
	Tool             entity.Tool
	Connection       entity.Connection
	Grant            entity.Grant
	DefinitionDigest string
}

type ExecutionCompletion struct {
	InvocationID         string
	AttemptID            string
	Fence                uint64
	ConnectionGeneration uint64
	GrantGeneration      uint64
	Result               entity.Result
	Audit                entity.AuditEvent
}

type ConnectionValidation struct {
	ConnectionID       string
	ExpectedGeneration uint64
	Status             entity.Connection
	Audit              entity.AuditEvent
}

type Transaction interface {
	StoreDefinition(context.Context, entity.Definition) error
	AdmitSession(context.Context, SessionAdmission) error
	ReserveInvocation(context.Context, InvocationReservation) (entity.Invocation, bool, error)
	DecideApproval(context.Context, Decision) (entity.Invocation, bool, error)
	CancelInvocation(context.Context, Cancellation) (entity.Invocation, bool, error)
	ClaimExecution(context.Context, time.Time) (ExecutionClaim, bool, error)
	MarkProviderDispatched(context.Context, string, string, time.Time) error
	CompleteExecution(context.Context, ExecutionCompletion) error
	SetConnectionValidation(context.Context, ConnectionValidation) error
	CloseSession(context.Context, string, time.Time, entity.AuditEvent) error
	Expire(context.Context, time.Time, int) (int64, error)
	ClaimContinuation(context.Context, time.Duration) (ContinuationClaim, bool, error)
	CompleteContinuation(context.Context, ContinuationCompletion) error
	RetryContinuation(context.Context, ContinuationRetry) error
}

type Repository interface {
	Transact(context.Context, Scope, func(Transaction) error) error
	NextExecutionScope(context.Context) (Scope, bool, error)
	NextLifecycleScope(context.Context) (Scope, bool, error)
	NextContinuationScope(context.Context) (Scope, bool, error)
	GetConnection(context.Context, Scope, string) (entity.Connection, error)
	ListTools(context.Context, Scope, string) ([]ToolBinding, error)
	GetInvocation(context.Context, Scope, string) (entity.Invocation, *entity.Approval, *entity.Result, error)
	TouchSession(context.Context, Scope, string, string, time.Time, time.Time, uint64, uint32) (entity.TransportSession, error)
	ReleaseSession(context.Context, Scope, string) error
	Check(context.Context) error
}
