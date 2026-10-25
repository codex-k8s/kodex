package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// serializableTransactionAttempts caps the shared marked-transaction loop at three total attempts,
// including the first call. It does not define the separate artifact READ retry budget or permit
// replay of external effects.
// serializableTransactionAttempts ограничивает shared marked-transaction loop тремя total
// attempts, включая первый call. Не задаёт отдельный artifact READ retry budget и не разрешает
// replay external effects.
const serializableTransactionAttempts = 3

// errSerializableTransactionRetry marks a caller-classified retryable transaction failure without
// retaining driver SQL text. Only explicit classification may add it; the loop itself cannot
// establish rollback safety or external-effect idempotency.
// errSerializableTransactionRetry помечает classified caller retryable transaction failure без
// сохранения driver SQL text. Добавляется только явной классификацией; сам loop не доказывает
// rollback safety либо external-effect idempotency.
var errSerializableTransactionRetry = errors.New("retry serializable transaction")

// retrySerializableTransaction runs at most three operations, returning success directly and zero
// result on any error. Only marked errors retry with five/ten-millisecond cancellable waits;
// exhaustion or cancellation after a marked failure is Unavailable. The first operation is invoked
// without a context precheck, and callers must finish rollback/cleanup and avoid unsafe external
// replay themselves.
// retrySerializableTransaction выполняет не более трёх operations, возвращая success напрямую и
// zero result при любой error. Только marked errors повторяются с отменяемыми waits пять/десять
// миллисекунд; exhaustion либо cancellation после marked failure дают Unavailable. Первый
// operation вызывается без context precheck; caller сам завершает rollback/cleanup и исключает
// unsafe external replay.
func retrySerializableTransaction[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < serializableTransactionAttempts; attempt++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, errSerializableTransactionRetry) {
			return zero, err
		}
		if attempt+1 == serializableTransactionAttempts || ctx.Err() != nil {
			return zero, errs.ErrUnavailable
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, errs.ErrUnavailable
		case <-timer.C:
		}
	}
	return zero, errs.ErrUnavailable
}

// serializableTransactionError adds the retry marker to the safe fallback only for PostgreSQL
// serialization/deadlock codes or ErrTxCommitRollback; otherwise returns the fallback unchanged.
// Driver error details are discarded, and this mapping alone does not roll back a transaction or
// authorize repeating an external action.
// serializableTransactionError добавляет retry marker к safe fallback только для PostgreSQL
// serialization/deadlock codes либо ErrTxCommitRollback; иначе возвращает fallback без изменения.
// Driver error details отбрасываются; mapping сам не откатывает transaction и не разрешает повтор
// external action.
func serializableTransactionError(err, fallback error) error {
	if serializableTransactionConflict(err) || errors.Is(err, pgx.ErrTxCommitRollback) {
		return errors.Join(errSerializableTransactionRetry, fallback)
	}
	return fallback
}

// serializableTransactionConflict recognizes wrapped PostgreSQL errors with SQLSTATE 40001 or
// 40P01 through errors.As. It is an error-class predicate only and does not prove transaction
// cleanup, caller cancellation or a safe retry boundary.
// serializableTransactionConflict распознаёт wrapped PostgreSQL errors с SQLSTATE 40001 либо 40P01
// через errors.As. Это только error-class predicate, не доказывающий transaction cleanup, caller
// cancellation либо safe retry boundary.
func serializableTransactionConflict(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && (pgError.Code == "40001" || pgError.Code == "40P01")
}

// retryAssistantLockedRead bounds the entire shared retry loop by a five-second child context and
// prechecks cancellation before each read callback. Each callback must acquire a fresh authorized
// snapshot and complete rollback before retry; the child is cancelled on return, so this helper
// does not own a long-lived returned reader context.
// retryAssistantLockedRead ограничивает весь shared retry loop пятисекундным child context и
// проверяет cancellation до каждого read callback. Каждый callback получает fresh authorized
// snapshot и завершает rollback до retry; child отменяется при возврате, поэтому helper не владеет
// долгоживущим returned reader context.
func retryAssistantLockedRead[T any](ctx context.Context, operation func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return retrySerializableTransaction(ctx, func() (T, error) {
		if ctx.Err() != nil {
			var zero T
			return zero, errs.ErrUnavailable
		}
		return operation(ctx)
	})
}

// assistantLockedReadError marks only wrapped PostgreSQL 40001 failures with the supplied safe
// fallback, excluding deadlocks and unknown commit outcomes from this read classification. It
// discards driver SQL text and does not retry or clean up by itself.
// assistantLockedReadError помечает только wrapped PostgreSQL 40001 failures переданным safe
// fallback, исключая deadlocks и unknown commit outcomes из read classification. Отбрасывает
// driver SQL text и сам не повторяет operation и не выполняет cleanup.
func assistantLockedReadError(err, fallback error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "40001" {
		return errors.Join(errSerializableTransactionRetry, fallback)
	}
	return fallback
}

// rollbackAssistantLockedRead attempts rollback with an independent one-second context stripped of
// caller cancellation. Closed transactions are harmless; any other cleanup failure overwrites the
// caller error with Unavailable so an unfinished transaction is not silently retried. The
// transaction and result-error pointer must be valid.
// rollbackAssistantLockedRead пытается выполнить rollback с independent one-second context без
// caller cancellation. Closed transactions безопасны; любой другой cleanup failure заменяет caller
// error на Unavailable, чтобы незавершённая transaction не повторялась молча. Transaction и
// result-error pointer должны быть valid.
func rollbackAssistantLockedRead(ctx context.Context, tx pgx.Tx, resultError *error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		// До следующей попытки завершение старой транзакции обязательно.
		*resultError = errs.ErrUnavailable
	}
}
