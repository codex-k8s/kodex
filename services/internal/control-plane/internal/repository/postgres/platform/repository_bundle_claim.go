package platform

import (
	"context"
	_ "embed"
	"errors"
	"math"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleClaimInvocation binds the existing native invocation to the same privately issued work epoch; generic workers cannot claim this operation.
// queryRepositoryBundleClaimInvocation связывает native invocation с тем же private work epoch; generic workers эту operation не получают.
//
//go:embed sql/repository_bundle__claim_invocation.sql
var queryRepositoryBundleClaimInvocation string

// queryRepositoryBundleClearInvocationLease releases only the exact formerly issued native lease into read-only uncertainty, without assigning a new write attempt.
// queryRepositoryBundleClearInvocationLease освобождает лишь exact прежнюю native lease в read-only uncertainty, не назначая новой write attempt.
//
//go:embed sql/repository_bundle__clear_invocation_lease.sql
var queryRepositoryBundleClearInvocationLease string

// queryRepositoryBundleRenewInvocation retains native and specialized lease deadlines together so completion cannot race an expired shadow lease.
// queryRepositoryBundleRenewInvocation совместно сохраняет native и specialized lease deadlines, исключая completion с expired shadow lease.
//
//go:embed sql/repository_bundle__renew_invocation.sql
var queryRepositoryBundleRenewInvocation string

// queryRepositoryBundleFinishUnstarted closes a known no-effect native invocation and, on expiry, its open gate/node atomically.
// queryRepositoryBundleFinishUnstarted атомарно закрывает native invocation без эффекта и при expiry её open Gate/node.
//
//go:embed sql/repository_bundle__finish_unstarted.sql
var queryRepositoryBundleFinishUnstarted string

// ClaimRepositoryBundles grants at most one bounded native work epoch. Once Begin is durable every replacement is read-only, including after expiry, revocation or root termination.
// ClaimRepositoryBundles выдаёт не более одного bounded native work epoch. После durable Begin любая replacement работает только read-only, включая expiry, revoke и terminal root.
func (repository *Repository) ClaimRepositoryBundles(ctx context.Context, principal value.Principal, claimant string, limit int32) ([]entity.RepositoryBundlePublicationWork, error) {
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != "platform.repository-bundles.work.claim" || principal.ProjectRef != "" || claimant == "" || len(claimant) > 128 || limit != 1 {
		return nil, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rollbackRepositoryBundle(ctx, tx)
	var ref string
	err = tx.QueryRow(ctx, queryRepositoryBundleCandidates, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		if tx.Commit(ctx) != nil {
			return nil, errs.ErrUnavailable
		}
		return []entity.RepositoryBundlePublicationWork{}, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	rootState, invocationState, generation, err := repositoryBundleWorkGuard(ctx, tx, current, ref)
	if err != nil {
		return nil, err
	}
	row, err := lockRepositoryBundle(ctx, tx, current, ref)
	if err != nil {
		return nil, err
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return nil, errs.ErrUnavailable
	}
	active := rootState == "RUNNING" || rootState == "WAITING_HUMAN"
	if row.started == nil {
		code := ""
		expired := !now.Before(row.proposal.ExpiresAt) || row.deadline != nil && !now.Before(*row.deadline)
		if expired {
			code = "DEADLINE_EXCEEDED"
		} else if !active {
			code = "AUTHORITY_CHANGED"
		} else if row.lease.Attempt >= 3 {
			code = "UNAVAILABLE"
		} else {
			err = repository.requireRepositoryBundleAuthority(ctx, tx, current, row, true)
			if err != nil && !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
				return nil, err
			}
			if err != nil {
				code = "AUTHORITY_CHANGED"
			}
		}
		if code != "" {
			if err := repository.finishRepositoryBundleUnstarted(ctx, tx, current, &row, code, expired, now); err != nil {
				return nil, err
			}
			if tx.Commit(ctx) != nil {
				return nil, errs.ErrUnavailable
			}
			return []entity.RepositoryBundlePublicationWork{}, nil
		}
		// WAITING_APPROVAL is selected only for expiry; an open gate never becomes execution permission through claim.
		// WAITING_APPROVAL выбирается только при expiry; open Gate не превращается в execution permission через claim.
		if row.proposal.State == "WAITING_APPROVAL" {
			return nil, errs.ErrConflict
		}
		row.proposal.State = "CLAIMED"
		row.lease.Attempt++
	} else {
		row.proposal.State = "UNKNOWN_OUTCOME"
		// Source/root revocation remains durable across recovery and explicit run retry; a past-fact read cannot mint a native invocation grant again.
		// Source/root revoke сохраняется через recovery и explicit run retry; past-fact read не выдаёт native invocation grant повторно.
		if row.proposal.FailureCode != "AUTHORITY_CHANGED" {
			row.proposal.FailureCode = "OUTCOME_UNCONFIRMED"
		}
	}
	if generation < row.lease.Generation {
		generation = row.lease.Generation
	}
	if generation == math.MaxInt64 {
		return nil, errs.ErrConflict
	}
	fence, err := newRef("fnc")
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	row.lease.Generation, row.lease.Claimant, row.lease.Fence, row.lease.ExpiresAt = generation+1, claimant, fence, now.Add(3*time.Minute)
	if row.started == nil && row.deadline != nil && row.deadline.Before(row.lease.ExpiresAt) {
		row.lease.ExpiresAt = *row.deadline
	}
	row.invocationLeaseRef = ""
	// Terminal native state is never reopened to deliver an old external fact. The ledger alone can still record authoritative readback.
	// Terminal native state не открывается ради старого external fact. Ledger всё ещё сохраняет authoritative readback отдельно.
	if active && row.proposal.FailureCode != "AUTHORITY_CHANGED" && (invocationState == "READY" || invocationState == "RUNNING" || invocationState == "UNKNOWN_OUTCOME") {
		row.invocationLeaseRef, err = newRef("lea")
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		tag, err := tx.Exec(ctx, queryRepositoryBundleClaimInvocation, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": row.invocationID,
			"lease_ref": row.invocationLeaseRef, "fence_digest": repositorybundle.Digest([]byte(fence)), "generation": row.lease.Generation, "claimant": claimant, "expires_at": row.lease.ExpiresAt})
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
	}
	if row.started == nil && row.invocationLeaseRef == "" {
		return nil, errs.ErrConflict
	}
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return nil, err
	}
	work := repositoryBundleWork(row)
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	return []entity.RepositoryBundlePublicationWork{work}, nil
}

