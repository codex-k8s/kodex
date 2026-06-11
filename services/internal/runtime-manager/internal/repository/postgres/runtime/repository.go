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

var (
	operationCreateSlot                       = repositoryOperation("CreateSlot")
	operationClaimReusableSlot                = repositoryOperation("ClaimReusableSlot")
	operationCreateCleanupPolicy              = repositoryOperation("CreateCleanupPolicy")
	operationUpdateCleanupPolicy              = repositoryOperation("UpdateCleanupPolicy")
	operationGetCleanupPolicy                 = repositoryOperation("GetCleanupPolicy")
	operationRunCleanupBatch                  = repositoryOperation("RunCleanupBatch")
	operationCreatePrewarmPool                = repositoryOperation("CreatePrewarmPool")
	operationUpdatePrewarmPool                = repositoryOperation("UpdatePrewarmPool")
	operationGetPrewarmPool                   = repositoryOperation("GetPrewarmPool")
	operationReconcilePrewarmPool             = repositoryOperation("ReconcilePrewarmPool")
	operationCreateJob                        = repositoryOperation("CreateJob")
	operationGetCommandResult                 = repositoryOperation("GetCommandResult")
	operationGetJob                           = repositoryOperation("GetJob")
	operationGetRuntimeArtifactRef            = repositoryOperation("GetRuntimeArtifactRef")
	operationGetSlot                          = repositoryOperation("GetSlot")
	operationListSlots                        = repositoryOperation("ListSlots")
	operationListJobs                         = repositoryOperation("ListJobs")
	operationListRuntimeArtifactRefs          = repositoryOperation("ListRuntimeArtifactRefs")
	operationPrepareRuntime                   = repositoryOperation("PrepareRuntime")
	operationClaimRunnableJob                 = repositoryOperation("ClaimRunnableJob")
	operationCreateWorkspaceMaterialization   = repositoryOperation("CreateWorkspaceMaterialization")
	operationPrepareBuildContext              = repositoryOperation("PrepareBuildContext")
	operationUpdateBuildContext               = repositoryOperation("UpdateBuildContext")
	operationGetBuildContext                  = repositoryOperation("GetBuildContext")
	operationGetBuildContextByFingerprint     = repositoryOperation("GetBuildContextByFingerprint")
	operationGetWorkspaceMaterialization      = repositoryOperation("GetWorkspaceMaterialization")
	operationListWorkspaceMaterializations    = repositoryOperation("ListWorkspaceMaterializations")
	operationRecordRuntimeArtifactRef         = repositoryOperation("RecordRuntimeArtifactRef")
	operationUpdateWorkspaceMaterialization   = repositoryOperation("UpdateWorkspaceMaterialization")
	operationUpdateJob                        = repositoryOperation("UpdateJob")
	operationClaimOutboxEvents                = repositoryOperation("ClaimOutboxEvents")
	operationMarkOutboxEventFailed            = repositoryOperation("MarkOutboxEventFailed")
	operationMarkOutboxEventPermanentlyFailed = repositoryOperation("MarkOutboxEventPermanentlyFailed")
	operationMarkOutboxEventPublished         = repositoryOperation("MarkOutboxEventPublished")
	operationPing                             = repositoryOperation("Ping")
	operationUpdateSlot                       = repositoryOperation("UpdateSlot")
)

func repositoryOperation(name string) string {
	return "domain.Repository." + name
}

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

func queryOne[T any](ctx context.Context, db queryer, query string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	rows, err := db.Query(ctx, query, args)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (T, error) {
		return scan(row)
	})
}

func getByID[T any](ctx context.Context, db queryer, id uuid.UUID, query string, operation string, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	value, err := queryOne(ctx, db, query, pgx.NamedArgs{"id": id}, scan)
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	return value, nil
}

func claimOneWithEventAndResult[T any](
	ctx context.Context,
	db database,
	operation string,
	query string,
	args pgx.NamedArgs,
	scan func(postgreslib.RowScanner) (T, error),
	recordFactory func(T) (entity.OutboxEvent, entity.CommandResult, error),
) (T, error) {
	var claimed T
	err := postgreslib.WithTx(ctx, db, func(tx pgx.Tx) error {
		value, err := queryOne(ctx, tx, query, args, scan)
		if err != nil {
			return err
		}
		event, result, err := recordFactory(value)
		if err != nil {
			return err
		}
		if err := insertEventAndCommandResult(ctx, tx, event, result); err != nil {
			return err
		}
		claimed = value
		return nil
	})
	return claimed, wrapError(operation, err)
}

func insertEventAndCommandResult(ctx context.Context, tx pgx.Tx, event entity.OutboxEvent, result entity.CommandResult) error {
	return postgreslib.RunDistinctMutations(
		ctx,
		tx,
		errs.ErrConflict,
		postgreslib.Mutation{Query: queryOutboxEventInsert, Args: outboxEventArgs(event), RequireAffected: true},
		postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
	)
}

func (r *Repository) mutateRecordWithCommandResult(ctx context.Context, operation string, query string, args pgx.NamedArgs, result entity.CommandResult) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(
			ctx,
			tx,
			errs.ErrConflict,
			postgreslib.Mutation{Query: query, Args: args, RequireAffected: true},
			postgreslib.Mutation{Query: queryCommandResultInsert, Args: commandResultArgs(result), RequireAffected: true},
		)
	})
	return wrapError(operation, err)
}
