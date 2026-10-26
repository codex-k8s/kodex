package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryLegacyReworkReadJoin resolves only native gate-backed READs of one exact owned physical node.
// queryLegacyReworkReadJoin разрешает только native Gate-backed READ точного owned physical node.
//
//go:embed sql/legacy_workflow_rework__read_join.sql
var queryLegacyReworkReadJoin string

// queryLegacyReworkReadSlot preserves one native gated continuation slot while allowing an exact invocation replay.
// queryLegacyReworkReadSlot сохраняет один native gated continuation slot, разрешая точный invocation replay.
//
//go:embed sql/legacy_workflow_rework__read_slot.sql
var queryLegacyReworkReadSlot string

// joinLegacyReworkReadTx retains a pending owner decision or joins its terminal READ before the result gate/callback; it never issues an external operation.
// joinLegacyReworkReadTx удерживает pending owner decision либо соединяет terminal READ до result Gate/callback, не выполняя внешнюю операцию.
func (repository *Repository) joinLegacyReworkReadTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) (bool, bool, error) {
	rows, err := tx.Query(ctx, queryLegacyReworkReadJoin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": lease["nodeID"]})
	if err != nil {
		return false, false, errs.ErrUnavailable
	}
	count, invocationID, state := 0, "", ""
	for rows.Next() {
		count++
		if rows.Scan(&invocationID, &state) != nil {
			rows.Close()
			return false, false, errs.ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, false, errs.ErrUnavailable
	}
	if count == 0 {
		return false, false, nil
	}
	if count != 1 {
		return false, false, errs.ErrLegacyReworkDeliveryBlocked
	}
	if _, err := tx.Exec(ctx, queryRuntimeIntegrationResumeChildRun, pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_id": lease["runID"]}); err != nil {
		return false, false, errs.ErrUnavailable
	}
	continued := false
	if contains([]string{"SUCCEEDED", "FAILED", "REJECTED", "CANCELLED"}, state) {
		continued, err = repository.scheduleIntegrationContinuation(ctx, tx, current, invocationID, stringMap(lease, "projectID"))
		if err != nil {
			return false, false, err
		}
		if !continued {
			return false, false, errs.ErrLegacyReworkDeliveryBlocked
		}
	}
	return true, continued, nil
}
