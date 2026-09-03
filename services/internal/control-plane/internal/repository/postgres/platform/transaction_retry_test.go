package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetrySerializableTransactionRetriesOnlyTransactionConflicts(t *testing.T) {
	t.Parallel()

	attempts := 0
	result, err := retrySerializableTransaction(t.Context(), func() (string, error) {
		attempts++
		if attempts == 1 {
			return "", serializableTransactionError(&pgconn.PgError{Code: "40001"}, errs.ErrUnavailable)
		}
		return "committed", nil
	})
	if err != nil || result != "committed" || attempts != 2 {
		t.Fatalf("serialization retry mismatch: result=%q attempts=%d err=%v", result, attempts, err)
	}

	attempts = 0
	_, err = retrySerializableTransaction(t.Context(), func() (string, error) {
		attempts++
		return "", errs.ErrForbidden
	})
	if !errors.Is(err, errs.ErrForbidden) || attempts != 1 {
		t.Fatalf("domain error was retried: attempts=%d err=%v", attempts, err)
	}
}

func TestSerializableTransactionErrorClassification(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"40001", "40P01"} {
		mapped := serializableTransactionError(&pgconn.PgError{Code: code}, errs.ErrUnavailable)
		if !errors.Is(mapped, errSerializableTransactionRetry) || !errors.Is(mapped, errs.ErrUnavailable) {
			t.Fatalf("SQLSTATE %s is not retryable: %v", code, mapped)
		}
	}
	if mapped := serializableTransactionError(&pgconn.PgError{Code: "23505"}, errs.ErrConflict); !errors.Is(mapped, errs.ErrConflict) || errors.Is(mapped, errSerializableTransactionRetry) {
		t.Fatalf("unique violation classification mismatch: %v", mapped)
	}
}
