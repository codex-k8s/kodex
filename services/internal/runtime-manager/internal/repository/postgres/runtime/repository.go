// Package runtime implements the PostgreSQL repository for runtime-manager.
package runtime

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
)

// SQLFiles contains named SQL queries for the runtime-manager PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ runtimerepo.Repository = (*Repository)(nil)

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

// Repository persists runtime-manager aggregates in PostgreSQL.
type Repository struct {
	db database
}

const (
	operationClaimOutboxEvents                = "domain.Repository.ClaimOutboxEvents"
	operationMarkOutboxEventFailed            = "domain.Repository.MarkOutboxEventFailed"
	operationMarkOutboxEventPermanentlyFailed = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationMarkOutboxEventPublished         = "domain.Repository.MarkOutboxEventPublished"
	operationPing                             = "domain.Repository.Ping"
)

// NewRepository creates a PostgreSQL-backed runtime repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// Ping checks that the runtime database is reachable.
func (r *Repository) Ping(ctx context.Context) error {
	return wrapError(operationPing, r.db.Ping(ctx))
}

// ClaimOutboxEvents leases unpublished outbox events for delivery.
func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	args, ok := postgreslib.OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, wrapError(operationClaimOutboxEvents, errs.ErrInvalidArgument)
	}
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
	ok, err := postgreslib.ExecOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, id, attemptCount, publishedAt)
	if ok {
		return wrapError(operationMarkOutboxEventPublished, err)
	}
	return wrapError(operationMarkOutboxEventPublished, errs.ErrInvalidArgument)
}

// MarkOutboxEventFailed schedules a leased outbox event for retry.
func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationMarkOutboxEventFailed, queryOutboxEventMarkFailed, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError)
}

// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationMarkOutboxEventPermanentlyFailed, queryOutboxEventMarkPermanentlyFailed, id, attemptCount, "failed_permanently_at", failedAt, lastError)
}

func (r *Repository) markOutboxFailure(ctx context.Context, operation string, queryText string, id uuid.UUID, attempts int, timestampColumn string, timestamp time.Time, message string) error {
	ok, err := postgreslib.ExecOutboxDeliveryFailure(ctx, r.db, queryText, id, attempts, timestampColumn, timestamp, message)
	if ok {
		return wrapError(operation, err)
	}
	return wrapError(operation, errs.ErrInvalidArgument)
}
