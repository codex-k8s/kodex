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
	postgreslib.TxBeginner
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
	operationCreateSlot                       = "domain.Repository.CreateSlot"
	operationGetCommandResult                 = "domain.Repository.GetCommandResult"
	operationGetSlot                          = "domain.Repository.GetSlot"
	operationListSlots                        = "domain.Repository.ListSlots"
	operationClaimOutboxEvents                = "domain.Repository.ClaimOutboxEvents"
	operationMarkOutboxEventFailed            = "domain.Repository.MarkOutboxEventFailed"
	operationMarkOutboxEventPermanentlyFailed = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationMarkOutboxEventPublished         = "domain.Repository.MarkOutboxEventPublished"
	operationPing                             = "domain.Repository.Ping"
	operationUpdateSlot                       = "domain.Repository.UpdateSlot"
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
	return r.finishPublishedOutboxEvent(ctx, outboxPublishedMutation{id: id, attempt: attemptCount, at: publishedAt})
}

// MarkOutboxEventFailed schedules a leased outbox event for retry.
func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.finishFailedOutboxEvent(ctx, newOutboxFailure(operationMarkOutboxEventFailed, queryOutboxEventMarkFailed, "next_attempt_at", id, attemptCount, nextAttemptAt, lastError))
}

// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.finishFailedOutboxEvent(ctx, newOutboxFailure(operationMarkOutboxEventPermanentlyFailed, queryOutboxEventMarkPermanentlyFailed, "failed_permanently_at", id, attemptCount, failedAt, lastError))
}

type outboxPublishedMutation struct {
	id      uuid.UUID
	attempt int
	at      time.Time
}

func (r *Repository) finishPublishedOutboxEvent(ctx context.Context, mutation outboxPublishedMutation) error {
	matched, err := postgreslib.ExecOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, mutation.id, mutation.attempt, mutation.at)
	return r.wrapOutboxMutation(operationMarkOutboxEventPublished, matched, err)
}

type outboxFailureMutation struct {
	operation      string
	query          string
	id             uuid.UUID
	attempt        int
	timestampField string
	at             time.Time
	message        string
}

func newOutboxFailure(operation string, query string, timestampField string, id uuid.UUID, attempt int, at time.Time, message string) outboxFailureMutation {
	return outboxFailureMutation{
		operation:      operation,
		query:          query,
		id:             id,
		attempt:        attempt,
		timestampField: timestampField,
		at:             at,
		message:        message,
	}
}

func (r *Repository) finishFailedOutboxEvent(ctx context.Context, mutation outboxFailureMutation) error {
	matched, err := postgreslib.ExecOutboxDeliveryFailure(ctx, r.db, mutation.query, mutation.id, mutation.attempt, mutation.timestampField, mutation.at, mutation.message)
	return r.wrapOutboxMutation(mutation.operation, matched, err)
}

func (r *Repository) wrapOutboxMutation(operation string, matched bool, err error) error {
	if !matched {
		err = errs.ErrInvalidArgument
	}
	return wrapError(operation, err)
}
