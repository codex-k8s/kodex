// Package hook contains codex-hook-ingress repository contracts.
package hook

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
)

// Repository stores only service-local safe records for idempotency and diagnostics.
type Repository interface {
	Ready() bool
	// RegisterAcceptedEvent atomically inserts event_id+payload_digest or returns the existing record.
	RegisterAcceptedEvent(ctx context.Context, event entity.AcceptedEvent) (entity.AcceptedEvent, bool, error)
}
