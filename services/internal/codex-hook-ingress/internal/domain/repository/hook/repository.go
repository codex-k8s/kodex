// Package hook contains codex-hook-ingress repository contracts.
package hook

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
)

// Repository stores only service-local safe records for idempotency and diagnostics.
type Repository interface {
	Ready() bool
	// FindAcceptedEvent returns an existing idempotency record without mutating admission counters.
	FindAcceptedEvent(ctx context.Context, eventID uuid.UUID) (entity.AcceptedEvent, bool, error)
	// RegisterAcceptedEvent atomically inserts event_id+payload_digest or returns the existing record.
	RegisterAcceptedEvent(ctx context.Context, event entity.AcceptedEvent) (entity.AcceptedEvent, bool, error)
	// RecordDeliveryResults stores safe owner route diagnostics for the accepted event.
	RecordDeliveryResults(ctx context.Context, update entity.DeliveryUpdate) (entity.AcceptedEvent, error)
}
