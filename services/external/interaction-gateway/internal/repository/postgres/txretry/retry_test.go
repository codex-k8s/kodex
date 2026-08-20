package txretry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunRetriesSerializationAndDeadlock(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := run(context.Background(), []time.Duration{0, 0, 0}, func() error {
		attempts++
		switch attempts {
		case 1:
			return &pgconn.PgError{Code: "40001"}
		case 2:
			return fmt.Errorf("commit transaction: %w", &pgconn.PgError{Code: "40P01"})
		default:
			return nil
		}
	})
	if err != nil || attempts != 3 {
		t.Fatalf("retry result: attempts=%d err=%v", attempts, err)
	}
}

func TestRunDoesNotRetryOtherErrors(t *testing.T) {
	t.Parallel()

	attempts := 0
	want := errors.New("validation rejected")
	err := run(context.Background(), []time.Duration{0, 0}, func() error {
		attempts++
		return want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("non-retryable result: attempts=%d err=%v", attempts, err)
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := run(ctx, []time.Duration{0, time.Hour}, func() error {
		attempts++
		cancel()
		return &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("canceled retry result: attempts=%d err=%v", attempts, err)
	}
}
