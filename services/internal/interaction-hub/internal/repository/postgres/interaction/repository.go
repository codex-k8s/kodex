// Package interaction implements the PostgreSQL repository for interaction-hub.
package interaction

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionrepo "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/repository/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

// SQLFiles contains named SQL queries for interaction-hub repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ interactionrepo.Repository = (*Repository)(nil)

type database interface {
	execQuerier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db database
}

const (
	operationBacklog                   = "domain.Repository.RecordBacklogOperation"
	operationCreateConversationThread  = "domain.Repository.CreateConversationThreadWithResult"
	operationCreateConversationMessage = "domain.Repository.CreateConversationMessageWithResult"
	operationGetCommandResult          = "domain.Repository.GetCommandResult"
	operationGetConversationMessage    = "domain.Repository.GetConversationMessage"
	operationGetConversationThread     = "domain.Repository.GetConversationThread"
	operationListConversationMessages  = "domain.Repository.ListConversationMessages"
	operationOutboxClaim               = "domain.Repository.ClaimOutboxEvents"
	operationOutboxMarkFailed          = "domain.Repository.MarkOutboxEventFailed"
	operationOutboxMarkPermanent       = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationOutboxMarkPublished       = "domain.Repository.MarkOutboxEventPublished"
)

// NewRepository creates an interaction-hub PostgreSQL repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Ready() bool {
	return r != nil && r.db != nil
}

func (r *Repository) RecordBacklogOperation(_ context.Context, operation enum.Operation) error {
	if !operation.Valid() {
		return wrapError(operationBacklog, errs.ErrInvalidArgument)
	}
	return nil
}

func (r *Repository) CreateConversationThreadWithResult(ctx context.Context, thread entity.ConversationThread, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateConversationThread,
		affectedMutation(queryThreadCreate, threadArgs(thread)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetConversationThread(ctx context.Context, id uuid.UUID) (entity.ConversationThread, error) {
	return queryOne(ctx, r.db, operationGetConversationThread, queryThreadGet, pgx.NamedArgs{"id": id}, scanThread)
}

func (r *Repository) CreateConversationMessageWithResult(ctx context.Context, message entity.ConversationMessage, thread entity.ConversationThread, previousThreadVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operationCreateConversationMessage,
		affectedMutation(queryMessageCreate, messageArgs(message)),
		affectedMutation(queryThreadUpdateLatestMessage, threadLatestMessageArgs(thread, previousThreadVersion)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) GetConversationMessage(ctx context.Context, id uuid.UUID) (entity.ConversationMessage, error) {
	return queryOne(ctx, r.db, operationGetConversationMessage, queryMessageGet, pgx.NamedArgs{"id": id}, scanMessage)
}

func (r *Repository) ListConversationMessages(ctx context.Context, filter query.ConversationMessageFilter) ([]entity.ConversationMessage, value.PageResult, error) {
	args := messageFilterArgs(filter)
	items, err := queryAll(ctx, r.db, operationListConversationMessages, queryMessageList, args.NamedArgs, scanMessage)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	pageItems, page := pageFromItems(items, args)
	return pageItems, page, nil
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	events, ok, err := postgreslib.ClaimOutboxRows(ctx, r.db, queryOutboxEventClaim, limit, now, lockedUntil, scanOutboxEvent)
	if !ok {
		return nil, wrapError(operationOutboxClaim, errs.ErrInvalidArgument)
	}
	return events, wrapError(operationOutboxClaim, err)
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	err := postgreslib.ApplyOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, errs.ErrInvalidArgument, id, attemptCount, publishedAt)
	return wrapError(operationOutboxMarkPublished, err)
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationOutboxMarkFailed, queryOutboxEventMarkFailed, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError)
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationOutboxMarkPermanent, queryOutboxEventMarkPermanent, id, attemptCount, "failed_permanently_at", failedAt, lastError)
}

func (r *Repository) markOutboxFailure(ctx context.Context, operation string, queryText string, id uuid.UUID, attemptCount int, timestampName string, timestamp time.Time, lastError string) error {
	err := postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, queryText, errs.ErrInvalidArgument, id, attemptCount, timestampName, timestamp, lastError)
	return wrapError(operation, err)
}

type mutation = postgreslib.Mutation

func (r *Repository) mutate(ctx context.Context, operation string, mutations ...mutation) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
	return wrapError(operation, err)
}

func affectedMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args, RequireAffected: true}
}

func commandResultMutation(result entity.CommandResult) mutation {
	return affectedMutation(queryCommandResultCreate, commandResultArgs(result))
}

func outboxEventMutation(event entity.OutboxEvent) mutation {
	return affectedMutation(queryOutboxEventCreate, outboxEventArgs(event))
}

func queryOne[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	value, err := scan(db.QueryRow(ctx, queryText, args))
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	return value, nil
}

func queryAll[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, queryText, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	return items, nil
}
