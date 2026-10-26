package platform

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// bootstrapTransactionPhase is the fixed diagnostic boundary for one whole owner bootstrap.
// bootstrapTransactionPhase задаёт фиксированную границу диагностики целого owner bootstrap.
const bootstrapTransactionPhase = "bootstrap transaction"

// bootstrapRollbackTimeout bounds cleanup independently from an exhausted caller deadline;
// it does not permit another bootstrap attempt after caller cancellation.
// bootstrapRollbackTimeout ограничивает cleanup независимо от истёкшего caller deadline,
// не разрешая новую bootstrap попытку после отмены вызывающего.
const bootstrapRollbackTimeout = time.Second

// errBootstrapTerminal prevents a nested helper retry sentinel from bypassing the cleanup
// decision; the original failure remains private in retryBootstrap for the final result.
// errBootstrapTerminal не даёт retry sentinel вложенного helper обойти cleanup решение;
// исходная ошибка остаётся приватной в retryBootstrap для итогового результата.
var errBootstrapTerminal = errors.New("bootstrap transaction terminal")

// bootstrapAttemptFailure records a completed attempt and its narrowly proven retry permission.
// Retry permission is assigned only after cleanup, never by arbitrary caller error text.
// bootstrapAttemptFailure хранит исход завершённой попытки и доказанный допуск повтора.
// Допуск назначается лишь после cleanup, а не по произвольному тексту ошибки.
type bootstrapAttemptFailure struct {
	cause error
	retry bool
}

// Error exposes only a fixed phase even when a nested legacy helper formatted a raw cause.
// Error раскрывает лишь фиксированный этап даже при сырой причине в legacy helper.
func (failure *bootstrapAttemptFailure) Error() string { return bootstrapTransactionPhase }

// Unwrap retains original driver, cancellation and cleanup causes privately.
// Unwrap приватно сохраняет исходные driver, cancellation и cleanup причины.
func (failure *bootstrapAttemptFailure) Unwrap() error { return failure.cause }

// bootstrapTx observes SQL failures only inside Bootstrap, retaining private causes even when
// shared owner helpers map them to domain sentinels. It never changes statements or authority.
// bootstrapTx наблюдает SQL failures лишь внутри Bootstrap, сохраняя приватные причины даже
// после domain mapping общих owner helpers; statements и authority не меняются.
type bootstrapTx struct {
	pgx.Tx
	cause         error
	commitStarted bool
	commitError   error
}

// capture records the first actual failure; ordinary not-found reads remain valid fallback inputs.
// capture записывает первый настоящий отказ; обычный not-found остаётся допустимым fallback input.
func (tx *bootstrapTx) capture(err error) {
	if tx.cause == nil && err != nil && !errors.Is(err, pgx.ErrNoRows) {
		tx.cause = err
	}
}

// Exec preserves the original command tag/error and records no query text or argument values.
// Exec сохраняет исходные command tag/error без записи query text и argument values.
func (tx *bootstrapTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	tx.capture(err)
	return tag, err
}

// QueryRow observes the eventual Scan failure without consuming or changing the result.
// QueryRow наблюдает будущий Scan failure без потребления или изменения результата.
func (tx *bootstrapTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return bootstrapRow{Row: tx.Tx.QueryRow(ctx, sql, args...), tx: tx}
}

// Query preserves streaming rows and records immediate and later iteration failures.
// Query сохраняет streaming rows и записывает немедленные и последующие iteration failures.
func (tx *bootstrapTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	tx.capture(err)
	if err != nil {
		return rows, err
	}
	return bootstrapRows{Rows: rows, tx: tx}, nil
}

// Commit records its own exact outcome, preventing an earlier driver failure from authorizing
// retry after an unknown commit result. A PostgreSQL 40001/40P01 response is a known abort.
// Commit сохраняет собственный exact outcome, не позволяя прежнему driver failure разрешить
// повтор после неизвестного commit; ответ PostgreSQL 40001/40P01 является известным abort.
func (tx *bootstrapTx) Commit(ctx context.Context) error {
	tx.commitStarted = true
	tx.commitError = tx.Tx.Commit(ctx)
	tx.capture(tx.commitError)
	return tx.commitError
}

