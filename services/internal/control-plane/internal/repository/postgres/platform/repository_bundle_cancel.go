package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleDependency selects only this kind's active owner rows for one authorized source mutation, in canonical root/invocation order and with a bounded cardinality witness.
// queryRepositoryBundleDependency выбирает лишь active owner rows этого kind для authorized source mutation в canonical root/invocation order с bounded cardinality witness.
//
//go:embed sql/repository_bundle__dependency.sql
var queryRepositoryBundleDependency string

// cancelRepositoryBundleDependency joins the existing authorized dependency mutation transaction. Pre-Begin work is known cancelled; every begun effect remains UNKNOWN and only its old lease is revoked. More than 256 consumers fails before any closure and rolls back the source mutation.
// cancelRepositoryBundleDependency участвует в прежней authorized dependency mutation transaction. Pre-Begin work отменяется как known no-effect; begun effect остаётся UNKNOWN с отзывом лишь старой lease. Более256 consumers отклоняются до closure с rollback source mutation.
func (repository *Repository) cancelRepositoryBundleDependency(ctx context.Context, tx pgx.Tx, current scope, kind, ref string) error {
	if kind != "CONNECTION" && kind != "AGENT" && kind != "ARTIFACT" || ref == "" {
		return errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryRepositoryBundleDependency, pgx.StrictNamedArgs{"organization_id": current.organizationID, "source_kind": kind, "source_ref": ref})
	if err != nil {
		return errs.ErrUnavailable
	}
	var refs []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	if len(refs) > 256 {
		return errs.ErrConflict
	}
	if len(refs) == 0 {
		return nil
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return errs.ErrUnavailable
	}
	for _, ref := range refs {
		if _, _, _, err := repositoryBundleWorkGuard(ctx, tx, current, ref); err != nil {
			return err
		}
		row, err := lockRepositoryBundle(ctx, tx, current, ref)
		if err != nil {
			return err
		}
		if repositoryBundleTerminal(row.proposal.State) {
			continue
		}
		if row.started == nil {
			if err := repository.finishRepositoryBundleUnstarted(ctx, tx, current, &row, "AUTHORITY_CHANGED", false, now); err != nil {
				return err
			}
			continue
		}
		if row.invocationLeaseRef != "" {
			if _, err := tx.Exec(ctx, queryRepositoryBundleClearInvocationLease, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": row.invocationID, "lease_ref": row.invocationLeaseRef, "fence_digest": repositorybundle.Digest([]byte(row.lease.Fence)), "generation": row.lease.Generation}); err != nil {
				return errs.ErrUnavailable
			}
		}
		row.proposal.State, row.proposal.FailureCode = "UNKNOWN_OUTCOME", "AUTHORITY_CHANGED"
		row.lease.Claimant, row.lease.Fence, row.lease.ExpiresAt, row.invocationLeaseRef = "", "", time.Time{}, ""
		if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
			return err
		}
	}
	return nil
}
