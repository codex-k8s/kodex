package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// errRepositoryBundleCallbackInputTooLarge distinguishes a valid owner snapshot exceeding the immutable task budget from authority, malformed input or SQL failures; only this presentation failure may preserve the external receipt.
// errRepositoryBundleCallbackInputTooLarge отличает valid owner snapshot сверх бюджета immutable task от authority, malformed input и SQL failure; лишь эта presentation failure допускает сохранение внешней квитанции.
var errRepositoryBundleCallbackInputTooLarge = errors.New("repository bundle callback input exceeds immutable task limit")

// queryRepositoryBundleCallbackWitness selects one exact server-owned publication witness for this callback join; it grants no caller-selected parent or additional external effect.
// queryRepositoryBundleCallbackWitness выбирает один exact server-owned publication witness данного callback join; caller-selected parent либо новый внешний effect не разрешается.
//
//go:embed sql/repository_bundle__callback_witness.sql
var queryRepositoryBundleCallbackWitness string

// queryRepositoryBundleContinuationAuthorityBlock distinguishes a completed typed origin denied fresh launch authority from pending work, a delivered callback or a terminal root; SQL failure never becomes a blocked-success fallback.
// queryRepositoryBundleContinuationAuthorityBlock отличает completed typed origin без fresh launch authority от pending work, delivered callback и terminal root; SQL failure не становится blocked-success fallback.
//
//go:embed sql/repository_bundle__continuation_authority_block.sql
var queryRepositoryBundleContinuationAuthorityBlock string

// queryRepositoryBundleInvocationOrigin resolves only the new kind's immutable invocation origin for a no-continuation authoritative readback; unrelated integration producers remain unchanged.
// queryRepositoryBundleInvocationOrigin разрешает лишь immutable invocation origin нового kind для authoritative readback без continuation; остальные integration producers не меняются.
//
//go:embed sql/repository_bundle__invocation_origin.sql
var queryRepositoryBundleInvocationOrigin string

// blockRepositoryBundleContinuationAuthority preserves an explicit delivery barrier when completed work loses launch eligibility before its wakeup; no model turn, grant or parent callback is fabricated.
// blockRepositoryBundleContinuationAuthority сохраняет explicit delivery barrier, когда completed work теряет launch eligibility до wakeup; model turn, grant и parent callback не выдумываются.
func (repository *Repository) blockRepositoryBundleContinuationAuthority(ctx context.Context, tx pgx.Tx, current scope, ref, nodeID string) error {
	var nodeRef, runID string
	err := tx.QueryRow(ctx, queryRepositoryBundleContinuationAuthorityBlock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "proposal_ref": ref, "node_id": nodeID}).Scan(&nodeRef, &runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	row, err := lockRepositoryBundle(ctx, tx, current, ref)
	if err != nil {
		return err
	}
	row.proposal.ContinuationState, row.proposal.ContinuationCode, row.proposal.ContinuationNodeRef = "BLOCKED", "CONTINUATION_AUTHORITY_CHANGED", nodeRef
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeIntegrationResumeChildRun, pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_id": runID}); err != nil {
		return errs.ErrUnavailable
	}
	return repositoryBundleAudit(ctx, tx, current, row.projectID, row.actorID, row.proposal.Ref, "CONTINUATION_BLOCKED")
}

// scheduleCallbackContinuation isolates only typed publication presentation in a savepoint. A valid oversized task durably blocks delivery while preserving Git outcome and complete immutable child receipts for owner readback; every other error remains fatal to the owner transaction.
// scheduleCallbackContinuation изолирует лишь typed publication presentation в savepoint. Valid oversized task устойчиво блокирует delivery, сохраняя Git outcome и полные immutable child receipts для owner readback; любая иная ошибка откатывает owner transaction.
func (repository *Repository) scheduleCallbackContinuation(ctx context.Context, tx pgx.Tx, current scope, nodeID, projectID string) (bool, error) {
	var ref, nodeRef string
	var own bool
	err := tx.QueryRow(ctx, queryRepositoryBundleCallbackWitness, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": nodeID}).Scan(&ref, &nodeRef, &own)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.scheduleCallbackContinuationTask(ctx, tx, current, nodeID, projectID)
	}
	if err != nil {
		return false, errs.ErrUnavailable
	}
	row, err := lockRepositoryBundle(ctx, tx, current, ref)
	if err != nil {
		return false, err
	}
	if row.proposal.ContinuationState != "" {
		return false, nil
	}
	presentation, err := tx.Begin(ctx)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	continued, joinErr := repository.scheduleCallbackContinuationTask(ctx, presentation, current, nodeID, projectID)
	if joinErr == nil {
		if presentation.Commit(ctx) != nil {
			return false, errs.ErrUnavailable
		}
		if !continued {
			if err := repository.blockRepositoryBundleContinuationAuthority(ctx, tx, current, ref, nodeID); err != nil {
				return false, err
			}
		} else {
			if _, err := tx.Exec(ctx, queryRuntimeIntegrationResumeRootRun, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": row.rootRunID}); err != nil {
				return false, errs.ErrUnavailable
			}
		}
		return continued, nil
	}
	if presentation.Rollback(ctx) != nil {
		return false, errs.ErrUnavailable
	}
	if !errors.Is(joinErr, errRepositoryBundleCallbackInputTooLarge) {
		return false, joinErr
	}
	row.proposal.ContinuationState, row.proposal.ContinuationCode, row.proposal.ContinuationNodeRef = "BLOCKED", "CALLBACK_INPUT_TOO_LARGE", nodeRef
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return false, err
	}
	if own {
		if _, err := tx.Exec(ctx, queryRuntimeIntegrationResumeChildRun, pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_id": row.runID}); err != nil {
			return false, errs.ErrUnavailable
		}
	}
	if err := repositoryBundleAudit(ctx, tx, current, row.projectID, row.actorID, row.proposal.Ref, "CONTINUATION_BLOCKED"); err != nil {
		return false, err
	}
	return false, nil
}
