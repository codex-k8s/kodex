package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// secretReferenceRow exposes one authoritative reference decision or a storage failure, without secret material.
// secretReferenceRow возвращает одно авторитетное решение об удержании либо storage failure без материала секрета.
type secretReferenceRow struct {
	referenced bool
	err        error
}

// Scan preserves the distinction between a negative reference decision and a failed owner read.
// Scan сохраняет различие между отсутствием удерживающей ссылки и ошибкой owner read.
func (row secretReferenceRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*bool) = row.referenced
	return nil
}

// secretReferenceTx admits only the exact tenant/secret owner query and has no mutation implementation.
// secretReferenceTx допускает лишь точный tenant/secret owner query и не реализует изменение состояния.
type secretReferenceTx struct {
	pgx.Tx
	t   *testing.T
	row secretReferenceRow
}

// QueryRow verifies the owner pins before returning the controlled read result.
// QueryRow проверяет owner pins до возврата управляемого результата чтения.
func (tx secretReferenceTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	if sql != queryRuntimeSecretIsReferenced || len(args) != 1 {
		tx.t.Fatal("reference check used an unexpected query")
	}
	pins, ok := args[0].(pgx.StrictNamedArgs)
	if !ok || len(pins) != 2 || pins["organization_id"] != "owner-organization" || pins["secret_ref"] != "sec_reference" {
		tx.t.Fatal("reference check lost exact owner pins")
	}
	return tx.row
}

// TestRuntimeSecretReferenceReadFailureClosed proves only a successful false decision permits revocation; storage errors never impersonate absence.
// TestRuntimeSecretReferenceReadFailureClosed доказывает, что отзыв допускает лишь успешный false; storage errors не подменяют отсутствие ссылок.
func TestRuntimeSecretReferenceReadFailureClosed(t *testing.T) {
	for _, item := range []struct {
		name string
		row  secretReferenceRow
		want error
	}{
		{name: "no consumers", row: secretReferenceRow{}},
		{name: "retained consumer", row: secretReferenceRow{referenced: true}, want: errs.ErrResourceInUse},
		{name: "storage failure", row: secretReferenceRow{err: errors.New("synthetic storage failure")}, want: errs.ErrUnavailable},
		{name: "missing owner result", row: secretReferenceRow{err: pgx.ErrNoRows}, want: errs.ErrUnavailable},
	} {
		t.Run(item.name, func(t *testing.T) {
			err := (&Repository{}).ensureRuntimeSecretUnreferenced(t.Context(), secretReferenceTx{t: t, row: item.row}, "owner-organization", "sec_reference")
			if !errors.Is(err, item.want) {
				t.Fatalf("reference decision error = %v, want %v", err, item.want)
			}
		})
	}
}
