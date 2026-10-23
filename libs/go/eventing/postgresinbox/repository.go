package postgresinbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// sqlStateSerializationFailure permits a bounded retry of a rolled-back serializable transaction.
	// sqlStateSerializationFailure допускает ограниченный повтор откатанной serializable-транзакции.
	sqlStateSerializationFailure = "40001"
	// sqlStateDeadlockDetected permits a bounded retry after PostgreSQL aborts a lock cycle.
	// sqlStateDeadlockDetected допускает ограниченный повтор после остановки PostgreSQL цикла блокировок.
	sqlStateDeadlockDetected = "40P01"
	// sqlStateUniqueViolation identifies a competing insert for explicit receive or receipt replay.
	// sqlStateUniqueViolation обозначает конкурирующую вставку для явного повтора приёма или квитанции.
	sqlStateUniqueViolation = "23505"
)

// mutationLockResource names the two row classes in the canonical mutation lock order.
// mutationLockResource обозначает два класса строк в каноническом порядке блокировок изменения.
type mutationLockResource uint8

const (
	// mutationLockCursor requires the ordering cursor to be locked before its inbox row.
	// mutationLockCursor требует блокировать курсор порядка перед строкой inbox.
	mutationLockCursor mutationLockResource = iota + 1
	// mutationLockInbox is the second lock after the owning ordering cursor.
	// mutationLockInbox — вторая блокировка после принадлежащего событию курсора.
	mutationLockInbox
)

// canonicalMutationLockOrder pins the cursor-before-inbox order for multi-row mutations.
// canonicalMutationLockOrder закрепляет блокировку курсора перед inbox для изменений нескольких строк.
var canonicalMutationLockOrder = [...]mutationLockResource{
	mutationLockCursor,
	mutationLockInbox,
}

// cursorRow carries the locked ordering watermark and next lease fence.
// cursorRow содержит заблокированный watermark порядка и следующий fence аренды.
type cursorRow struct {
	LastSequence    uint64
	LastEventID     *string
	LastEventDigest []byte
	NextFence       uint64
}

// inboxRow is the canonical durable event projection, including lease and retry evidence.
// inboxRow — каноническая устойчивая проекция события с данными аренды и повтора.
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

// transact runs one serializable operation with pinned search path and independently bounded finalization.
// transact выполняет операцию serializable с закреплённым search path и отдельно ограниченным завершением.
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

// retryTransaction retries only serialization or deadlock failures within the fixed attempt budget.
// retryTransaction повторяет только ошибки сериализации или deadlock в фиксированном лимите попыток.
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

// setSearchPath verifies the exact schema and stable runtime identity inside the active transaction.
// setSearchPath проверяет точную схему и устойчивую runtime-идентичность внутри действующей транзакции.
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

// ensureAndLockCursor creates an absent ordering cursor and locks it before reading its watermark.
// ensureAndLockCursor создаёт отсутствующий курсор порядка и блокирует его до чтения watermark.
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

// getInboxByEvent locks the exact event row for a mutation under its consumer scope.
// getInboxByEvent блокирует точную строку события для изменения в области потребителя.
func (processor *Processor) getInboxByEvent(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	eventID string,
) (inboxRow, error) {
	return processor.getInbox(ctx, tx, processor.queries.inboxGetByEventForUpdate, consumer, eventID)
}

// readInboxByEvent reads immutable event identity before cursor-first mutation locks.
// readInboxByEvent читает неизменную идентичность события до блокировок изменения по порядку курсора.
func (processor *Processor) readInboxByEvent(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	eventID string,
) (inboxRow, error) {
	return processor.getInbox(ctx, tx, processor.queries.inboxGetByEvent, consumer, eventID)
}

// lockCursorThenInbox reads immutable identity, locks cursor then inbox, and rejects changed evidence.
// lockCursorThenInbox читает неизменную идентичность, блокирует курсор и inbox, затем отклоняет расхождение данных.
func (processor *Processor) lockCursorThenInbox(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	eventID string,
) (cursorRow, inboxRow, error) {
	preRead, err := processor.readInboxByEvent(ctx, tx, consumer, eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cursorRow{}, inboxRow{}, err
		}
		return cursorRow{}, inboxRow{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	cursor, err := processor.ensureAndLockCursor(ctx, tx, consumer, preRead.OrderingKey)
	if err != nil {
		return cursorRow{}, inboxRow{}, err
	}
	row, err := processor.getInboxByEvent(ctx, tx, consumer, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return cursorRow{}, inboxRow{}, ErrStaleClaim
	}
	if err != nil {
		return cursorRow{}, inboxRow{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	if row.EventID != preRead.EventID ||
		!sameDigest(row.EventDigest, preRead.EventDigest) ||
		!sameOrderingKey(row.OrderingKey, preRead.OrderingKey) ||
		row.EventSequence != preRead.EventSequence {
		return cursorRow{}, inboxRow{}, ErrStaleClaim
	}
	return cursor, row, nil
}

// getInbox binds exact consumer and event coordinates to either locked or read-only SQL.
// getInbox привязывает точные координаты потребителя и события к блокирующему либо читающему SQL.
func (processor *Processor) getInbox(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	consumer Consumer,
	eventID string,
) (inboxRow, error) {
	return scanInboxRow(tx.QueryRow(
		ctx,
		query,
		pgx.StrictNamedArgs{
			"consumer_name":  consumer.Name,
			"consumer_scope": consumer.Scope,
			"event_id":       eventID,
		},
	))
}

// scanInboxRow decodes the canonical inbox projection in SQL column order without changing QueryRow's error semantics.
// scanInboxRow разбирает каноническую проекцию inbox в порядке SQL-столбцов, сохраняя семантику ошибок QueryRow.
func scanInboxRow(source pgx.Row) (inboxRow, error) {
	row := inboxRow{}
	err := source.Scan(
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

// getInboxBySequence locks one event under an ordering key and sequence to detect competing identity.
// getInboxBySequence блокирует событие по ключу порядка и номеру для выявления конкурирующей идентичности.
func (processor *Processor) getInboxBySequence(
	ctx context.Context,
	tx pgx.Tx,
	consumer Consumer,
	orderingKey string,
	sequence uint64,
) (inboxRow, error) {
	return scanInboxRow(tx.QueryRow(
		ctx,
		processor.queries.inboxGetBySequenceForUpdate,
		pgx.StrictNamedArgs{
			"consumer_name":  consumer.Name,
			"consumer_scope": consumer.Scope,
			"ordering_key":   orderingKey,
			"event_sequence": sequence,
		},
	))
}

// isRetryableTransactionError recognizes only PostgreSQL serialization and deadlock abort codes.
// isRetryableTransactionError распознаёт только коды отмены PostgreSQL при сериализации и deadlock.
func isRetryableTransactionError(err error) bool {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return false
	}
	return pgError.Code == sqlStateSerializationFailure ||
		pgError.Code == sqlStateDeadlockDetected
}

// isUniqueViolation distinguishes a competing insert from other storage failures.
// isUniqueViolation отличает конкурирующую вставку от прочих ошибок хранилища.
func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == sqlStateUniqueViolation
}
