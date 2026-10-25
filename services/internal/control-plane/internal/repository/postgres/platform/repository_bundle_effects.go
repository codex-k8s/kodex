package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// rollbackRepositoryBundle gives owner cleanup an independent bounded context even when a caller or work deadline has already cancelled the operation.
// rollbackRepositoryBundle даёт owner cleanup независимый bounded context, даже когда caller либо work deadline уже отменили операцию.
func rollbackRepositoryBundle(ctx context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

// openRepositoryBundleWork resolves exact service authority and acquires canonical nonblocking root/invocation barriers before the publication lock. Any error rolls back before returning ownership to the caller.
// openRepositoryBundleWork разрешает exact service authority и берёт canonical nonblocking root/invocation barriers до publication lock. Любая ошибка вызывает rollback до передачи ownership caller.
func (repository *Repository) openRepositoryBundleWork(ctx context.Context, principal value.Principal, action, ref string) (scope, pgx.Tx, repositoryBundleRow, string, string, error) {
	var empty repositoryBundleRow
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != "platform.repository-bundles.work."+action || principal.ProjectRef != "" {
		return scope{}, nil, empty, "", "", errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return current, nil, empty, "", "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return current, nil, empty, "", "", errs.ErrUnavailable
	}
	rootState, invocationState, _, err := repositoryBundleWorkGuard(ctx, tx, current, ref)
	if err != nil {
		rollbackRepositoryBundle(ctx, tx)
		return current, nil, empty, "", "", err
	}
	row, err := lockRepositoryBundle(ctx, tx, current, ref)
	if err != nil {
		rollbackRepositoryBundle(ctx, tx)
		return current, nil, empty, "", "", err
	}
	return current, tx, row, rootState, invocationState, nil
}

// repositoryBundleEffectDigest binds semantic candidate/source and the entire work epoch; renewed expiry is authoritative owner state and cannot mint another Begin receipt.
// repositoryBundleEffectDigest связывает semantic candidate/source и весь work epoch; renewed expiry является authoritative owner state и не создаёт новую Begin receipt.
func repositoryBundleEffectDigest(input port.RepositoryBundleEffect) string {
	input.Lease.ExpiresAt = time.Time{}
	return repositorybundle.Digest(asJSON(input))
}

// validRepositoryBundleEffect requires exact SHA1 candidate/base and full approved archive/manifest pins; a caller cannot substitute another project tree or truncated digest.
// validRepositoryBundleEffect требует exact SHA1 candidate/base и полные approved archive/manifest pins; caller не подменяет project tree либо digest prefix.
func validRepositoryBundleEffect(row repositoryBundleRow, input port.RepositoryBundleEffect) bool {
	return len(input.CandidateCommitSHA) == 40 && validSourceCommit(input.CandidateCommitSHA) && len(input.CandidateTreeSHA) == 40 && validSourceCommit(input.CandidateTreeSHA) &&
		input.BaseCommitSHA == row.proposal.BaseCommitSHA && input.ManifestSHA256 == row.proposal.Manifest.SHA256 && input.ArchiveSHA256 == row.proposal.Archive.SHA256
}

// BeginRepositoryBundle linearizes the sole candidate intent before Git. A lost commit/ACK returns an error, never a new push permission; an exact later replay returns AlreadyStarted.
// BeginRepositoryBundle фиксирует sole candidate intent до Git. Потеря commit/ACK возвращает ошибку, не новое разрешение push; exact последующий replay возвращает AlreadyStarted.
func (repository *Repository) BeginRepositoryBundle(ctx context.Context, principal value.Principal, input port.RepositoryBundleEffect) (entity.RepositoryBundlePublication, bool, error) {
	current, tx, row, _, _, err := repository.openRepositoryBundleWork(ctx, principal, "begin", input.Lease.ProposalRef)
	if err != nil {
		return entity.RepositoryBundlePublication{}, false, err
	}
	defer rollbackRepositoryBundle(ctx, tx)
	if !validRepositoryBundleEffect(row, input) {
		return entity.RepositoryBundlePublication{}, false, errs.ErrConflict
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.RepositoryBundlePublication{}, false, errs.ErrUnavailable
	}
	if !repositoryBundleLeaseMatches(row, input.Lease, now) {
		return entity.RepositoryBundlePublication{}, false, errs.ErrForbidden
	}
	digest := repositoryBundleEffectDigest(input)
	if receipt, exists := row.receipts["BEGIN"]; exists {
		if receipt.Digest != digest || row.started == nil || row.proposal.CandidateCommitSHA != input.CandidateCommitSHA || row.proposal.CandidateTreeSHA != input.CandidateTreeSHA {
			return entity.RepositoryBundlePublication{}, false, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return entity.RepositoryBundlePublication{}, false, errs.ErrUnavailable
		}
		return row.proposal, true, nil
	}
	if row.proposal.State != "CLAIMED" || row.started != nil || row.deadline == nil || !now.Before(*row.deadline) {
		return entity.RepositoryBundlePublication{}, false, errs.ErrConflict
	}
	if err := repository.requireRepositoryBundleAuthority(ctx, tx, current, row, true); err != nil {
		return entity.RepositoryBundlePublication{}, false, err
	}
	row.proposal.State, row.proposal.CandidateCommitSHA, row.proposal.CandidateTreeSHA, row.started = "EFFECT_STARTED", input.CandidateCommitSHA, input.CandidateTreeSHA, &now
	receipt := row.proposal
	receipt.Version++
	row.receipts["BEGIN"] = repositoryBundleReceipt{Digest: digest, Proposal: receipt}
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return entity.RepositoryBundlePublication{}, false, err
	}
	if tx.Commit(ctx) != nil {
		return entity.RepositoryBundlePublication{}, false, errs.ErrUnavailable
	}
	return row.proposal, false, nil
}

