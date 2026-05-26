// Package entity contains codex-hook-ingress service-local persistence entities.
package entity

import (
	"time"

	"github.com/google/uuid"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// AcceptedEvent is the safe idempotency record for a normalized hook event.
type AcceptedEvent struct {
	EventID        uuid.UUID
	PayloadDigest  string
	HookEventName  hookenum.HookEventName
	CorrelationID  string
	RetentionClass hookenum.RetentionClass
	Result         value.HookHandlerResult
	RecordedAt     time.Time
}
