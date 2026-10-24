package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// runtimeContinuationTx exposes only the fixture's exact query/exec callbacks and caller context;
// unused embedded transaction methods remain nil so unexpected commits or other effects fail.
// runtimeContinuationTx предоставляет лишь точные query/exec callbacks fixture и caller context;
// неиспользуемые методы вложенной transaction остаются nil, чтобы неожиданные commits/effects падали.
type runtimeContinuationTx struct {
	pgx.Tx
	t     *testing.T
	ctx   context.Context
	query func(string, ...any) pgx.Row
	exec  func(string, ...any) (pgconn.CommandTag, error)
}

// QueryRow checks context identity before passing the exact SQL and arguments to the fixture.
// QueryRow сверяет context identity до передачи точных SQL и arguments в fixture.
func (tx *runtimeContinuationTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	tx.t.Helper()
	if ctx != tx.ctx {
		tx.t.Fatal("continuation query changed caller context")
	}
	return tx.query(query, args...)
}

// Exec checks context identity before exposing an ordered session-counter write to the fixture.
// Exec сверяет context identity до передачи упорядоченной session-counter записи в fixture.
func (tx *runtimeContinuationTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.t.Helper()
	if ctx != tx.ctx {
		tx.t.Fatal("continuation write changed caller context")
	}
	return tx.exec(query, args...)
}

// TestRuntimeContinuationSharedStepsPreserveSessionBoundary checks exact tenant/run/session pins,
// identity drift, row failures, historical zero counters, insert-before-counter ordering and SQL
// failure short-circuiting without a database. It preserves content bytes and the existing
// zero-affected-row policy; nil unused transaction methods also prohibit hidden commit/rollback.
// TestRuntimeContinuationSharedStepsPreserveSessionBoundary проверяет точные tenant/run/session
// pins, identity drift, row failures, прежние zero counters, insert-before-counter порядок и
// остановку при SQL failure без БД. Сохраняет content bytes и прежнюю zero-affected-row policy;
// nil неиспользуемые transaction methods также запрещают скрытые commit/rollback.
func TestRuntimeContinuationSharedStepsPreserveSessionBoundary(t *testing.T) {
	driverFailure := errors.New("synthetic private driver detail")
	for _, test := range []struct {
		name, session string
		number        int64
		rowError      error
		want          int64
		failed        bool
	}{
		{name: "exact", session: "session-owned", number: 9, want: 9},
		{name: "zero-counter", session: "session-owned", number: 0, want: 0},
		{name: "foreign-session", session: "session-foreign", number: 9, failed: true},
		{name: "missing-session", session: "", number: 9, failed: true},
		{name: "no-row", rowError: pgx.ErrNoRows, failed: true},
		{name: "driver-error", session: "session-owned", number: 9, rowError: driverFailure, failed: true},
	} {
		t.Run("lock/"+test.name, func(t *testing.T) {
			calls := 0
			tx := &runtimeContinuationTx{t: t, ctx: t.Context()}
			tx.query = func(query string, args ...any) pgx.Row {
				calls++
				if query != queryRuntimeCallbackSelectParentSession || !reflect.DeepEqual(args, []any{pgx.StrictNamedArgs{"organization_id": "org-owned", "parent_run_id": "parent-owned"}}) {
					t.Fatal("continuation session query lost exact owner/run pins")
				}
				return proofResolutionRow(func(dest ...any) error {
					if len(dest) != 2 {
						t.Fatal("continuation session projection changed")
					}
					*dest[0].(*string), *dest[1].(*int64) = test.session, test.number
					return test.rowError
				})
			}
			got, err := lockRuntimeContinuationSession(tx.ctx, tx, "org-owned", "parent-owned", "session-owned")
			if calls != 1 || got != test.want || test.failed && !errors.Is(err, errs.ErrUnavailable) || !test.failed && err != nil {
				t.Fatal("continuation session boundary changed")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("continuation session disclosed driver detail")
			}
		})
	}
	for _, test := range []struct {
		name                  string
		scanError, writeError error
		tag                   string
		failed                bool
		writes                int
	}{
		{name: "success", tag: "UPDATE 1", writes: 1},
		{name: "zero-affected-preserved", tag: "UPDATE 0", writes: 1},
		{name: "insert-failure", scanError: driverFailure, failed: true, writes: 0},
		{name: "counter-failure", writeError: driverFailure, failed: true, writes: 1},
	} {
		t.Run("insert/"+test.name, func(t *testing.T) {
			content := "  Exact callback result\nДанные <&>\n"
			stage, writes := 0, 0
			tx := &runtimeContinuationTx{t: t, ctx: t.Context()}
			tx.query = func(query string, args ...any) pgx.Row {
				if stage != 0 || query != queryRuntimeCallbackInsertContinuationTurn || len(args) != 1 {
					t.Fatal("continuation insert sequence changed")
				}
				actual, ok := args[0].(pgx.StrictNamedArgs)
				if !ok || !strings.HasPrefix(stringValue(actual["turn_ref"]), "trn_") {
					t.Fatal("continuation turn identity missing")
				}
				expected := pgx.StrictNamedArgs{"turn_ref": actual["turn_ref"], "organization_id": "org-owned", "session_id": "session-owned", "parent_run_id": "parent-owned", "turn_number": int64(9), "agent_ref": "agent-owned", "content": content}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatal("continuation insert changed owner/sequence/content pins")
				}
				stage = 1
				return proofResolutionRow(func(dest ...any) error {
					if len(dest) != 1 {
						t.Fatal("continuation turn result changed")
					}
					*dest[0].(*string) = "turn-created"
					return test.scanError
				})
			}
			tx.exec = func(query string, args ...any) (pgconn.CommandTag, error) {
				writes++
				if stage != 1 || query != queryRuntimeCallbackUpdateSession || !reflect.DeepEqual(args, []any{pgx.StrictNamedArgs{"session_id": "session-owned"}}) {
					t.Fatal("continuation counter advanced before exact turn insert")
				}
				stage = 2
				return pgconn.NewCommandTag(test.tag), test.writeError
			}
			got, err := insertRuntimeContinuationTurn(tx.ctx, tx, "org-owned", "parent-owned", "session-owned", "agent-owned", 9, content)
			if writes != test.writes || test.failed && (!errors.Is(err, errs.ErrUnavailable) || got != "") || !test.failed && (err != nil || got != "turn-created") {
				t.Fatal("continuation insert failure boundary changed")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("continuation insert disclosed driver detail")
			}
		})
	}
}
