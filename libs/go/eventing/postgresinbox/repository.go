package postgresinbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	sqlStateSerializationFailure = "40001"
	sqlStateDeadlockDetected     = "40P01"
	sqlStateUniqueViolation      = "23505"
)

type cursorRow struct {
	LastSequence    uint64
	LastEventID     *string
	LastEventDigest []byte
	NextFence       uint64
}

type inboxRow struct {
	EventID         string
	EventDigest     []byte
	OrderingKey     string
	EventSequence   uint64
	State           string
	Attempts        uint32
	MaxAttempts     uint32
	RepairCount     uint32
	MaxRepairs      uint32
	LeaseOwner      *string
	LeaseToken      *string
	LeaseGeneration uint64
	LeaseFence      uint64
	LeaseExpiresAt  *time.Time
	AvailableAt     time.Time
	LastErrorCode   *string
	ProcessedAt     *time.Time
	CleanupAfter    *time.Time
	TerminalAt      *time.Time
	AvailableNow    bool
	LeaseActive     bool
}

func (processor *Processor) transact(
	ctx context.Context,
	operation func(pgx.Tx) error,
) (err error) {
	tx, beginErr := processor.beginner.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable,
	})
	if beginErr != nil {
		return wrapSafe(errorTextTransactionBegin, beginErr)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			processor.config.FinalizeTimeout,
		)
		defer cancel()
		rollbackErr := tx.Rollback(rollbackCtx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			rollbackErr = wrapSafe(errorTextTransactionRollback, rollbackErr)
			if err == nil {
				err = rollbackErr
			} else {
				err = errors.Join(err, rollbackErr)
			}
		}
	}()

	if err = processor.setSearchPath(ctx, tx); err != nil {
		return err
	}
	if err = operation(tx); err != nil {
		return err
	}
	commitCtx, cancelCommit := context.WithTimeout(
		context.WithoutCancel(ctx),
		processor.config.FinalizeTimeout,
	)
	defer cancelCommit()
	if commitErr := tx.Commit(commitCtx); commitErr != nil {
		return wrapSafe(errorTextTransactionCommit, commitErr)
	}
	committed = true
	return nil
}

func (processor *Processor) retryTransaction(
	ctx context.Context,
	operation func(pgx.Tx) error,
) error {
	var err error
	for attempt := 0; attempt < maximumTransactionRetries; attempt++ {
		err = processor.transact(ctx, operation)
		if !isRetryableTransactionError(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func (processor *Processor) setSearchPath(ctx context.Context, tx pgx.Tx) error {
	var applied string
	var identityStable bool
	err := tx.QueryRow(
		ctx,
		processor.queries.schemaSetSearchPath,
		pgx.StrictNamedArgs{"schema_name": processor.config.Schema},
	).Scan(&applied, &identityStable)
	if err != nil {
		return wrapSafe(errorTextDatabaseOperation, err)
	}
	if applied != "pg_catalog,"+processor.config.Schema+",pg_temp" ||
		!identityStable {
		return ErrSchemaMismatch
	}
	return nil
}

func (processor *Processor) ensureAndLockCursor(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	orderingKey string,
) (cursorRow, error) {
	arguments := pgx.StrictNamedArgs{
		"consumer_name":  consumer.Name,
		"consumer_scope": consumer.Scope,
		"ordering_key":   orderingKey,
	}
	if _, err := tx.Exec(ctx, processor.queries.cursorEnsure, arguments); err != nil {
		return cursorRow{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	var row cursorRow
	err := tx.QueryRow(ctx, processor.queries.cursorGetForUpdate, arguments).Scan(
		&row.LastSequence,
		&row.LastEventID,
		&row.LastEventDigest,
		&row.NextFence,
	)
	if err != nil {
		return cursorRow{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	return row, nil
}

func (processor *Processor) getInboxByEvent(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	eventID string,
) (inboxRow, error) {
	return processor.getInbox(ctx, tx, processor.queries.inboxGetByEventForUpdate, consumer, eventID)
}

func (processor *Processor) readInboxByEvent(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	eventID string,
) (inboxRow, error) {
	return processor.getInbox(ctx, tx, processor.queries.inboxGetByEvent, consumer, eventID)
}

func (processor *Processor) getInbox(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	consumer Consumer,
	eventID string,
) (inboxRow, error) {
	row := inboxRow{}
	err := tx.QueryRow(
		ctx,
		query,
		pgx.StrictNamedArgs{
			"consumer_name":  consumer.Name,
			"consumer_scope": consumer.Scope,
			"event_id":       eventID,
		},
	).Scan(
		&row.EventID,
		&row.EventDigest,
		&row.OrderingKey,
		&row.EventSequence,
		&row.State,
		&row.Attempts,
		&row.MaxAttempts,
		&row.RepairCount,
		&row.MaxRepairs,
		&row.LeaseOwner,
		&row.LeaseToken,
		&row.LeaseGeneration,
		&row.LeaseFence,
		&row.LeaseExpiresAt,
		&row.AvailableAt,
		&row.LastErrorCode,
		&row.ProcessedAt,
		&row.CleanupAfter,
		&row.TerminalAt,
		&row.AvailableNow,
		&row.LeaseActive,
	)
	return row, err
}

func (processor *Processor) getInboxBySequence(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	orderingKey string,
	sequence uint64,
) (inboxRow, error) {
	row := inboxRow{}
	err := tx.QueryRow(
		ctx,
		processor.queries.inboxGetBySequenceForUpdate,
		pgx.StrictNamedArgs{
			"consumer_name":  consumer.Name,
			"consumer_scope": consumer.Scope,
			"ordering_key":   orderingKey,
			"event_sequence": sequence,
		},
	).Scan(
		&row.EventID,
		&row.EventDigest,
		&row.OrderingKey,
		&row.EventSequence,
		&row.State,
		&row.Attempts,
		&row.MaxAttempts,
		&row.RepairCount,
		&row.MaxRepairs,
		&row.LeaseOwner,
		&row.LeaseToken,
		&row.LeaseGeneration,
		&row.LeaseFence,
		&row.LeaseExpiresAt,
		&row.AvailableAt,
		&row.LastErrorCode,
		&row.ProcessedAt,
		&row.CleanupAfter,
		&row.TerminalAt,
		&row.AvailableNow,
		&row.LeaseActive,
	)
	return row, err
}

func isRetryableTransactionError(err error) bool {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return false
	}
	return pgError.Code == sqlStateSerializationFailure ||
		pgError.Code == sqlStateDeadlockDetected
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == sqlStateUniqueViolation
}
