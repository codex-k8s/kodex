package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// secretAdmissionRow supplies controlled metadata or a failure without connecting to a database or accessing secret values.
// secretAdmissionRow передаёт управляемые metadata либо failure без подключения к БД и доступа к значениям секретов.
type secretAdmissionRow func(...any) error

// Scan preserves the exact callback result for the real owner scanner.
// Scan сохраняет точный callback result для настоящего owner scanner.
func (row secretAdmissionRow) Scan(dest ...any) error { return row(dest...) }

// TestRuntimeEnvironmentSecretTargetScan rejects malformed/null/unknown JSON and owner shapes without confusing an unavailable read with absent metadata.
// TestRuntimeEnvironmentSecretTargetScan отклоняет malformed/null/unknown JSON и owner shapes, не смешивая unavailable read с отсутствующими metadata.
func TestRuntimeEnvironmentSecretTargetScan(t *testing.T) {
	for _, item := range []struct {
		name, scope, project, raw string
		readErr, want             error
	}{
		{name: "empty project secrets", scope: "PROJECT", project: "project", raw: "[]"},
		{name: "empty organization secrets", scope: "ORGANIZATION", raw: "[]"},
		{name: "null is not an array", scope: "PROJECT", project: "project", raw: "null", want: errs.ErrUnavailable},
		{name: "malformed JSON", scope: "PROJECT", project: "project", raw: "[", want: errs.ErrUnavailable},
		{name: "unknown JSON member", scope: "PROJECT", project: "project", raw: `[{"unknown":true}]`, want: errs.ErrUnavailable},
		{name: "trailing JSON", scope: "PROJECT", project: "project", raw: "[] {}", want: errs.ErrUnavailable},
		{name: "unknown owner scope", scope: "UNKNOWN", raw: "[]", want: errs.ErrUnavailable},
		{name: "missing project owner", scope: "PROJECT", raw: "[]", want: errs.ErrUnavailable},
		{name: "organization cannot take project", scope: "ORGANIZATION", project: "project", raw: "[]", want: errs.ErrUnavailable},
		{name: "missing row", readErr: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "SQL failure", readErr: &pgconn.PgError{Code: "XX000"}, want: errs.ErrUnavailable},
		{name: "lock contention", readErr: &pgconn.PgError{Code: "55P03"}, want: errs.ErrUnavailable},
		{name: "cancelled read", readErr: context.Canceled, want: errs.ErrUnavailable},
	} {
		t.Run(item.name, func(t *testing.T) {
			_, err := scanRuntimeEnvironmentSecretTarget(secretAdmissionRow(func(dest ...any) error {
				if item.readErr != nil {
					return item.readErr
				}
				*dest[0].(*string), *dest[1].(*string), *dest[2].(*string), *dest[3].(*[]byte) = item.scope, item.project, "renvv_exact", []byte(item.raw)
				return nil
			}))
			if !errors.Is(err, item.want) {
				t.Fatalf("stored target error=%v want=%v", err, item.want)
			}
		})
	}
}

// secretAdmissionTx admits only the NOWAIT metadata lock and existing canonical resolver, checking that both use the same exact owner/revision pins.
// secretAdmissionTx допускает лишь NOWAIT metadata lock и существующий canonical resolver, проверяя одинаковые exact owner/revision pins в обоих.
type secretAdmissionTx struct {
	pgx.Tx
	t                   *testing.T
	want, actual        entity.RuntimeSecretDescriptor
	lockErr, resolveErr error
	reads               int
}

// QueryRow exposes either a locked revision or the full descriptor and rejects unrelated SQL or latest-revision fallback.
// QueryRow раскрывает locked revision либо полный descriptor и отклоняет посторонний SQL и latest-revision fallback.
func (tx *secretAdmissionTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	tx.reads++
	want := pgx.StrictNamedArgs{"organization_id": "tenant", "project_id": "project", "scope_kind": "PROJECT", "secret_ref": tx.want.SecretRef, "revision": tx.want.Revision}
	if len(args) != 1 || !reflect.DeepEqual(args[0], want) {
		tx.t.Fatal("secret admission lost exact owner/revision pins")
	}
	return secretAdmissionRow(func(dest ...any) error {
		switch sql {
		case queryRuntimeSecretExactAdmissionLock:
			if tx.lockErr != nil {
				return tx.lockErr
			}
			*dest[0].(*int64) = tx.want.Revision
		case queryRuntimeSecretResolveBinding:
			if tx.resolveErr != nil {
				return tx.resolveErr
			}
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*int64) = tx.actual.SecretRef, tx.actual.Namespace, tx.actual.Revision
			*dest[3].(*string), *dest[4].(*string), *dest[5].(*string) = tx.actual.SecretName, tx.actual.SecretKey, tx.actual.SecretUID
			*dest[6].(*string), *dest[7].(*string) = tx.actual.SecretResourceVersion, tx.actual.ContentSHA256
		default:
			tx.t.Fatal("secret admission reached an unrelated query")
		}
		return nil
	})
}

