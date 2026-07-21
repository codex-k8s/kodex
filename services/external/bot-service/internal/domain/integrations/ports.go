package integrations

import (
	"context"
	"time"
)

// SessionAdmission аутентифицирует bearer и возвращает только server-resolved scope.
type SessionAdmission interface {
	AuthorizeIntegrationSession(ctx context.Context, sessionKey string, token string) (SessionContext, error)
}

// Repository владеет состоянием интеграций и переходами T1–T5.
type Repository interface {
	ListCatalog(ctx context.Context, session SessionContext, now time.Time) ([]CatalogEntry, error)
	CreateOrReplayInvocation(ctx context.Context, input CreateInvocationInput) (Invocation, bool, error)
	ClaimApprovalDelivery(ctx context.Context, approvalID int64, leaseOwner string, now time.Time, leaseDuration time.Duration) (ApprovalDelivery, bool, error)
	CompleteApprovalDelivery(ctx context.Context, approvalID int64, leaseOwner string, postID string, now time.Time) error
	ReleaseApprovalDelivery(ctx context.Context, approvalID int64, leaseOwner string, reasonCode string, now time.Time) error
	DecideApproval(ctx context.Context, input ApprovalDecisionInput) (Invocation, error)
	ClaimExecution(ctx context.Context, workerID string, proposedFence string, now time.Time, leaseDuration time.Duration) (ExecutionClaim, bool, error)
	CancelExecution(ctx context.Context, claim ExecutionClaim, reasonCode string, now time.Time) error
	FinalizeExecution(ctx context.Context, claim ExecutionClaim, now time.Time) (Invocation, error)
}

// RecordingStore является единственным side-effect port текущего среза.
type RecordingStore interface {
	RecordExecution(ctx context.Context, claim ExecutionClaim, executionID string, now time.Time) (ExecutionReceipt, error)
}

// RecordingExecutor записывает безопасную квитанцию, но не изменяет внешние системы.
type RecordingExecutor interface {
	Execute(ctx context.Context, claim ExecutionClaim) (ExecutionReceipt, error)
}

// ApprovalCardPublisher обеспечивает idempotent ensure/readback карточки.
type ApprovalCardPublisher interface {
	EnsureApprovalCard(ctx context.Context, delivery ApprovalDelivery) (string, error)
}

// CreateInvocationInput содержит полностью проверенный T1 binding.
type CreateInvocationInput struct {
	Session            SessionContext
	ConnectionPublicID string
	CapabilityKey      string
	IdempotencyKey     string
	Arguments          RestartArguments
	ArgumentsHash      string
	InvocationPublicID string
	ApprovalPublicID   string
	CorrelationID      string
	Now                time.Time
	ApprovalExpiresAt  time.Time
}
