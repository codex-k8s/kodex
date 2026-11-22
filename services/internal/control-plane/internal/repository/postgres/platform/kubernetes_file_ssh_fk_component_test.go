package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryFileOwnerSSHConstraints inspects only the four named canonical FK default modes.
// RU: queryFileOwnerSSHConstraints проверяет только default modes четырёх named canonical FK.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_constraints.sql
var queryFileOwnerSSHConstraints string

// queryFileOwnerSSHOrphan attempts a bounded invalid receipt solely in the disposable owner fixture.
// RU: queryFileOwnerSSHOrphan пытается записать bounded invalid receipt только в disposable owner fixture.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_orphan.sql
var queryFileOwnerSSHOrphan string

// queryFileOwnerSSHOrphanAbsent checks rollback of that exact synthetic row without reading other receipts.
// RU: queryFileOwnerSSHOrphanAbsent проверяет rollback exact synthetic row, не читая другие receipts.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_orphan_absent.sql
var queryFileOwnerSSHOrphanAbsent string

// queryFileOwnerSSHDefer explicitly requests transaction-local checking for the one named fixture FK.
// RU: queryFileOwnerSSHDefer явно запрашивает transaction-local checking для одного named fixture FK.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_defer.sql
var queryFileOwnerSSHDefer string

// queryFileOwnerSSHBindingDeleteDenied tests ordinary runtime denial, never the private canonical purge context.
// RU: queryFileOwnerSSHBindingDeleteDenied проверяет ordinary runtime denial, не private canonical purge context.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_binding_delete_denied.sql
var queryFileOwnerSSHBindingDeleteDenied string

// queryFileOwnerSSHIntentDeleteDenied tests the unchanged runtime boundary for execution intents.
// RU: queryFileOwnerSSHIntentDeleteDenied проверяет неизменённую runtime boundary execution intents.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_intent_delete_denied.sql
var queryFileOwnerSSHIntentDeleteDenied string

// queryFileOwnerSSHObservationDeleteDenied tests the unchanged runtime boundary for immutable observations.
// RU: queryFileOwnerSSHObservationDeleteDenied проверяет неизменённую runtime boundary immutable observations.
//
//go:embed testdata/sql/kubernetes_file_owner_ssh_observation_delete_denied.sql
var queryFileOwnerSSHObservationDeleteDenied string

// assertFileProjectSSHFKBoundary checks immediate defaults, deferred commit failure and ordinary delete denial.
// Its invalid receipt never commits, grants authority or changes the positive FILE/project lifecycle.
// RU: assertFileProjectSSHFKBoundary проверяет immediate defaults, deferred commit failure и ordinary delete denial.
// Invalid receipt не фиксируется, не выдаёт authority и не меняет positive FILE/project lifecycle.
func assertFileProjectSSHFKBoundary(t *testing.T, ctx context.Context, repository *Repository, current scope) {
	t.Helper()
	var exact bool
	if err := repository.pool.QueryRow(ctx, queryFileOwnerSSHConstraints).Scan(&exact); err != nil || !exact {
		t.Fatal("SSH project foreign keys lost immediate defaults", err)
	}
	for _, statement := range []string{queryFileOwnerSSHBindingDeleteDenied, queryFileOwnerSSHIntentDeleteDenied, queryFileOwnerSSHObservationDeleteDenied} {
		_, err := repository.pool.Exec(ctx, statement)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("ordinary runtime could delete immutable SSH receipts")
		}
	}
	intent := uuid.NewString()
	_, err := repository.pool.Exec(ctx, queryFileOwnerSSHOrphan, intent, current.organizationID)
	var immediate *pgconn.PgError
	if !errors.As(err, &immediate) || immediate.Code != "23503" {
		t.Fatal("ordinary SSH receipt insert bypassed immediate foreign key")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("open deferred SSH receipt fixture")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, queryFileOwnerSSHDefer); err != nil {
		t.Fatal("defer exact SSH receipt foreign key", err)
	}
	if _, err := tx.Exec(ctx, queryFileOwnerSSHOrphan, intent, current.organizationID); err != nil {
		t.Fatal("deferred SSH fixture did not reach commit boundary", err)
	}
	var rejected *pgconn.PgError
	if err := tx.Commit(ctx); !errors.As(err, &rejected) || rejected.Code != "23503" {
		t.Fatal("deferred SSH orphan receipt committed")
	}
	var retained int64
	if err := repository.pool.QueryRow(ctx, queryFileOwnerSSHOrphanAbsent, intent, current.organizationID).Scan(&retained); err != nil || retained != 0 {
		t.Fatal("failed deferred SSH receipt left durable state", err)
	}
}
