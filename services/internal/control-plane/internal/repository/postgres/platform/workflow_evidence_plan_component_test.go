package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestWorkflowEvidencePlanScopeComponent holds the exact same physical connection
// across success, empty selection, a real SQL failure and transaction end. It
// proves that a bounded selector cannot leak its planning mode to later commands
// or pooled callers; no business rows, external services or live settings change.
// TestWorkflowEvidencePlanScopeComponent удерживает одно physical connection
// при успехе, пустой выборке, настоящем SQL-отказе и завершении транзакции.
// Проверяется отсутствие утечки planning mode в последующие команды либо pooled
// callers; business rows, внешние сервисы и live settings не меняются.
func TestWorkflowEvidencePlanScopeComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool := newTracedOrganizationComponentPool(t, ctx)
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquire disposable planner connection")
	}
	defer connection.Release()
	var baseline string
	if err := connection.QueryRow(ctx, queryWorkflowEvidencePlanRead).Scan(&baseline); err != nil {
		t.Fatal("read disposable baseline planner")
	}
	for _, planner := range []string{"auto", "force_custom_plan", "force_generic_plan"} {
		for _, outcome := range []string{"success", "empty", "sql-failure"} {
			t.Run(planner+"/"+outcome, func(t *testing.T) {
				tx, err := connection.Begin(ctx)
				if err != nil {
					t.Fatal("begin disposable planner transaction")
				}
				defer func() { _ = tx.Rollback(ctx) }()
				var mode string
				if err := tx.QueryRow(ctx, queryWorkflowEvidencePlanSet, pgx.StrictNamedArgs{"mode": planner}).Scan(&mode); err != nil || mode != planner {
					t.Fatal("set disposable caller planner")
				}
				err = readWorkflowEvidenceWithGenericPlan(ctx, tx, func() error {
					if err := tx.QueryRow(ctx, queryWorkflowEvidencePlanRead).Scan(&mode); err != nil {
						return err
					}
					if mode != "force_generic_plan" {
						t.Fatal("evidence selector retained repeated custom planning")
					}
					var value int
					switch outcome {
					case "empty":
						return tx.QueryRow(ctx, "SELECT 1 WHERE false").Scan(&value)
					case "sql-failure":
						return tx.QueryRow(ctx, "SELECT 1/0").Scan(&value)
					default:
						return tx.QueryRow(ctx, "SELECT 1").Scan(&value)
					}
				})
				if outcome == "sql-failure" {
					var failure *pgconn.PgError
					if !errors.As(err, &failure) || failure.Code != "22012" {
						t.Fatal("evidence selector changed SQL rejection semantics")
					}
					if err := tx.Rollback(ctx); err != nil {
						t.Fatal("rollback rejected evidence transaction")
					}
				} else {
					if outcome == "empty" && !errors.Is(err, pgx.ErrNoRows) || outcome == "success" && err != nil {
						t.Fatal("evidence selector changed empty or successful read")
					}
					if err := tx.QueryRow(ctx, queryWorkflowEvidencePlanRead).Scan(&mode); err != nil || mode != planner {
						t.Fatal("evidence selector changed later transaction planning")
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal("commit disposable planner transaction")
					}
				}
				if err := connection.QueryRow(ctx, queryWorkflowEvidencePlanRead).Scan(&mode); err != nil || mode != baseline {
					t.Fatal("evidence selector leaked planning mode to pooled connection")
				}
			})
		}
	}
}
