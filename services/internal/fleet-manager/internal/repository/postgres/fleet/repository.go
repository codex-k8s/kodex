// Package fleet implements the PostgreSQL repository for fleet-manager.
package fleet

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	fleetrepo "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/repository/fleet"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
)

// SQLFiles contains named SQL queries for the fleet-manager PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ fleetrepo.Repository = (*Repository)(nil)

type database interface {
	Ping(ctx context.Context) error
	execer
	queryer
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Repository persists fleet-manager aggregates in PostgreSQL.
type Repository struct {
	db database
}

var (
	operationAppendOutboxEvent                = repositoryOperation("AppendOutboxEvent")
	operationClaimOutboxEvents                = repositoryOperation("ClaimOutboxEvents")
	operationMarkOutboxEventFailed            = repositoryOperation("MarkOutboxEventFailed")
	operationMarkOutboxEventPermanentlyFailed = repositoryOperation("MarkOutboxEventPermanentlyFailed")
	operationMarkOutboxEventPublished         = repositoryOperation("MarkOutboxEventPublished")
	operationPing                             = repositoryOperation("Ping")
)

func repositoryOperation(name string) string {
	return "domain.Repository." + name
}

// NewRepository creates a PostgreSQL-backed fleet repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// Ping checks that the fleet database is reachable.
func (r *Repository) Ping(ctx context.Context) error {
	return wrapError(operationPing, r.db.Ping(ctx))
}

// AppendOutboxEvent stores one fleet domain event in the local outbox.
func (r *Repository) AppendOutboxEvent(ctx context.Context, event entity.OutboxEvent) error {
	tag, err := r.db.Exec(ctx, queryOutboxEventInsert, outboxEventArgs(event))
	if err != nil {
		return wrapError(operationAppendOutboxEvent, err)
	}
	if tag.RowsAffected() == 0 {
		return wrapError(operationAppendOutboxEvent, errs.ErrConflict)
	}
	return nil
}

// ClaimOutboxEvents leases unpublished outbox events for delivery.
func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	args, ok := postgreslib.OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, wrapError(operationClaimOutboxEvents, errs.ErrInvalidArgument)
	}
	return r.claimOutboxEventsWithArgs(ctx, args)
}

func (r *Repository) claimOutboxEventsWithArgs(ctx context.Context, args pgx.NamedArgs) ([]entity.OutboxEvent, error) {
	rows, err := r.db.Query(ctx, queryOutboxEventClaim, args)
	if err != nil {
		return nil, wrapError(operationClaimOutboxEvents, err)
	}
	events, err := postgreslib.ScanRows(rows, scanOutboxEvent)
	if err != nil {
		return nil, wrapError(operationClaimOutboxEvents, err)
	}
	return events, nil
}

// MarkOutboxEventPublished marks a leased outbox event as published.
func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	return r.finishPublishedOutboxEvent(ctx, outboxPublishedMutation{id: id, attempt: attemptCount, at: publishedAt})
}

// MarkOutboxEventFailed schedules a leased outbox event for retry.
func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.finishFailedOutboxEvent(ctx, operationMarkOutboxEventFailed, queryOutboxEventMarkFailed, "next_attempt_at", id, attemptCount, nextAttemptAt, lastError)
}

// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.finishFailedOutboxEvent(ctx, operationMarkOutboxEventPermanentlyFailed, queryOutboxEventMarkPermanentlyFailed, "failed_permanently_at", id, attemptCount, failedAt, lastError)
}

type outboxPublishedMutation struct {
	id      uuid.UUID
	attempt int
	at      time.Time
}

func (r *Repository) finishPublishedOutboxEvent(ctx context.Context, mutation outboxPublishedMutation) error {
	err := postgreslib.ApplyOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, errs.ErrInvalidArgument, mutation.id, mutation.attempt, mutation.at)
	return wrapError(operationMarkOutboxEventPublished, err)
}

func (r *Repository) finishFailedOutboxEvent(ctx context.Context, operation string, query string, timestampField string, id uuid.UUID, attempt int, at time.Time, message string) error {
	err := postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, query, errs.ErrInvalidArgument, id, attempt, timestampField, at, message)
	return wrapError(operation, err)
}
