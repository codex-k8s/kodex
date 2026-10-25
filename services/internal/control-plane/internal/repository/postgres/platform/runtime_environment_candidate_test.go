package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// environmentCandidateRow returns one controlled binding identity or a read failure without storage effects.
// environmentCandidateRow возвращает один управляемый binding identity либо read failure без storage effects.
type environmentCandidateRow struct {
	ref string
	err error
}

// Scan supplies the exact owner result while keeping absence distinct from storage errors.
// Scan передаёт точный owner result, сохраняя различие absence и storage errors.
func (row environmentCandidateRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*string) = row.ref
	return nil
}

// environmentCandidateTx observes the small savepoint boundary; unsupported mutations have no implementation.
// environmentCandidateTx наблюдает малую savepoint boundary; неподдержанные mutations не реализованы.
type environmentCandidateTx struct {
	pgx.Tx
	t                                 *testing.T
	child                             *environmentCandidateTx
	row                               environmentCandidateRow
	beginErr, rollbackErr, commitErr  error
	begins, commits, rollbacks, reads int
}

// Begin exposes only the configured nested transaction or a controlled failure.
// Begin раскрывает лишь настроенную вложенную transaction либо управляемый failure.
func (tx *environmentCandidateTx) Begin(context.Context) (pgx.Tx, error) {
	tx.begins++
	return tx.child, tx.beginErr
}

// QueryRow checks every immutable candidate/owner pin before returning the synthetic binding decision.
// QueryRow проверяет каждый immutable candidate/owner pin перед возвратом синтетического binding decision.
func (tx *environmentCandidateTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	tx.reads++
	want := pgx.StrictNamedArgs{"organization_id": "tenant", "project_id": "project", "agent_id": "agent", "environment_ref": "renv",
		"environment_version_id": "version-id", "environment_digest": "environment-digest", "binding_id": "binding-id",
		"binding_ref": "aenv", "binding_version": int64(4), "binding_digest": "binding-digest"}
	if sql != queryRuntimeClaimEnvironmentCandidate || len(args) != 1 || !reflect.DeepEqual(args[0], want) {
		tx.t.Fatal("candidate guard lost exact immutable owner pins")
	}
	return tx.row
}

// Commit records release of the successful savepoint while the outer transaction retains its row locks.
// Commit учитывает release успешного savepoint, чьи row locks сохраняет внешняя transaction.
func (tx *environmentCandidateTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

// Rollback records release of partial guard locks and never manufactures a successful commit.
// Rollback учитывает освобождение частичных guard locks без создания успешного commit.
func (tx *environmentCandidateTx) Rollback(context.Context) error {
	tx.rollbacks++
	return tx.rollbackErr
}

// TestRuntimeEnvironmentCandidateGuard keeps stale/contended absence transient, preserves exact pins and fails closed on SQL/savepoint failures.
// TestRuntimeEnvironmentCandidateGuard сохраняет transient absence при stale/contended candidate, exact pins и закрытый отказ при SQL/savepoint failures.
func TestRuntimeEnvironmentCandidateGuard(t *testing.T) {
	failure := errors.New("synthetic guard failure")
	for _, item := range []struct {
		name                             string
		row                              environmentCandidateRow
		beginErr, rollbackErr, commitErr error
		eligible                         bool
		want                             error
		commits, rollbacks               int
	}{
		{name: "exact pinned binding", row: environmentCandidateRow{ref: "aenv"}, eligible: true, commits: 1},
		{name: "missing changed or contended row", row: environmentCandidateRow{err: pgx.ErrNoRows}, rollbacks: 1},
		{name: "SQL failure", row: environmentCandidateRow{err: failure}, want: errs.ErrUnavailable, rollbacks: 1},
		{name: "substituted identity", row: environmentCandidateRow{ref: "foreign"}, want: errs.ErrUnavailable, rollbacks: 1},
		{name: "begin failure", beginErr: failure, want: errs.ErrUnavailable},
		{name: "rollback failure", row: environmentCandidateRow{err: pgx.ErrNoRows}, rollbackErr: failure, want: errs.ErrUnavailable, rollbacks: 1},
		{name: "release failure", row: environmentCandidateRow{ref: "aenv"}, commitErr: failure, want: errs.ErrUnavailable, commits: 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			guard := &environmentCandidateTx{t: t, row: item.row, commitErr: item.commitErr, rollbackErr: item.rollbackErr}
			outer := &environmentCandidateTx{t: t, child: guard, beginErr: item.beginErr}
			candidate := claimableExecution{projectID: "project", agentID: "agent", runtimeEnvironmentRef: "renv", runtimeEnvironmentID: "version-id", runtimeEnvironmentDigest: "environment-digest",
				environmentBindingID: "binding-id", environmentBindingRef: "aenv", environmentBindingVersion: 4, environmentBindingDigest: "binding-digest"}
			eligible, err := retainRuntimeEnvironmentCandidate(t.Context(), outer, "tenant", candidate)
			if eligible != item.eligible || !errors.Is(err, item.want) || outer.begins != 1 || guard.commits != item.commits || guard.rollbacks != item.rollbacks {
				t.Fatalf("guard eligible=%t err=%v commits=%d rollbacks=%d", eligible, err, guard.commits, guard.rollbacks)
			}
			if item.beginErr != nil && guard.reads != 0 || item.beginErr == nil && guard.reads != 1 {
				t.Fatal("guard query escaped its savepoint boundary")
			}
		})
	}
}
