package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// terminalSecretDraftRecovery admits only the atomic published draft/completed PUBLISH combination; active work and cleanup ACK retain their original SERIALIZABLE path.
// terminalSecretDraftRecovery допускает лишь атомарную пару published draft/completed PUBLISH; active work и cleanup ACK сохраняют прежний SERIALIZABLE path.
func terminalSecretDraftRecovery(draft secretDraftRow, operation secretDraftOperationRow) bool {
	return draft.public.State == "PUBLISHED" && operation.kind == "PUBLISH" && operation.state == "COMPLETED"
}

// recoverTerminalSecretDraft repeats scoped draft/Secret/operation locks and exact claimant/generation in READ COMMITTED before the existing recovery consumer. Fresh retention, revision retirement, durable cleanup intent and audit remain one owner transaction. A known nonterminal probe rolls back without effects before a full original-path read; errors never authorize fallback or another transaction retry.
// recoverTerminalSecretDraft повторяет scoped draft/Secret/operation locks и exact claimant/generation в READ COMMITTED перед существующим recovery consumer. Свежий retention, retirement ревизии, durable cleanup intent и audit остаются одной owner transaction. Известный nonterminal probe откатывается без effects перед полным чтением прежнего пути; ошибки не разрешают fallback либо ещё один transaction retry.
func (r *Repository) recoverTerminalSecretDraft(ctx context.Context, p value.Principal, input repoport.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftResult, bool, error) {
	var empty entity.RuntimeSecretDraftResult
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, false, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return empty, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	opRef, draftRef, err := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, "", "", "", input.OperationRef, "")
	if err != nil {
		return empty, false, err
	}
	d, err := r.lockSecretDraft(ctx, tx, s.organizationID, draftRef)
	if err != nil {
		return empty, false, err
	}
	o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, opRef)
	if err != nil {
		return empty, false, err
	}
	if o.claimantID != input.ClaimantID || o.claimGeneration != input.ClaimGeneration {
		return empty, false, errs.ErrConflict
	}
	if !terminalSecretDraftRecovery(d, o) {
		if err := tx.Rollback(ctx); err != nil {
			return empty, false, errs.ErrUnavailable
		}
		return empty, false, nil
	}
	if o.targetRevision < 1 || d.public.PublishedRevision != o.targetRevision {
		return empty, true, errs.ErrUnavailable
	}
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, true, errs.ErrUnavailable
	}
	result, err := r.recoverSecretDraft(ctx, tx, s, &d, &o, input, now)
	if err != nil {
		return empty, true, err
	}
	if tx.Commit(ctx) != nil {
		return empty, true, errs.ErrConflict
	}
	return result, true, nil
}
