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

type SubscriptionFilter struct {
	Scope         value.ScopeRef
	SubscriberRef string
	Status        enum.SubscriptionStatus
	Page          value.PageRequest
}

type CommandIdentity struct {
	CommandID      uuid.UUID
	IdempotencyKey string
	ActorRef       string
	Operation      enum.Operation
}
