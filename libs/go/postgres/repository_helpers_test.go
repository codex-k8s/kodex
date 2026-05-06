package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunMutationRequiresAffectedRows(t *testing.T) {
	t.Parallel()

	conflict := errors.New("conflict")
	db := &fakeExecQuerier{tags: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")}}

	err := RunMutation(context.Background(), db, conflict, Mutation{
		Query:           "UPDATE projects SET version = version + 1 WHERE id = @id",
		Args:            pgx.NamedArgs{"id": 1},
		RequireAffected: true,
	})
	if !errors.Is(err, conflict) {
		t.Fatalf("RunMutation() error = %v, want conflict", err)
	}
	if len(db.calls) != 1 {
		t.Fatalf("Exec calls = %d, want 1", len(db.calls))
	}
}

func TestRunMutationPropagatesExecError(t *testing.T) {
	t.Parallel()

	execErr := errors.New("exec failed")
	db := &fakeExecQuerier{err: execErr}

	err := RunMutation(context.Background(), db, errors.New("conflict"), Mutation{
		Query: "INSERT INTO projects(id) VALUES (@id)",
		Args:  pgx.NamedArgs{"id": 1},
	})
	if !errors.Is(err, execErr) {
		t.Fatalf("RunMutation() error = %v, want exec error", err)
	}
}

func TestRunDistinctMutationsRejectsDuplicateQueryBeforeExec(t *testing.T) {
	t.Parallel()

	db := &fakeExecQuerier{}
	err := RunDistinctMutations(
		context.Background(),
		db,
		errors.New("conflict"),
		Mutation{Query: "UPDATE projects SET updated_at = @updated_at WHERE id = @id", Args: pgx.NamedArgs{"id": 1}},
		Mutation{Query: "UPDATE projects SET updated_at = @updated_at WHERE id = @id", Args: pgx.NamedArgs{"id": 2}},
	)
	if err == nil {
		t.Fatal("expected duplicate query error")
	}
	if len(db.calls) != 0 {
		t.Fatalf("Exec calls = %d, want 0 before duplicate query is fixed", len(db.calls))
	}
}

func TestRunDistinctMutationsExecutesDifferentQueries(t *testing.T) {
	t.Parallel()

	db := &fakeExecQuerier{
		tags: []pgconn.CommandTag{
			pgconn.NewCommandTag("INSERT 0 1"),
			pgconn.NewCommandTag("UPDATE 1"),
		},
	}
	err := RunDistinctMutations(
		context.Background(),
		db,
		errors.New("conflict"),
		Mutation{Query: "INSERT INTO projects(id) VALUES (@id)", Args: pgx.NamedArgs{"id": 1}, RequireAffected: true},
		Mutation{Query: "UPDATE project_outbox SET published_at = @published_at WHERE id = @id", Args: pgx.NamedArgs{"id": 2}, RequireAffected: true},
	)
	if err != nil {
		t.Fatalf("RunDistinctMutations() error = %v", err)
	}
	if len(db.calls) != 2 {
		t.Fatalf("Exec calls = %d, want 2", len(db.calls))
	}
	if db.calls[0].query == db.calls[1].query {
		t.Fatalf("expected different query texts, got %q", db.calls[0].query)
	}
}

type fakeExecQuerier struct {
	tags  []pgconn.CommandTag
	err   error
	calls []fakeExecCall
}

type fakeExecCall struct {
	query string
	args  []any
}

func (f *fakeExecQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.calls = append(f.calls, fakeExecCall{query: sql, args: args})
	if f.err != nil {
		return pgconn.CommandTag{}, f.err
	}
	if len(f.tags) == 0 {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	tag := f.tags[0]
	f.tags = f.tags[1:]
	return tag, nil
}
