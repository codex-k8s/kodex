// Package fleet defines persistence ports owned by the fleet-manager domain.
package fleet

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
)

// Repository is the domain persistence contract for fleet-manager infrastructure state.
type Repository interface {
	// Ping checks that the fleet database is reachable.
	Ping(ctx context.Context) error
	// AppendOutboxEvent stores one fleet domain event in the local outbox.
	AppendOutboxEvent(ctx context.Context, event entity.OutboxEvent) error
	// ClaimOutboxEvents leases unpublished outbox events for delivery.
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	// MarkOutboxEventPublished marks a leased outbox event as published.
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	// MarkOutboxEventFailed schedules a leased outbox event for retry.
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}
