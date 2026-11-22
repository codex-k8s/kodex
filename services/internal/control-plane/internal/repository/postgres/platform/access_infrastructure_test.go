package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// accessFailureRow models an unsuccessful Scan without filling zero-valued authority fields.
// accessFailureRow моделирует неуспешный Scan без заполнения нулевых полей authority.
type accessFailureRow struct{ err error }

// Scan returns only the prepared driver failure, never a synthetic active subject.
// Scan возвращает только подготовленную driver failure, не создавая synthetic active subject.
func (row accessFailureRow) Scan(...any) error { return row.err }

// accessFailureTx supplies the exact subject/fallback row sequence without other transaction methods.
// accessFailureTx выдаёт точную последовательность subject/fallback rows без иных методов transaction.
type accessFailureTx struct {
	pgx.Tx
	rows  []error
	calls int
}

// QueryRow refuses unexpected access queries rather than manufacturing absent rows.
// QueryRow отвергает неожиданные access queries вместо вымышленного отсутствия строк.
func (tx *accessFailureTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.calls >= len(tx.rows) {
		panic("unexpected access query")
	}
	err := tx.rows[tx.calls]
	tx.calls++
	return accessFailureRow{err: err}
}

// TestAccessInfrastructureClassification covers partial Scan and OIDC fallback failures independently
// of absent/inactive subjects, plus infrastructure precedence over joined denial sentinels.
// TestAccessInfrastructureClassification проверяет partial Scan и OIDC fallback failures отдельно
// от отсутствующих/неактивных subjects и приоритет infrastructure над joined denial sentinels.
func TestAccessInfrastructureClassification(t *testing.T) {
	fault := errors.New("synthetic scan failure")
	for _, item := range []struct {
		name string
		rows []error
		want error
	}{
		{"subject scan", []error{fault}, errs.ErrUnavailable},
		{"mixed absent infrastructure", []error{errors.Join(pgx.ErrNoRows, errs.ErrUnavailable)}, errs.ErrUnavailable},
		{"mixed group infrastructure", []error{pgx.ErrNoRows, errors.Join(pgx.ErrNoRows, errs.ErrUnavailable)}, errs.ErrUnavailable},
		{"group scan", []error{pgx.ErrNoRows, fault}, errs.ErrUnavailable},
		{"missing", []error{pgx.ErrNoRows, pgx.ErrNoRows}, errs.ErrNotFound},
		{"inactive subject", []error{nil}, errs.ErrNotFound},
		{"inactive group", []error{pgx.ErrNoRows, nil}, errs.ErrNotFound},
	} {
		t.Run(item.name, func(t *testing.T) {
			tx := &accessFailureTx{rows: item.rows}
			_, err := (&Repository{}).resolveAccessSubject(t.Context(), tx, "org_fixture", "subject_fixture")
			if !errors.Is(err, item.want) || tx.calls != len(item.rows) {
				t.Fatal("wrong access failure classification")
			}
		})
	}
	for _, infrastructure := range []error{errs.ErrUnavailable, context.Canceled, context.DeadlineExceeded} {
		mixed := errors.Join(infrastructure, errs.ErrNotFound, errs.ErrForbidden)
		if concealAccessDenial(mixed, errs.ErrForbidden) != mixed || !accessInfrastructureFailure(mixed) {
			t.Fatal("infrastructure cause lost to joined denial")
		}
	}
	if concealAccessDenial(errs.ErrForbidden, errs.ErrNotFound) != errs.ErrNotFound || concealAccessDenial(errs.ErrNotFound, errs.ErrForbidden) != errs.ErrForbidden {
		t.Fatal("proven denial concealment changed")
	}
	if concealAccessDenial(fault, errs.ErrNotFound) != fault {
		t.Fatal("unknown failure became denial")
	}
}
