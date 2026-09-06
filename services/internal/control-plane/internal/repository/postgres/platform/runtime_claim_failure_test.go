package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

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
