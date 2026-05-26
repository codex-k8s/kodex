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
	GetAcceptedEvent(ctx context.Context, eventID uuid.UUID) (entity.AcceptedEvent, bool, error)
	RecordAcceptedEvent(ctx context.Context, event entity.AcceptedEvent) error
}
