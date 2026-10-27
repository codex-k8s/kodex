package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// ownerProjectionFixture reproduces positional scan output and partial failures.
// ownerProjectionFixture воспроизводит позиционное чтение и частичное заполнение при ошибке.
type ownerProjectionFixture struct {
	raw     string
	failure error
}

// Scan fills four private IDs before the JSON or a synthetic failure.
// Scan заполняет четыре внутренних ID до JSON либо синтетической ошибки.
func (row ownerProjectionFixture) Scan(dest ...any) error {
	*dest[0].(*string) = "private-id"
	if row.failure != nil {
		return row.failure
	}
	for i := 1; i < 4; i++ {
		*dest[i].(*string) = "pin"
	}
	*dest[4].(*[]byte) = []byte(row.raw)
	return nil
}

// TestOwnerProjectionScans keeps typed JSON and partially scanned IDs under every error.
// TestOwnerProjectionScans сохраняет typed JSON и частичные ID при каждом виде ошибки.
func TestOwnerProjectionScans(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  ownerProjectionFixture
		want error
		ref  string
	}{
		{"success", ownerProjectionFixture{raw: `{"ref":"synthetic"}`}, nil, "synthetic"},
		{"missing", ownerProjectionFixture{failure: fmt.Errorf("wrapped: %w", pgx.ErrNoRows)}, errs.ErrNotFound, ""},
		{"driver", ownerProjectionFixture{failure: errors.New("synthetic error")}, errs.ErrUnavailable, ""},
		{"invalid json", ownerProjectionFixture{raw: "{"}, errs.ErrUnavailable, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory, err := scanMemoryRecord(tc.row)
			if !errors.Is(err, tc.want) || memory.id != "private-id" || memory.Ref != tc.ref {
				t.Fatal("memory scan changed")
			}
			skill, err := scanSkillBundle(tc.row)
			if !errors.Is(err, tc.want) || skill.id != "private-id" || skill.Ref != tc.ref {
				t.Fatal("skill scan changed")
			}
		})
	}
}

// ownerVersionLockFixture checks exact query and owner pins before returning a synthetic row.
// ownerVersionLockFixture проверяет запрос и owner pins перед возвратом синтетической строки.
type ownerVersionLockFixture struct {
	pgx.Tx
	t       *testing.T
	query   string
	failure error
}

// QueryRow validates the tenant and reference for the caller-selected lock query.
// QueryRow проверяет tenant и reference для выбранного вызывающим lock query.
func (tx ownerVersionLockFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	if sql != tx.query || len(args) != 2 || args[0] != "org" || args[1] != "ref" {
		tx.t.Fatal("lock query or owner pins changed")
	}
	return tx
}

// Scan returns the locked tuple or a configured driver error.
// Scan возвращает заблокированный tuple либо настроенную ошибку драйвера.
func (tx ownerVersionLockFixture) Scan(dest ...any) error {
	if tx.failure != nil {
		return tx.failure
	}
	*dest[0].(*string) = "id"
	*dest[1].(*int64) = 7
	*dest[2].(*string) = "ACTIVE"
	return nil
}

// TestOwnerVersionLock checks exact SQL selection, tuple and fail-closed mappings.
// TestOwnerVersionLock проверяет точный выбор SQL, tuple и закрытые ошибки чтения.
func TestOwnerVersionLock(t *testing.T) {
	for _, query := range []string{queryMemoryRecordLock, querySkillBundleLock} {
		for _, tc := range []struct{ failure, want error }{{nil, nil}, {fmt.Errorf("wrapped: %w", pgx.ErrNoRows), errs.ErrNotFound}, {errors.New("synthetic error"), errs.ErrUnavailable}} {
			tx := ownerVersionLockFixture{t: t, query: query, failure: tc.failure}
			expected := int64(7)
			id, version, state, err := lockOwnerVersionState(t.Context(), tx, query, "org", "ref", &expected)
			if !errors.Is(err, tc.want) {
				t.Fatal("lock error changed")
			}
			if tc.want == nil && (id != "id" || version != 7 || state != "ACTIVE") {
				t.Fatal("lock tuple changed")
			}
		}
		for _, expected := range []*int64{nil, new(int64)} {
			tx := ownerVersionLockFixture{t: t, query: query}
			_, _, _, err := lockOwnerVersionState(t.Context(), tx, query, "org", "ref", expected)
			if !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatal("expected version mismatch")
			}
		}
	}
}
