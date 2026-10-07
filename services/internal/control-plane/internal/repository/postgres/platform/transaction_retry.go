package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const serializableTransactionAttempts = 3

var errSerializableTransactionRetry = errors.New("retry serializable transaction")

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

func serializableTransactionError(err, fallback error) error {
	if serializableTransactionConflict(err) || errors.Is(err, pgx.ErrTxCommitRollback) {
		return errors.Join(errSerializableTransactionRetry, fallback)
	}
	return fallback
}

func serializableTransactionConflict(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && (pgError.Code == "40001" || pgError.Code == "40P01")
}

// Assistant read удерживает lease до завершения одного snapshot. Renew может
// изменить строку между snapshot и FOR SHARE; повтор начинает всю операцию
// заново после rollback. Общий цикл сохраняет максимум три попытки и backoff.
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

// Read path не наследует retry deadlock/unknown commit outcome от commands.
// SQL text и driver payload не входят в возвращаемую ошибку.
func assistantLockedReadError(err, fallback error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "40001" {
		return errors.Join(errSerializableTransactionRetry, fallback)
	}
	return fallback
}

func rollbackAssistantLockedRead(ctx context.Context, tx pgx.Tx, resultError *error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		// До следующей попытки завершение старой транзакции обязательно.
		*resultError = errs.ErrUnavailable
	}
}
