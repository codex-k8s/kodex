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

// readWorkflowEvidenceWithGenericPlan reuses plans for repeated exact authority
// checks inside a single evidence selector. The original mode is restored before
// success or an empty result; failures propagate to the caller's mandatory rollback.
// Neither a cached plan nor this wrapper caches an authorization result.
// readWorkflowEvidenceWithGenericPlan переиспользует планы повторных exact
// authority checks внутри одного evidence selector. Прежний mode восстанавливается
// до успеха либо пустого результата; ошибка передаётся обязательному rollback
// вызывающего. Cached plan и эта обёртка не кэшируют результат авторизации.
func readWorkflowEvidenceWithGenericPlan(ctx context.Context, tx pgx.Tx, read func() error) error {
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
