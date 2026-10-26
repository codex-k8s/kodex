package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowAttemptAppend constructs private execution pins from completed owner rows and appends
// exactly one business outcome. It cannot accept model-supplied lineage or replace earlier outcomes.
// queryWorkflowAttemptAppend строит приватное происхождение по завершённым owner rows и добавляет
// ровно один бизнес-исход. Запрос не принимает происхождение от модели и не заменяет прежние исходы.
//
//go:embed sql/workflow_attempt__append.sql
var queryWorkflowAttemptAppend string

// appendWorkflowAttemptTx persists an already evaluated business outcome inside its completion
// transaction. The caller must first check all pending-work barriers and result predicates; this
// storage boundary alone neither accepts a model declaration nor advances the workflow.
// appendWorkflowAttemptTx сохраняет уже проверенный бизнес-исход внутри транзакции завершения.
// Вызывающий сначала проверяет незавершённую работу и обязательные результаты; эта граница хранения
// сама не принимает заявление модели и не запускает следующий шаг процесса.
func appendWorkflowAttemptTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any,
	accepted bool, reason string, results []entity.WorkflowAcceptedResult,
) (entity.WorkflowAttemptResult, error) {
	var result entity.WorkflowAttemptResult
	if results == nil {
		results = []entity.WorkflowAcceptedResult{}
	}
	err := tx.QueryRow(ctx, queryWorkflowAttemptAppend, pgx.StrictNamedArgs{
		"organization_id":     current.organizationID,
		"node_id":             stringMap(lease, "nodeID"),
		"root_run_id":         stringMap(lease, "rootRunID"),
		"runtime_revision_id": stringMap(lease, "runtimeRevisionID"),
		"lease_id":            stringMap(lease, "leaseID"),
		"accepted":            accepted,
		"reason":              reason,
		"results":             results,
	}).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkflowAttemptResult{}, errs.ErrConflict
	}
	if err != nil {
		return entity.WorkflowAttemptResult{}, errs.ErrUnavailable
	}
	return result, nil
}
