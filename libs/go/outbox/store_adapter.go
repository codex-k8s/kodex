package outbox

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StoreAdapter converts a service-local outbox entity store to the shared Store contract.
type StoreAdapter[T any] struct {
	store   EntityStore[T]
	convert func(T) Event
}

// NewStoreAdapter creates a shared outbox store adapter around a service-local store.
func NewStoreAdapter[T any](store EntityStore[T], convert func(T) Event) StoreAdapter[T] {
	return StoreAdapter[T]{store: store, convert: convert}
}

// ClaimOutboxEvents claims and converts service-local outbox entities.
func (a StoreAdapter[T]) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]Event, error) {
	events, err := a.store.ClaimOutboxEvents(ctx, limit, now, lockedUntil)
	if err != nil {
		return nil, err
	}
	items := make([]Event, 0, len(events))
	for _, event := range events {
		items = append(items, a.convert(event))
	}
	return items, nil
}

// MarkOutboxEventPublished marks an event as published in the service-local store.
func (a StoreAdapter[T]) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	return a.store.MarkOutboxEventPublished(ctx, id, attemptCount, publishedAt)
}

// MarkOutboxEventFailed schedules retry in the service-local store.
func (a StoreAdapter[T]) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return a.store.MarkOutboxEventFailed(ctx, id, attemptCount, nextAttemptAt, lastError)
}

// MarkOutboxEventPermanentlyFailed marks an event as permanently failed in the service-local store.
func (a StoreAdapter[T]) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return a.store.MarkOutboxEventPermanentlyFailed(ctx, id, attemptCount, failedAt, lastError)
}
