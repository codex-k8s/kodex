package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/integration_health_refresh__cancel_stale.sql
var queryIntegrationHealthRefreshCancelStale string

//go:embed sql/integration_health_refresh__expire.sql
var queryIntegrationHealthRefreshExpire string

//go:embed sql/integration_health_refresh__candidates.sql
var queryIntegrationHealthRefreshCandidates string

//go:embed sql/integration_health_refresh__create.sql
var queryIntegrationHealthRefreshCreate string

// Опрос прежнего owner RPC является единственным producer новых refresh tasks.
// Успешный probe меняет только ledger, не immutable configuration/grant pins.
func enqueueManagedMCPHealthRefresh(ctx context.Context, tx pgx.Tx, current scope, limit int32) error {
	if limit < 1 || limit > 32 {
		return errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit}
	for _, query := range []string{queryIntegrationHealthRefreshCancelStale, queryIntegrationHealthRefreshExpire} {
		if _, err := tx.Exec(ctx, query, args); err != nil {
			return errs.ErrUnavailable
		}
	}
	rows, err := tx.Query(ctx, queryIntegrationHealthRefreshCandidates,
		pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	type candidate struct {
		connectionID, predecessor string
		attempt                   int
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.connectionID, &item.predecessor, &item.attempt); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range candidates {
		ref, err := newRef("tst")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryIntegrationHealthRefreshCreate, pgx.StrictNamedArgs{
			"ref": ref, "organization_id": current.organizationID, "connection_id": item.connectionID,
			"actor_id": current.actorID, "attempt": item.attempt, "predecessor_ref": item.predecessor,
		}); err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}
