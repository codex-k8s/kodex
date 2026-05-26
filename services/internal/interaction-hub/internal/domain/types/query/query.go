package query

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

type ConversationMessageFilter struct {
	ThreadID uuid.UUID
	Page     value.PageRequest
}

type CommandIdentity struct {
	CommandID      uuid.UUID
	IdempotencyKey string
	ActorRef       string
	Operation      enum.Operation
}
