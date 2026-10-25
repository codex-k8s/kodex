package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleOriginCompleted locates only a terminal publication produced by the exact just-completed immutable node/run/revision tuple; generic historical integration paths are excluded.
// queryRepositoryBundleOriginCompleted находит лишь terminal publication exact только что завершённого immutable node/run/revision tuple; generic historical integration paths исключены.
//
//go:embed sql/repository_bundle__origin_completed.sql
var queryRepositoryBundleOriginCompleted string

// joinRepositoryBundleOrigin handles the effect-before-origin completion order under the caller's canonical root lock. Native CONTINUES uniqueness and terminal-root guards remain authoritative; no second integration effect is created.
// joinRepositoryBundleOrigin обрабатывает effect-before-origin completion под canonical root lock caller. Native CONTINUES uniqueness и terminal-root guards сохраняются; второй integration effect не создаётся.
func (repository *Repository) joinRepositoryBundleOrigin(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) error {
	rows, err := tx.Query(ctx, queryRepositoryBundleOriginCompleted, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": lease["nodeID"], "run_id": lease["runID"], "root_run_id": lease["rootRunID"], "runtime_revision_id": lease["runtimeRevisionID"]})
	if err != nil {
		return errs.ErrUnavailable
	}
	type origin struct{ invocationID, projectID string }
	var origins []origin
	for rows.Next() {
		var item origin
		if rows.Scan(&item.invocationID, &item.projectID) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		origins = append(origins, item)
	}
	rows.Close()
	if rows.Err() != nil || len(origins) > 1 {
		return errs.ErrUnavailable
	}
	for _, item := range origins {
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, item.invocationID, item.projectID); err != nil {
			return err
		}
	}
	return nil
}
