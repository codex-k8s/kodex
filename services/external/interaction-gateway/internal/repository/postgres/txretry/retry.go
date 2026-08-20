// Package txretry ограниченно повторяет конкурентные PostgreSQL transaction.
package txretry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var retryDelays = [...]time.Duration{
	0,
	10 * time.Millisecond,
	30 * time.Millisecond,
	75 * time.Millisecond,
	150 * time.Millisecond,
}

// Run повторяет всю transaction только после serialization failure или deadlock.
func Run(ctx context.Context, operation func() error) error {
	if operation == nil {
		return errors.New("PostgreSQL transaction retry operation is nil")
	}
	return run(ctx, retryDelays[:], operation)
}

func run(ctx context.Context, delays []time.Duration, operation func() error) error {
	var lastErr error
	for _, delay := range delays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		lastErr = operation()
		if lastErr == nil || !isRetryable(lastErr) {
			return lastErr
		}
	}
	return fmt.Errorf("retry PostgreSQL transaction: %w", lastErr)
}

func isRetryable(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	return postgresError.Code == "40001" || postgresError.Code == "40P01"
}
