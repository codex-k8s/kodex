package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// workflowExceptionTestTx supplies only native savepoint calls exercised by the cleanup policy.
// workflowExceptionTestTx предоставляет только native savepoint calls, проверяемые cleanup policy.
type workflowExceptionTestTx struct {
	pgx.Tx
	child                                *workflowExceptionTestTx
	rowError, rollbackError, commitError error
	commits, rollbacks                   int
	cleanupCanceled                      bool
}

// Begin returns a distinct child so tests cannot confuse releasing a savepoint with owner commit.
// Begin возвращает отдельного child, не позволяя спутать release savepoint с owner commit.
func (tx *workflowExceptionTestTx) Begin(context.Context) (pgx.Tx, error) { return tx.child, nil }

// Commit records only the savepoint release outcome.
// Commit учитывает только результат release savepoint.
func (tx *workflowExceptionTestTx) Commit(context.Context) error { tx.commits++; return tx.commitError }

// Rollback observes whether cleanup received an independent active context.
// Rollback отмечает, получил ли cleanup независимый активный context.
func (tx *workflowExceptionTestTx) Rollback(ctx context.Context) error {
	tx.rollbacks++
	tx.cleanupCanceled = ctx.Err() != nil
	return tx.rollbackError
}

// QueryRow postpones a driver error until Scan, as native pgx does.
// QueryRow откладывает driver error до Scan, как native pgx.
func (tx *workflowExceptionTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return workflowExceptionTestRow{err: tx.rowError}
}

// workflowExceptionTestRow retains no payload or SQL arguments.
// workflowExceptionTestRow не хранит payload или SQL arguments.
type workflowExceptionTestRow struct{ err error }

// Scan returns the independent driver outcome selected by the test.
// Scan возвращает независимый driver outcome, выбранный тестом.
func (row workflowExceptionTestRow) Scan(...any) error { return row.err }

// TestWorkflowConfigurationSavepoint proves that only a rolled-back eligibility failure becomes an
// exception; masked SQL, unknown release and rollback errors never gain this fallback authority.
// TestWorkflowConfigurationSavepoint доказывает, что только откатившийся eligibility failure становится
// exception; скрытые SQL, неизвестные release и rollback errors не получают fallback authority.
func TestWorkflowConfigurationSavepoint(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		failure, row, rollback, commit error
		expected                       bool
		want                           error
	}{
		{name: "success"},
		{name: "missing configuration", failure: errs.ErrConflict, expected: true},
		{name: "missing candidate", failure: errs.ErrConflict, row: pgx.ErrNoRows, expected: true},
		{name: "masked database", failure: errs.ErrConflict, row: &pgconn.PgError{Code: "42703"}, want: errs.ErrUnavailable},
		{name: "database unavailable", failure: errs.ErrUnavailable, want: errs.ErrUnavailable},
		{name: "unknown rollback", failure: errs.ErrConflict, rollback: errors.New("rollback unavailable"), want: errs.ErrUnavailable},
		{name: "unknown release", commit: errors.New("release unavailable"), want: errs.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &workflowExceptionTestTx{rowError: tc.row, rollbackError: tc.rollback, commitError: tc.commit}
			parent := &workflowExceptionTestTx{child: child}
			expected, err := workflowConfigurationAttemptTx(t.Context(), parent, func(tx pgx.Tx) error {
				if tc.row != nil {
					_ = tx.QueryRow(t.Context(), workflowExceptionAgent).Scan(new(string))
				}
				return tc.failure
			})
			if expected != tc.expected || (tc.want == nil && err != nil) || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("unexpected savepoint classification: expected=%t error=%v", expected, err)
			}
			if child.cleanupCanceled {
				t.Fatal("rollback used a canceled context")
			}
			if tc.failure != nil && child.rollbacks != 1 || tc.failure == nil && child.commits != 1 {
				t.Fatal("savepoint was not closed exactly once")
			}
		})
	}
	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		child := &workflowExceptionTestTx{}
		parent := &workflowExceptionTestTx{child: child}
		expected, err := workflowConfigurationAttemptTx(ctx, parent, func(pgx.Tx) error { cancel(); return errs.ErrConflict })
		if expected || !errors.Is(err, context.Canceled) || child.cleanupCanceled || child.rollbacks != 1 {
			t.Fatal("cancellation gained fallback or skipped bounded cleanup")
		}
	})
}
