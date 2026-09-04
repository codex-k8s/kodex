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
