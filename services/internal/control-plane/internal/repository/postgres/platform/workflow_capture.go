package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkflowCaptureContext identifies an actual pinned DAG step without treating legacy or
	// coordinator executions as business attempts. It does not assign an execution identity.
	// queryWorkflowCaptureContext определяет закреплённый шаг DAG, не превращая прежние процессы
	// и координатора в бизнес-попытки. Запрос не назначает identity исполнения.
	//go:embed sql/workflow_capture__context.sql
	queryWorkflowCaptureContext string
	// queryWorkflowCaptureRecord constructs capture provenance from the completed owner rows and
	// exact inserted artifact IDs. Caller metadata never supplies producing revision or file pins.
	// queryWorkflowCaptureRecord строит provenance по завершённым owner rows и точным ID вставленных
	// артефактов. Метаданные вызывающего не задают producing revision или file pins.
	//go:embed sql/workflow_capture__record.sql
	queryWorkflowCaptureRecord string
)

// captureWorkflowCompletionTx records one immutable batch, including an explicitly empty batch,
// only for a server-assigned physical DAG step. It runs after content inserts in the existing
// completion transaction; replay is handled by the enclosing idempotency receipt before leasing.
// captureWorkflowCompletionTx сохраняет один неизменяемый batch, включая явно пустой, только для
// назначенного сервером physical шага DAG. Вызывается после content inserts в общей completion
// transaction; повтор обслуживает внешняя idempotency receipt до проверки lease.
func captureWorkflowCompletionTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, artifactIDs []string) error {
	var required bool
	var businessAttempt int32
	if err := tx.QueryRow(ctx, queryWorkflowCaptureContext, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"node_id":         stringMap(lease, "nodeID"),
	}).Scan(&required, &businessAttempt); err != nil {
		return errs.ErrUnavailable
	}
	if !required {
		return nil
	}
	if businessAttempt < 1 || businessAttempt > 4 || len(artifactIDs) > 16 {
		return errs.ErrConflict
	}
	if artifactIDs == nil {
		artifactIDs = []string{}
	}
	tag, err := tx.Exec(ctx, queryWorkflowCaptureRecord, pgx.StrictNamedArgs{
		"organization_id":     current.organizationID,
		"node_id":             stringMap(lease, "nodeID"),
		"run_id":              stringMap(lease, "runID"),
		"root_run_id":         stringMap(lease, "rootRunID"),
		"lease_id":            stringMap(lease, "leaseID"),
		"runtime_revision_id": stringMap(lease, "runtimeRevisionID"),
		"artifact_ids":        artifactIDs,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
