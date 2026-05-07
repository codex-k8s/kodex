// Package runtime defines persistence ports owned by the runtime-manager domain.
package runtime

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

// Repository is the domain persistence contract for runtime-manager use cases.
type Repository interface {
	// Ping checks that the runtime database is reachable.
	Ping(ctx context.Context) error
	// GetCommandResult returns the aggregate linked to an idempotent command.
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	// CreateSlot stores a new slot, its command result and its outbox event atomically.
	CreateSlot(ctx context.Context, slot entity.Slot, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateSlot stores an existing slot mutation, its outbox event and optional command result atomically.
	UpdateSlot(ctx context.Context, slot entity.Slot, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetSlot returns one slot by id.
	GetSlot(ctx context.Context, id uuid.UUID) (entity.Slot, error)
	// ListSlots returns slots matching the filter and page.
	ListSlots(ctx context.Context, filter query.SlotFilter) ([]entity.Slot, query.PageResult, error)
	// ClaimOutboxEvents leases unpublished outbox events for delivery.
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	// MarkOutboxEventPublished marks a leased outbox event as published.
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	// MarkOutboxEventFailed schedules a leased outbox event for retry.
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}

// Clock provides deterministic time for domain commands and tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides aggregate and event identifiers for domain commands.
type IDGenerator interface {
	New() uuid.UUID
}
