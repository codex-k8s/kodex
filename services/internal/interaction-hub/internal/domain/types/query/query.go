package query

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

type ConversationMessageFilter struct {
	ThreadID uuid.UUID
	Page     value.PageRequest
}

type InteractionRequestFilter struct {
	Scope           value.ScopeRef
	RequestKind     enum.InteractionRequestKind
	Status          enum.InteractionRequestStatus
	SourceOwnerKind enum.SourceOwnerKind
	SourceOwnerRef  string
	DeadlineBefore  *time.Time
	Page            value.PageRequest
}

type OwnerInboxFilter struct {
	RequestID          uuid.UUID
	Scope              value.ScopeRef
	RequestKinds       []enum.InteractionRequestKind
	Statuses           []enum.InteractionRequestStatus
	SourceOwnerKind    enum.SourceOwnerKind
	SourceOwnerRef     string
	AssigneeRef        value.ActorRef
	ActorRef           string
	CorrelationRef     value.ExternalRef
	CorrelationID      string
	IncludeDiagnostics bool
	Page               value.PageRequest
}

type SubscriptionFilter struct {
	Scope         value.ScopeRef
	SubscriberRef string
	Status        enum.SubscriptionStatus
	Page          value.PageRequest
}

type DeliveryAttemptFilter struct {
	Target     value.DeliveryTarget
	DeliveryID string
	Limit      int32
}

type ChannelCallbackFilter struct {
	DeliveryAttemptIDs []uuid.UUID
	RequestID          uuid.UUID
	DeliveryID         string
}

type CommandIdentity struct {
	CommandID      uuid.UUID
	IdempotencyKey string
	ActorRef       string
	Operation      enum.Operation
}
