package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// persistRevisionImpact bounds items, assigns server plan identity/actor-bound digest and PREPARED
// version1, then persists snapshot and items in the caller transaction. It returns the current
// plan even on error, so partial in-memory metadata is not proof of commit. It does not perform
// consumer access checks or apply any binding.
// persistRevisionImpact ограничивает items, назначает серверную plan identity/actor-bound digest и
// PREPARED version1, затем сохраняет snapshot и items в переданной транзакции. Возвращает текущий
// plan также при ошибке, поэтому partial metadata в памяти не доказывает Commit. Не проверяет
// доступ к consumers и не применяет binding.
func persistRevisionImpact(ctx context.Context, tx pgx.Tx, s scope, plan entity.RevisionImpactPlan, items []entity.RevisionImpactItem) (entity.RevisionImpactPlan, error) {
	if len(items) > maximumRevisionImpactItems {
		return plan, errs.ErrConflict
	}
	var err error
	plan.Ref, err = newRef("rvip")
	if err != nil {
		return plan, err
	}
	plan.Total = int64(len(items))
	plan.Version = 1
	plan.State = "PREPARED"
	plan.Digest, err = revisionImpactDigest(plan, s.actorID, items)
	if err != nil {
		return plan, err
	}
	var id string
	if err = tx.QueryRow(ctx, queryRevisionImpactInsert, pgx.StrictNamedArgs{"ref": plan.Ref, "organization_id": s.organizationID, "actor_id": s.actorID, "kind": plan.Kind, "snapshot": asJSON(plan), "digest": plan.Digest}).Scan(&id, &plan.CreatedAt, &plan.ExpiresAt); err != nil {
		return plan, errs.ErrUnavailable
	}
	for _, item := range items {
		if _, err = tx.Exec(ctx, queryRevisionImpactInsertItem, pgx.StrictNamedArgs{"plan_id": id, "ref": item.Ref, "snapshot": asJSON(item)}); err != nil {
			return plan, errs.ErrUnavailable
		}
	}
	return plan, nil
}

// selectRevisionImpactItems validates a bounded selection containing only known distinct refs
// before mutating any item. Unselected entries become NOT_SELECTED while selected outcomes remain
// unchanged; empty selection marks every item unselected. Invalid returns without mutation. It
// neither reloads owner state nor checks permission or plan freshness.
// selectRevisionImpactItems проверяет bounded selection лишь известных distinct refs до изменения
// любого item. Невыбранные entries становятся NOT_SELECTED, outcomes выбранных сохраняются; пустой
// выбор помечает все items невыбранными. Invalid возвращается без изменений. Не перечитывает owner
// state и не проверяет права и свежесть плана.
func selectRevisionImpactItems(items []entity.RevisionImpactItem, refs []string) error {
	if len(refs) > maximumRevisionImpactItems {
		return errs.ErrInvalid
	}
	selected := map[string]bool{}
	known := map[string]bool{}
	for _, item := range items {
		known[item.Ref] = true
	}
	for _, ref := range refs {
		if !known[ref] || selected[ref] {
			return errs.ErrInvalid
		}
		selected[ref] = true
	}
	for i := range items {
		if !selected[items[i].Ref] {
			items[i].Outcome = "NOT_SELECTED"
		}
	}
	return nil
}

// finishRevisionImpact persists each outcome with exactly one affected row, then finishes a
// still-eligible plan as APPLIED version2 with published revision. Item write failure is
// Unavailable; finish failure/mismatch is Conflict. APPLIED does not mean every item succeeded,
// and later errors require outer rollback of prior writes; there is no independent commit.
// finishRevisionImpact сохраняет каждый outcome с ровно одной изменённой строкой, затем завершает
// ещё допустимый план как APPLIED version2 с published revision. Item write failure даёт
// Unavailable, finish failure/mismatch — Conflict. APPLIED не означает успех каждого item, поздняя
// ошибка требует outer rollback прежних записей; отдельного Commit нет.
func finishRevisionImpact(ctx context.Context, tx pgx.Tx, row revisionImpactRow, items []entity.RevisionImpactItem, published string) (entity.RevisionImpactPlan, error) {
	for _, item := range items {
		tag, err := tx.Exec(ctx, queryRevisionImpactOutcome, pgx.StrictNamedArgs{"plan_id": row.id, "ref": item.Ref, "outcome": item.Outcome, "revision_ref": item.ResultRevisionRef, "binding_ref": item.ResultBindingRef, "binding_version": item.ResultBindingVersion, "consumer_version": item.ResultConsumerVersion})
		if err != nil || tag.RowsAffected() != 1 {
			return row.plan, errs.ErrUnavailable
		}
	}
	tag, err := tx.Exec(ctx, queryRevisionImpactFinish, pgx.StrictNamedArgs{"plan_id": row.id, "revision_ref": published})
	if err != nil || tag.RowsAffected() != 1 {
		return row.plan, errs.ErrConflict
	}
	row.plan.Version = 2
	row.plan.State = "APPLIED"
	row.plan.PublishedRevisionRef = published
	return row.plan, nil
}
