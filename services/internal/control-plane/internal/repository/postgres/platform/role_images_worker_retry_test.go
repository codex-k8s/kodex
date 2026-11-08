package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestRoleImageWorkerRetryClassification checks the shared bounded loop's adapter: only explicit
// role-image or serializable markers retry, while plain availability and authority errors fail
// once. Failed callbacks cannot expose partial results or increase the existing attempt budget.
// TestRoleImageWorkerRetryClassification проверяет adapter общего bounded loop: повторяются лишь
// явные role-image либо serializable markers, обычные availability и authority errors отклоняются
// после одной попытки. Partial results не выдаются, прежний attempt budget не увеличивается.
func TestRoleImageWorkerRetryClassification(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		failure  error
		attempts int
	}{
		{"role-image-rollback", errors.Join(errRoleImageTransactionRetry, errs.ErrConflict), 3},
		{"commit-reconciliation", errors.Join(errRoleImageTransactionRetry, errs.ErrUnavailable), 3},
		{"outbox-rollback", errors.Join(errSerializableTransactionRetry, errs.ErrUnavailable), 3},
		{"plain-unavailable", errs.ErrUnavailable, 1},
		{"forbidden", errs.ErrForbidden, 1},
		{"version-mismatch", errs.ErrVersionMismatch, 1},
		{"idempotency-reuse", errs.ErrIdempotencyReuse, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			result, err := retryRoleImageWorkerTransaction(t.Context(), func() (int, error) { calls++; return 42, scenario.failure })
			wanted := scenario.failure
			if scenario.attempts == 3 {
				wanted = errs.ErrUnavailable
			}
			if calls != scenario.attempts || result != 0 || !errors.Is(err, wanted) {
				t.Fatal("incorrect retry class, budget or partial result")
			}
		})
	}
}

// TestRoleImageWorkerRetryCancellation proves both an already-cancelled context and cancellation
// after the first known rollback stop before another transaction, returning a zero result.
// TestRoleImageWorkerRetryCancellation доказывает, что уже отменённый context и cancellation
// после первого известного rollback останавливают новую transaction и возвращают zero result.
func TestRoleImageWorkerRetryCancellation(t *testing.T) {
	for _, before := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if before {
			cancel()
		}
		calls := 0
		result, err := retryRoleImageWorkerTransaction(ctx, func() (int, error) {
			calls++
			cancel()
			return 42, errors.Join(errRoleImageTransactionRetry, errs.ErrConflict)
		})
		expected := 1
		if before {
			expected = 0
		}
		if calls != expected || result != 0 || !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("cancelled transaction restarted or exposed partial result")
		}
	}
}