// finishRepositoryBundleUnstarted records a known no-effect terminal state and closes all associated native approval consumers in the same transaction; unknown begun effects cannot use this path.
// finishRepositoryBundleUnstarted фиксирует terminal без эффекта и закрывает все native approval consumers одной транзакцией; begun UNKNOWN сюда не допускается.
func (repository *Repository) finishRepositoryBundleUnstarted(ctx context.Context, tx pgx.Tx, current scope, row *repositoryBundleRow, code string, expired bool, now time.Time) error {
	if row.started != nil {
		return errs.ErrConflict
	}
	row.proposal.State, row.proposal.FailureCode, row.completedAt = "FAILED", code, &now
	if expired {
		row.proposal.State = "EXPIRED"
	} else if code == "AUTHORITY_CHANGED" {
		row.proposal.State = "CANCELLED"
	}
	comment := "Repository bundle authority changed"
	if expired {
		comment = "Repository bundle approval expired"
	}
	if _, err := tx.Exec(ctx, queryRepositoryBundleFinishUnstarted, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": row.invocationID,
		"gate_id": row.gateID, "actor_id": current.actorID, "decision_comment": comment, "safe_error_code": "INTEGRATION_REQUEST_REJECTED"}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryInteractionCancelPendingGateDeliveries, pgx.StrictNamedArgs{"gate_id": row.gateID}); err != nil {
		return errs.ErrUnavailable
	}
	row.lease.Claimant, row.lease.Fence, row.lease.ExpiresAt, row.invocationLeaseRef = "", "", time.Time{}, ""
	if err := saveRepositoryBundle(ctx, tx, current, row); err != nil {
		return err
	}
	_, err := repository.scheduleIntegrationContinuation(ctx, tx, current, row.invocationID, row.projectID)
	if err != nil {
		return err
	}
	fresh, err := lockRepositoryBundle(ctx, tx, current, row.proposal.Ref)
	if err != nil {
		return err
	}
	*row = fresh
	return nil
}
