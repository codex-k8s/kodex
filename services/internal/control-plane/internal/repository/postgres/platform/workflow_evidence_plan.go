package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowEvidencePlanRead reads the caller's transaction planning mode so
// the evidence selector cannot change later owner statements or a pooled session.
// queryWorkflowEvidencePlanRead читает planning mode транзакции вызывающего,
// чтобы evidence selector не менял последующие owner statements либо pooled session.
//
//go:embed sql/workflow_evidence__plan_read.sql
var queryWorkflowEvidencePlanRead string

// queryWorkflowEvidencePlanSet changes only the current transaction's planning
// strategy. It does not change authority, query predicates, budgets or database defaults.
// queryWorkflowEvidencePlanSet меняет только стратегию планирования текущей
// транзакции, сохраняя authority, query predicates, бюджеты и настройки всей БД.
//
//go:embed sql/workflow_evidence__plan_set.sql
var queryWorkflowEvidencePlanSet string

// withWorkflowEvidenceGenericPlan reuses plans within one bounded owner operation
// and restores the previous mode before later transaction work or pooled reuse.
// Read or write statements retain their original predicates and authority checks;
// only query plans are reused, never eligibility or authorization results.
// Errors propagate to the caller's mandatory rollback, including an aborted transaction.
// withWorkflowEvidenceGenericPlan переиспользует планы одной ограниченной owner
// операции и восстанавливает прежний mode до следующих команд и pooled reuse.
// Чтения и записи сохраняют predicates и authority checks; кэшируются только
// планы запросов, не eligibility или решения авторизации. Ошибки передаются
// обязательному rollback вызывающего, включая уже прерванную транзакцию.
func withWorkflowEvidenceGenericPlan(ctx context.Context, tx pgx.Tx, read func() error) error {
	var previous, active string
	if err := tx.QueryRow(ctx, queryWorkflowEvidencePlanRead).Scan(&previous); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, queryWorkflowEvidencePlanSet, pgx.StrictNamedArgs{"mode": "force_generic_plan"}).Scan(&active); err != nil {
		return err
	}
	if active != "force_generic_plan" {
		return errs.ErrUnavailable
	}
	readErr := read()
	if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
		return readErr
	}
	if err := tx.QueryRow(ctx, queryWorkflowEvidencePlanSet, pgx.StrictNamedArgs{"mode": previous}).Scan(&active); err != nil {
		return err
	}
	if active != previous {
		return errs.ErrUnavailable
	}
	return readErr
}