// bootstrapRow retains the transaction observer until QueryRow's deferred Scan completes.
// bootstrapRow удерживает transaction observer до отложенного Scan у QueryRow.
type bootstrapRow struct {
	pgx.Row
	tx *bootstrapTx
}

// Scan returns the original scan error while preserving its private driver cause for startup.
// Scan возвращает исходный scan error, сохраняя приватную driver cause для startup.
func (row bootstrapRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	row.tx.capture(err)
	return err
}

// bootstrapRows observes iteration/scan failure while delegating all other pgx row behavior.
// bootstrapRows наблюдает iteration/scan failure, делегируя остальное поведение rows в pgx.
type bootstrapRows struct {
	pgx.Rows
	tx *bootstrapTx
}

// Next captures a terminal iteration error before a helper maps it to a domain sentinel.
// Next сохраняет terminal iteration error до domain mapping внутри helper.
func (rows bootstrapRows) Next() bool {
	ok := rows.Rows.Next()
	if !ok {
		rows.tx.capture(rows.Rows.Err())
	}
	return ok
}

// Scan preserves a row-level decoder failure without exposing values.
// Scan сохраняет row-level decoder failure без раскрытия значений.
func (rows bootstrapRows) Scan(dest ...any) error {
	err := rows.Rows.Scan(dest...)
	rows.tx.capture(err)
	return err
}

// Err records a late driver failure even when a helper reads it without calling Next again.
// Err сохраняет поздний driver failure даже при чтении без повторного вызова Next.
func (rows bootstrapRows) Err() error { err := rows.Rows.Err(); rows.tx.capture(err); return err }

// Close observes any terminal row failure before releasing the delegated driver rows.
// Close сохраняет terminal row failure после закрытия делегированных driver rows.
func (rows bootstrapRows) Close() { rows.Rows.Close(); rows.tx.capture(rows.Rows.Err()) }

// finishBootstrapAttempt confirms rollback or a typed server abort before assigning retry;
// unknown commit/cleanup errors stay terminal, even when an earlier statement had 40001.
// finishBootstrapAttempt подтверждает rollback либо типизированный server abort до допуска
// повтора; неизвестные commit/cleanup ошибки terminal даже при прежнем40001.
func finishBootstrapAttempt(ctx context.Context, tx *bootstrapTx, failure *error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), bootstrapRollbackTimeout)
	defer cancel()
	rollback := tx.Rollback(cleanup)
	if *failure == nil && rollback != nil && !errors.Is(rollback, pgx.ErrTxClosed) {
		*failure = bootstrapFailure("rollback bootstrap transaction", rollback)
	}
	if *failure == nil {
		return
	}
	cause := errors.Join(*failure, tx.cause)
	clean := rollback == nil || errors.Is(rollback, pgx.ErrTxClosed) && tx.commitStarted && serializableTransactionConflict(tx.commitError)
	if rollback != nil && !errors.Is(rollback, pgx.ErrTxClosed) {
		cause = errors.Join(cause, bootstrapFailure("rollback bootstrap transaction", rollback))
	}
	retry := clean && ctx.Err() == nil && serializableTransactionConflict(tx.cause) && (!tx.commitStarted || serializableTransactionConflict(tx.commitError))
	*failure = &bootstrapAttemptFailure{cause: cause, retry: retry}
}

// retryBootstrap reuses the existing three-attempt transaction policy without replacing the
// final private cause. The caller startup context controls every attempt and bounded pause.
// retryBootstrap использует прежнюю политику трёх transaction attempts без замены последней
// приватной причины; caller startup context ограничивает каждую попытку и паузу.
func retryBootstrap(ctx context.Context, operation func(context.Context) error) error {
	var last error
	_, err := retrySerializableTransaction(ctx, func() (struct{}, error) {
		if ctx.Err() != nil {
			last = bootstrapFailure(bootstrapTransactionPhase, ctx.Err())
			return struct{}{}, last
		}
		last = operation(ctx)
		var failure *bootstrapAttemptFailure
		if errors.As(last, &failure) && failure.retry {
			return struct{}{}, errors.Join(errSerializableTransactionRetry, last)
		}
		if last != nil {
			return struct{}{}, errBootstrapTerminal
		}
		return struct{}{}, nil
	})
	if err == nil {
		return nil
	}
	return bootstrapFailure(bootstrapTransactionPhase, errors.Join(last, ctx.Err()))
}
