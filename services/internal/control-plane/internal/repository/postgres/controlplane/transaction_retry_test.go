package controlplane

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
)

func TestTransactionRetryDelayIsBoundedExponential(t *testing.T) {
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	for attempt, expected := range want {
		if actual := transactionRetryDelay(attempt); actual != expected {
			t.Fatalf("attempt %d delay = %s, want %s", attempt, actual, expected)
		}
	}
}

func TestMapErrorPreservesClassifiedDomainErrors(t *testing.T) {
	t.Parallel()
	for _, domainError := range []error{
		errs.ErrInvalidInput,
		errs.ErrUnauthenticated,
		errs.ErrPermissionDenied,
		errs.ErrNotFound,
		errs.ErrStateConflict,
		errs.ErrIdempotencyConflict,
		errs.ErrAborted,
		errs.ErrVersionMismatch,
		errs.ErrFailedPrecondition,
		errs.ErrDataLoss,
		errs.ErrUnavailable,
		errs.ErrInternal,
	} {
		domainError := domainError
		t.Run(domainError.Error(), func(t *testing.T) {
			t.Parallel()
			wrapped := errs.WithSafeCode(domainError, "LEGACY_RETRY_TEST")
			actual := mapError(wrapped)
			if !errors.Is(actual, domainError) || errs.SafeCode(actual) != "LEGACY_RETRY_TEST" {
				t.Fatalf("mapError() lost domain classification: %v", actual)
			}
		})
	}
}
