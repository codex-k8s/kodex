package platform

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// sessionReadinessFixture implements a fixed readiness row using synthetic typed values, an
// optional failure and query-use tracking. It deliberately rejects unbounded queries and cannot
// represent a real pgx connection or prove live session/account readiness.
// sessionReadinessFixture реализует fixed readiness row через synthetic typed values, optional
// failure и query-use tracking. Намеренно отклоняет unbounded queries и не является real pgx
// connection и доказательством live session/account readiness.
type sessionReadinessFixture struct {
	values  []any
	failure error
	called  bool
}

// Query panics on any unbounded Query call so readiness projection must use its exact QueryRow
// seam. No SQL executes and the panic is a test contract failure rather than a runtime diagnostic.
// Query вызывает panic при любом unbounded Query, заставляя readiness projection использовать
// exact QueryRow seam. SQL не исполняется, panic является test contract failure, а не runtime
// diagnostic.
func (fixture *sessionReadinessFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected unbounded query")
}

// QueryRow records use and requires the exact readiness SQL with one StrictNamedArgs
// tenant/run/session tuple before returning the fixture row. Unexpected SQL or pins panic; this
// validates query construction without evaluating PostgreSQL policy.
// QueryRow фиксирует вызов и требует exact readiness SQL с одним StrictNamedArgs
// tenant/run/session tuple до возврата fixture row. Неверные SQL либо pins вызывают panic;
// проверяет query construction без PostgreSQL policy evaluation.
func (fixture *sessionReadinessFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	fixture.called = true
	if sql != queryRunSessionReadiness || len(args) != 1 {
		panic("unexpected readiness query")
	}
	pins := args[0].(pgx.StrictNamedArgs)
	if pins["organization_id"] != "organization-fixture" || pins["run_ref"] != "run_fixture01" || pins["session_ref"] != "ses_fixture01" {
		panic("readiness owner binding lost")
	}
	return fixture
}

// Scan returns the configured failure or reflectively copies fixed typed values to supplied
// destinations. It assumes exact destination count/types and supports no general row conversion,
// database read or isolation behavior.
// Scan возвращает настроенную ошибку либо reflectively копирует fixed typed values в destinations.
// Предполагает точные destination count/types и не поддерживает general row conversion, DB read и
// isolation behavior.
func (fixture *sessionReadinessFixture) Scan(dest ...any) error {
	if fixture.failure != nil {
		return fixture.failure
	}
	for i, v := range fixture.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// TestRunSessionReadinessProjectionClosed table-checks storage/account/earlier-execution blocker
// reasons and safe archive-task error normalization, requiring private sentinels absent from JSON.
// It tests synthetic projection rows without asserting live storage, a runnable lease or real
// account availability.
// TestRunSessionReadinessProjectionClosed таблично проверяет storage/account/earlier-execution
// blocker reasons и safe archive-task error normalization с отсутствием private sentinels в JSON.
// Проверяет synthetic projection rows без live storage, runnable lease и real account
// availability.
func TestRunSessionReadinessProjectionClosed(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		state            string
		account, earlier bool
		reason           string
	}{
		"untracked":        {"UNTRACKED", true, false, "NO_SESSION_BLOCKER"},
		"live":             {"LIVE", true, false, "NO_SESSION_BLOCKER"},
		"archive":          {"ARCHIVED", true, false, "STORAGE_NOT_LIVE"},
		"terminal failure": {"ERROR", true, false, "STORAGE_NOT_LIVE"},
		"purged":           {"PURGED", true, false, "STORAGE_NOT_LIVE"},
		"account":          {"LIVE", false, false, "SESSION_ACCOUNT_UNAVAILABLE"},
		"predecessor":      {"LIVE", true, true, "EARLIER_EXECUTION"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := &sessionReadinessFixture{values: []any{"ses_fixture01", c.state, c.account, c.earlier, "sat_fixture01", "SNAPSHOT", "DEAD_LETTER", int32(5), int32(5), "private sentinel password=secret"}}
			run := entity.Run{Ref: "run_fixture01", SessionRef: "ses_fixture01"}
			if err := attachRunSessionReadiness(t.Context(), fixture, scope{organizationID: "organization-fixture"}, &run); err != nil {
				t.Fatal(err)
			}
			if run.SessionReadiness.Reason != c.reason || run.SessionReadiness.LatestArchiveTask.Ref != "sat_fixture01" || run.SessionReadiness.LatestArchiveTask.SafeErrorCode != "UNKNOWN" {
				t.Fatal("closed session projection mismatch")
			}
			raw, _ := json.Marshal(run.SessionReadiness)
			if strings.Contains(string(raw), "private sentinel") || strings.Contains(string(raw), "password") || strings.Contains(string(raw), "secret") {
				t.Fatal("private error leaked")
			}
		})
	}
}

// TestRunSessionReadinessUnknownFailsClosed mutates owner session, storage/task shape, attempts
// and backend failure, requiring Unavailable with nil projection and no private error text.
// Missing session must avoid querying. It verifies the closed projection seam without changing
// real session or archive state.
// TestRunSessionReadinessUnknownFailsClosed подменяет owner session, storage/task shape, attempts
// и backend failure, требуя Unavailable с nil projection без private error text. Missing session
// не вызывает query. Проверяет closed projection seam без изменения real session и archive state.
func TestRunSessionReadinessUnknownFailsClosed(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*sessionReadinessFixture){
		func(f *sessionReadinessFixture) { f.values[0] = "ses_foreign01" },
		func(f *sessionReadinessFixture) { f.values[1] = "private-sentinel" },
		func(f *sessionReadinessFixture) { f.values[4] = "private/payload" },
		func(f *sessionReadinessFixture) { f.values[5] = "DELETE_OBJECT" },
		func(f *sessionReadinessFixture) { f.values[6] = "private-sentinel" },
		func(f *sessionReadinessFixture) { f.values[7] = int32(6) },
		func(f *sessionReadinessFixture) { f.values[8] = int32(6) },
		func(f *sessionReadinessFixture) { f.failure = errors.New("private database sentinel") },
	} {
		fixture := &sessionReadinessFixture{values: []any{"ses_fixture01", "LIVE", true, false, "sat_fixture01", "SNAPSHOT", "READY", int32(0), int32(5), ""}}
		mutate(fixture)
		run := entity.Run{Ref: "run_fixture01", SessionRef: "ses_fixture01"}
		err := attachRunSessionReadiness(t.Context(), fixture, scope{organizationID: "organization-fixture"}, &run)
		if !errors.Is(err, errs.ErrUnavailable) || run.SessionReadiness != nil || strings.Contains(err.Error(), "sentinel") {
			t.Fatal("invalid session projection was not closed")
		}
	}
	fixture := &sessionReadinessFixture{}
	run := entity.Run{}
	if err := attachRunSessionReadiness(t.Context(), fixture, scope{}, &run); err != nil || fixture.called || run.SessionReadiness != nil {
		t.Fatal("missing session guessed")
	}
}
