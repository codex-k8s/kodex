// Package hook provides a safe in-memory repository stub for CHI-3.
package hook

import (
	"context"
	"sync"

	"github.com/google/uuid"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookrepo "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/repository/hook"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

var _ hookrepo.Repository = (*Repository)(nil)

// Repository stores idempotency metadata without raw payload.
type Repository struct {
	mu     sync.RWMutex
	events map[uuid.UUID]entity.AcceptedEvent
}

// NewRepository creates an in-memory CHI-3 repository stub.
func NewRepository() *Repository {
	return &Repository{events: make(map[uuid.UUID]entity.AcceptedEvent)}
}

// Ready reports whether the stub repository was initialized.
func (r *Repository) Ready() bool {
	return r != nil && r.events != nil
}

// FindAcceptedEvent returns an existing event idempotency record without changing state.
func (r *Repository) FindAcceptedEvent(_ context.Context, eventID uuid.UUID) (entity.AcceptedEvent, bool, error) {
	if r == nil {
		return entity.AcceptedEvent{}, false, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	existing, ok := r.events[eventID]
	if !ok {
		return entity.AcceptedEvent{}, false, nil
	}
	existing.RouteDiagnostics = cloneRouteDiagnostics(existing.RouteDiagnostics)
	return existing, true, nil
}

// RegisterAcceptedEvent atomically stores or returns the existing event idempotency record.
func (r *Repository) RegisterAcceptedEvent(_ context.Context, event entity.AcceptedEvent) (entity.AcceptedEvent, bool, error) {
	if r == nil {
		return entity.AcceptedEvent{}, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.events[event.EventID]
	if ok {
		if existing.PayloadDigest != event.PayloadDigest ||
			existing.CorrelationID != event.CorrelationID ||
			existing.HookEventName != event.HookEventName {
			return entity.AcceptedEvent{}, false, hookerrs.ErrDuplicateConflict
		}
		existing.RouteDiagnostics = cloneRouteDiagnostics(existing.RouteDiagnostics)
		return existing, true, nil
	}
	event.RouteDiagnostics = cloneRouteDiagnostics(event.RouteDiagnostics)
	r.events[event.EventID] = event
	return event, false, nil
}

// RecordDeliveryResults stores safe route diagnostics for the accepted event.
func (r *Repository) RecordDeliveryResults(_ context.Context, update entity.DeliveryUpdate) (entity.AcceptedEvent, error) {
	if r == nil {
		return entity.AcceptedEvent{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.events[update.EventID]
	if !ok {
		return entity.AcceptedEvent{}, hookerrs.ErrInvalidArgument
	}
	if existing.PayloadDigest != update.PayloadDigest {
		return entity.AcceptedEvent{}, hookerrs.ErrDuplicateConflict
	}
	existing.Result = update.Result
	existing.RouteDiagnostics = cloneRouteDiagnostics(update.RouteDiagnostics)
	existing.DeliveryCompleted = true
	r.events[update.EventID] = existing
	existing.RouteDiagnostics = cloneRouteDiagnostics(existing.RouteDiagnostics)
	return existing, nil
}

func cloneRouteDiagnostics(diagnostics []value.RouteDeliveryResult) []value.RouteDeliveryResult {
	if len(diagnostics) == 0 {
		return nil
	}
	copied := make([]value.RouteDeliveryResult, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		diagnostic.SafeParts = append([]string(nil), diagnostic.SafeParts...)
		copied = append(copied, diagnostic)
	}
	return copied
}
