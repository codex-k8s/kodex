package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestSSHComposeAdmissionDenialNeverPersistsTransientFailure checks the complete error chain.
// A joined denial cannot override cancellation, unavailability, serialization or a transaction
// conflict; only an authoritative current eligibility refusal may create a durable marker.
// TestSSHComposeAdmissionDenialNeverPersistsTransientFailure проверяет всю error chain.
// Denial в joined error не перекрывает cancel, unavailable, serialization или transaction conflict;
// только авторитетный current eligibility отказ допускает durable marker.
func TestSSHComposeAdmissionDenialNeverPersistsTransientFailure(t *testing.T) {
	for _, err := range []error{errs.ErrForbidden, errs.ErrNotFound, fmt.Errorf("owner: %w", errs.ErrNotFound)} {
		if !permanentSSHComposeAdmissionDenial(err) {
			t.Fatal("authoritative denial was not recognized")
		}
	}
	for _, transient := range []error{context.Canceled, context.DeadlineExceeded, errs.ErrUnavailable, errs.ErrConflict,
		&pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40P01"},
		&pgconn.PgError{Code: "55P03"}, &pgconn.PgError{Code: "08006"}, &pgconn.PgError{Code: "42501"}} {
		for _, err := range []error{transient, errors.Join(errs.ErrNotFound, transient), errors.Join(transient, errs.ErrForbidden)} {
			if permanentSSHComposeAdmissionDenial(err) {
				t.Fatal("transient failure became a permanent admission decision")
			}
		}
	}
	if permanentSSHComposeAdmissionDenial(nil) {
		t.Fatal("successful read became an admission refusal")
	}
}
