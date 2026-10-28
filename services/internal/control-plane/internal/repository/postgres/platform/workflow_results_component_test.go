package platform

import (
	"context"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// assertWorkflowBusinessResult probes the real evaluator after native capture/lease completion
// while the workflow root is active. It derives identities through the existing owner fixture,
// rolls back all reader locks and never inserts owner state or substitutes receipt evidence.
// This helper is an integration point for the dispatch component suite, not a native PASS claim.
// assertWorkflowBusinessResult проверяет настоящий evaluator после native capture/lease completion
// при активном workflow root. Identity читает existing owner fixture; helper откатывает read locks,
// не вставляет owner state и не подменяет receipt evidence. Это точка подключения component suite,
// а не заявление об успешном native прогоне.
func assertWorkflowBusinessResult(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	nodeRef, leaseRef string, payload command.CompleteExecutionInput,
	accepted []entity.WorkflowAcceptedResult, reason string,
) {
	t.Helper()
	current, lease, _ := workflowJournalFixture(t, ctx, pool, nodeRef, leaseRef)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin isolated business result probe")
	}
	actual, rejection, evaluateErr := (&Repository{}).evaluateWorkflowStepResultTx(ctx, tx, current, lease, payload)
	rollbackWorkflowJournalProbe(t, ctx, tx)
	if evaluateErr != nil || rejection != reason || !reflect.DeepEqual(actual, accepted) {
		t.Fatal("unexpected owner business result evaluation")
	}
}
