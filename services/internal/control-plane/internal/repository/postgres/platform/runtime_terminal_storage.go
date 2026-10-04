package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_claim__select_terminal_storage.sql
var queryRuntimeClaimTerminalStorage string

// Terminal storage проверяется до полного eligibility join: отсутствующая
// credential/configuration не должна скрыть уже неисполняемый owner graph.
func (repository *Repository) reconcileTerminalSessionStorage(ctx context.Context, tx pgx.Tx, current scope, input command.Command, limit int32) ([]claimableExecution, error) {
	rows, err := tx.Query(ctx, queryRuntimeClaimTerminalStorage, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "limit": limit,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var candidates []claimableExecution
	for rows.Next() {
		var candidate claimableExecution
		if err := rows.Scan(&candidate.nodeID, &candidate.nodeRef, &candidate.runID, &candidate.runRef,
			&candidate.rootRunID, &candidate.projectID, &candidate.projectRef, &candidate.sessionID,
			&candidate.sessionRef, &candidate.stableKey); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	seen := make(map[string]bool)
	var failed []claimableExecution
	for _, candidate := range candidates {
		if seen[candidate.rootRunID] {
			continue
		}
		if err := repository.failRuntimeCandidateGraph(ctx, tx, current, input, candidate); err != nil {
			return nil, err
		}
		seen[candidate.rootRunID] = true
		failed = append(failed, candidate)
	}
	return failed, nil
}