// RenewRepositoryBundle extends only the same active work epoch within its owner deadline; readonly UNKNOWN never becomes execution permission.
// RenewRepositoryBundle продлевает лишь тот же active work epoch в owner deadline; readonly UNKNOWN не превращается в execution permission.
func (repository *Repository) RenewRepositoryBundle(ctx context.Context, principal value.Principal, input entity.RepositoryBundleLease) (entity.RepositoryBundleLease, error) {
	current, tx, row, _, _, err := repository.openRepositoryBundleWork(ctx, principal, "renew", input.ProposalRef)
	if err != nil {
		return entity.RepositoryBundleLease{}, err
	}
	defer rollbackRepositoryBundle(ctx, tx)
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.RepositoryBundleLease{}, errs.ErrUnavailable
	}
	if !repositoryBundleLeaseMatches(row, input, now) {
		return entity.RepositoryBundleLease{}, errs.ErrForbidden
	}
	if row.proposal.State != "UNKNOWN_OUTCOME" {
		if err := repository.requireRepositoryBundleAuthority(ctx, tx, current, row, true); err != nil {
			return entity.RepositoryBundleLease{}, err
		}
	}
	row.lease.ExpiresAt = now.Add(3 * time.Minute)
	if row.proposal.State != "UNKNOWN_OUTCOME" && row.deadline != nil && row.deadline.Before(row.lease.ExpiresAt) {
		row.lease.ExpiresAt = *row.deadline
	}
	if !now.Before(row.lease.ExpiresAt) {
		return entity.RepositoryBundleLease{}, errs.ErrConflict
	}
	if row.invocationLeaseRef != "" {
		tag, err := tx.Exec(ctx, queryRepositoryBundleRenewInvocation, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": row.invocationID,
			"lease_ref": row.invocationLeaseRef, "fence_digest": repositorybundle.Digest([]byte(row.lease.Fence)), "generation": row.lease.Generation, "expires_at": row.lease.ExpiresAt})
		if err != nil {
			return entity.RepositoryBundleLease{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return entity.RepositoryBundleLease{}, errs.ErrConflict
		}
	}
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return entity.RepositoryBundleLease{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.RepositoryBundleLease{}, errs.ErrUnavailable
	}
	return row.lease, nil
}
