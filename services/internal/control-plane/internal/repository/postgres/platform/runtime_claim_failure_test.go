package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestRuntimeCandidateEligibilityDoesNotHideInfrastructureFailure checks all six wrapped domain outcomes against infrastructure and joined-error counterexamples so a provider/database interruption cannot be treated as revoked eligibility.
// TestRuntimeCandidateEligibilityDoesNotHideInfrastructureFailure проверяет все шесть обёрнутых доменных исходов и инфраструктурные/объединённые контрпримеры, чтобы перебой провайдера/БД не трактовался как отзыв доступности.
func TestRuntimeCandidateEligibilityDoesNotHideInfrastructureFailure(t *testing.T) {
	for _, err := range []error{errs.ErrConflict, errs.ErrVersionMismatch, errs.ErrNotFound, errs.ErrForbidden, errs.ErrCapabilityRequired, errs.ErrInvalid} {
		if !runtimeCandidateEligibilityFailure(fmt.Errorf("candidate eligibility: %w", err)) {
			t.Fatalf("expected owner eligibility outcome rejected: %v", err)
		}
	}
	for _, err := range []error{
		errs.ErrUnavailable, context.Canceled, context.DeadlineExceeded,
		&pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "23505"},
		errors.Join(errs.ErrConflict, &pgconn.PgError{Code: "40P01"}),
		errors.Join(errs.ErrConflict, context.Canceled), errors.New("unexpected adapter failure"),
	} {
		if runtimeCandidateEligibilityFailure(err) {
			t.Fatalf("infrastructure failure would be converted to terminal eligibility: %v", err)
		}
	}
}

// TestRuntimeEligibilityErrorClassIsBounded verifies the fixed domain class mapping, wrapped-sentinel support and unknown fallback without leaking an arbitrary diagnostic string into class output.
// TestRuntimeEligibilityErrorClassIsBounded проверяет фиксированное отображение доменных классов, поддержку обёрнутой sentinel и unknown без переноса произвольной диагностической строки в класс.
func TestRuntimeEligibilityErrorClassIsBounded(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("wrapped: %w", errs.ErrConflict), "CONFLICT"},
		{errs.ErrVersionMismatch, "VERSION_MISMATCH"},
		{errs.ErrNotFound, "NOT_FOUND"},
		{errs.ErrForbidden, "FORBIDDEN"},
		{errs.ErrCapabilityRequired, "CAPABILITY_REQUIRED"},
		{errs.ErrInvalid, "INVALID"},
		{errors.New("private diagnostic"), "UNKNOWN"},
	} {
		if got := runtimeEligibilityErrorClass(test.err); got != test.want {
			t.Fatalf("error class = %q, want %q", got, test.want)
		}
	}
}
