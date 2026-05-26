// Package hook provides a safe in-memory repository stub for CHI-3.
package hook

import (
	"context"
	"sync"

	"github.com/google/uuid"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookrepo "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/repository/hook"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
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

// RegisterAcceptedEvent atomically stores or returns the existing event idempotency record.
func (r *Repository) RegisterAcceptedEvent(_ context.Context, event entity.AcceptedEvent) (entity.AcceptedEvent, bool, error) {
	if r == nil {
		return entity.AcceptedEvent{}, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.events[event.EventID]
	if ok {
		if existing.PayloadDigest != event.PayloadDigest {
			return entity.AcceptedEvent{}, false, hookerrs.ErrDuplicateConflict
		}
		return existing, true, nil
	}
	r.events[event.EventID] = event
	return event, false, nil
}