// TestRuntimeEnvironmentSecretDescriptorEligibility preserves exact historical pins and keeps contention/storage/cancel errors outside the terminal claim class set.
// TestRuntimeEnvironmentSecretDescriptorEligibility сохраняет exact historical pins и исключает contention/storage/cancel errors из terminal claim class set.
func TestRuntimeEnvironmentSecretDescriptorEligibility(t *testing.T) {
	expected := entity.RuntimeSecretDescriptor{Name: "MARKER", SecretRef: "sec_exact", Namespace: "runtime", Revision: 1,
		SecretName: "runtime-secret-exact-r1", SecretKey: "value", SecretUID: "uid", SecretResourceVersion: "7", ContentSHA256: strings.Repeat("a", 64)}
	for _, item := range []struct {
		name                      string
		lockErr, resolveErr, want error
		mutate                    func(*entity.RuntimeSecretDescriptor)
	}{
		{name: "eligible exact historical revision"},
		{name: "missing owner row", lockErr: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "inactive exact revision", resolveErr: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "NOWAIT contention", lockErr: &pgconn.PgError{Code: "55P03"}, want: errs.ErrUnavailable},
		{name: "SQL failure", resolveErr: &pgconn.PgError{Code: "XX000"}, want: errs.ErrUnavailable},
		{name: "caller cancellation", lockErr: context.Canceled, want: errs.ErrUnavailable},
		{name: "ref substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.SecretRef = "sec_other" }, want: errs.ErrConflict},
		{name: "revision substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.Revision = 2 }, want: errs.ErrConflict},
		{name: "namespace substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.Namespace = "other" }, want: errs.ErrConflict},
		{name: "name substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.SecretName = "other" }, want: errs.ErrConflict},
		{name: "key substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.SecretKey = "other" }, want: errs.ErrConflict},
		{name: "UID substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.SecretUID = "other" }, want: errs.ErrConflict},
		{name: "RV substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.SecretResourceVersion = "8" }, want: errs.ErrConflict},
		{name: "digest substitution", mutate: func(d *entity.RuntimeSecretDescriptor) { d.ContentSHA256 = strings.Repeat("b", 64) }, want: errs.ErrConflict},
	} {
		t.Run(item.name, func(t *testing.T) {
			actual := expected
			if item.mutate != nil {
				item.mutate(&actual)
			}
			tx := &secretAdmissionTx{t: t, want: expected, actual: actual, lockErr: item.lockErr, resolveErr: item.resolveErr}
			err := requireRuntimeEnvironmentSecretDescriptors(t.Context(), tx, "tenant", runtimeEnvironmentSecretTarget{scopeKind: "PROJECT", projectID: "project", descriptors: []entity.RuntimeSecretDescriptor{expected}})
			if !errors.Is(err, item.want) {
				t.Fatalf("descriptor eligibility error=%v want=%v", err, item.want)
			}
			if errors.Is(item.want, errs.ErrUnavailable) && runtimeCandidateEligibilityFailure(err) {
				t.Fatal("temporary storage/lock failure became terminal claim failure")
			}
		})
	}
	for _, malformed := range []entity.RuntimeSecretDescriptor{{}, {Name: "MARKER", SecretRef: "sec_exact", Revision: 0, Namespace: "runtime"}} {
		tx := &secretAdmissionTx{t: t}
		if err := requireRuntimeEnvironmentSecretDescriptors(t.Context(), tx, "tenant", runtimeEnvironmentSecretTarget{descriptors: []entity.RuntimeSecretDescriptor{malformed}}); !errors.Is(err, errs.ErrConflict) || tx.reads != 0 {
			t.Fatal("malformed descriptor reached a latest/unknown SQL lookup")
		}
	}
}
