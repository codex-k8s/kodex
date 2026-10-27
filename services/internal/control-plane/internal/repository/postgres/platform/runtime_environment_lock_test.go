package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// environmentLockFixture checks query identity and supplies one synthetic lock row.
// environmentLockFixture проверяет идентичность запроса и возвращает синтетическую lock row.
type environmentLockFixture struct {
	pgx.Tx
	t       *testing.T
	failure error
	calls   int
}

// QueryRow checks the exact SQL and organization/resource pins.
// QueryRow проверяет точный SQL и привязку организации и ресурса.
func (tx *environmentLockFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	tx.calls++
	if sql != queryRuntimeConfigurationLockEnvironment || len(args) != 2 || args[0] != "org" || args[1] != "env" {
		tx.t.Fatal("wrong lock query or scope")
	}
	return tx
}

// Scan returns the ordered tuple or the configured synthetic storage failure.
// Scan возвращает упорядоченный tuple либо настроенную ошибку хранилища.
func (tx *environmentLockFixture) Scan(dest ...any) error {
	if tx.failure != nil {
		return tx.failure
	}
	*dest[0].(*string) = "env-id"
	*dest[1].(*string) = "project-id"
	*dest[2].(*string) = "project-ref"
	*dest[3].(*int64) = 11
	*dest[4].(*string) = "revision-id"
	*dest[5].(*int64) = 12
	return nil
}

// TestLockRuntimeEnvironmentTuple verifies exact tuple order, tenant pins and closed read errors.
// TestLockRuntimeEnvironmentTuple проверяет порядок tuple, tenant pins и закрытые ошибки чтения.
func TestLockRuntimeEnvironmentTuple(t *testing.T) {
	for _, tc := range []struct {
		name          string
		failure, want error
	}{
		{"success", nil, nil}, {"missing", fmt.Errorf("wrapped: %w", pgx.ErrNoRows), errs.ErrNotFound}, {"driver", errors.New("synthetic failure"), errs.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &environmentLockFixture{t: t, failure: tc.failure}
			got, err := lockRuntimeEnvironmentTuple(t.Context(), tx, "org", "env")
			if !errors.Is(err, tc.want) || tx.calls != 1 {
				t.Fatal("incorrect lock outcome")
			}
			if tc.want == nil && (got.environmentID != "env-id" || got.projectID != "project-id" || got.projectRef != "project-ref" || got.environmentVersion != 11 || got.currentVersionID != "revision-id" || got.currentRevision != 12) {
				t.Fatal("lock tuple changed")
			}
		})
	}
}

// environmentDraftLockFixture supplies one draft with an invalid owner scope.
// environmentDraftLockFixture возвращает черновик с недопустимой областью владельца.
type environmentDraftLockFixture struct {
	pgx.Tx
	t     *testing.T
	calls int
}

// QueryRow requires the exact tenant-scoped draft lock.
// QueryRow требует точную блокировку черновика в области организации.
func (tx *environmentDraftLockFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	tx.calls++
	if sql != queryEnvironmentDraftLock || len(args) != 2 || args[0] != "org" || args[1] != "draft" {
		tx.t.Fatal("wrong draft lock query or scope")
	}
	return tx
}

// Scan fills the minimum synthetic draft fields needed for decoding and scope validation.
// Scan заполняет минимум синтетических полей для декодирования и проверки области.
func (tx *environmentDraftLockFixture) Scan(dest ...any) error {
	*dest[5].(*int64) = 7
	*dest[6].(*[]byte) = []byte("{}")
	*dest[8].(*[]byte) = []byte("[]")
	*dest[13].(*string) = "INVALID"
	return nil
}

// TestEnvironmentDraftOwnerPrecedesVersion rejects an invalid owner scope before exposing OCC.
// TestEnvironmentDraftOwnerPrecedesVersion отклоняет неверную область владельца до проверки OCC.
func TestEnvironmentDraftOwnerPrecedesVersion(t *testing.T) {
	for _, version := range []*int64{nil, new(int64)} {
		tx := &environmentDraftLockFixture{t: t}
		_, err := (&Repository{}).lockEnvironmentDraftForMutation(t.Context(), tx, scope{organizationID: "org"}, "draft", version)
		if !errors.Is(err, errs.ErrNotFound) || tx.calls != 1 {
			t.Fatal("owner scope must precede version check")
		}
	}
}
