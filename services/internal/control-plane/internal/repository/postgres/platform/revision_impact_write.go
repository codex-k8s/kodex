package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

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
