package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) authorizeEnvironmentDraftImpact(ctx context.Context, tx pgx.Tx, s scope, input command.Command) error {
	payload, ok := input.Payload.(command.RuntimeEnvironmentDraftInput)
	if !ok {
		return errs.ErrInvalid
	}
	draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftGet, s.organizationID, payload.DraftRef))
	if err != nil {
		return err
	}
	digest, err := r.validateEnvironmentDraft(ctx, tx, s, draft)
	if err != nil {
		return err
	}
	if digest != draft.ValidationDigest {
		return errs.ErrConflict
	}
	if input.Kind == command.PublishRuntimeEnvironmentDraft {
		row, err := r.revisionImpact(ctx, tx, s, payload.PlanRef)
		if err != nil {
			return err
		}
		if row.plan.Kind != "RUNTIME_ENVIRONMENT" || row.plan.DraftRef != draft.Ref || row.plan.TargetDigest != digest {
			return errs.ErrConflict
		}
		return r.revisionImpactAccess(ctx, tx, s, row)
	}
	return nil
}

func (r *Repository) prepareEnvironmentDraftImpact(ctx context.Context, tx pgx.Tx, s scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentDraftInput)
	if !ok || payload.PlanRef != "" || len(payload.SelectedItemRefs) != 0 {
		return commandOutcome{}, errs.ErrInvalid
	}
	draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftLock, s.organizationID, payload.DraftRef))
	if err != nil {
		return commandOutcome{}, err
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != draft.Version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if draft.State != "VALID" {
		return commandOutcome{}, errs.ErrConflict
	}
	digest, err := r.validateEnvironmentDraft(ctx, tx, s, draft)
	if err != nil {
		return commandOutcome{}, err
	}
	if digest != draft.ValidationDigest {
		return commandOutcome{}, errs.ErrConflict
	}
	if draft.EnvironmentRef != "" {
		source, _, err := r.environmentImpactTarget(ctx, tx, s, draft.EnvironmentRef, draft.BaseVersionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if source.EnvironmentVersion != draft.ExpectedEnvironmentVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
	}
	plan := entity.RevisionImpactPlan{Kind: "RUNTIME_ENVIRONMENT", Version: 1, SourceRef: draft.EnvironmentRef, SourceVersion: draft.ExpectedEnvironmentVersion,
		SourceRevisionRef: draft.BaseVersionRef, DraftRef: draft.Ref, DraftVersion: draft.Version, TargetDigest: digest, State: "PREPARED"}
	items := []entity.RevisionImpactItem{}
	if draft.EnvironmentRef != "" {
		rows, err := tx.Query(ctx, queryRevisionImpactEnvironmentConsumers, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "environment_ref": draft.EnvironmentRef, "authority_project": s.authorityProjectID, "evaluated_at": time.Now().UTC()})
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		for rows.Next() {
			item := entity.RevisionImpactItem{ConsumerKind: "AGENT", Outcome: "PENDING"}
			if rows.Scan(&item.ProjectRef, &item.ConsumerRef, &item.ConsumerVersion, &item.BindingRef, &item.BindingVersion, &item.SourceRevisionRef) != nil {
				rows.Close()
				return commandOutcome{}, errs.ErrUnavailable
			}
			item.Ref, err = newRef("rvit")
			if err != nil {
				rows.Close()
				return commandOutcome{}, errs.ErrUnavailable
			}
			items = append(items, item)
		}
		rows.Close()
		if rows.Err() != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if len(items) > maximumRevisionImpactItems {
		return commandOutcome{}, errs.ErrConflict
	}
	plan.Ref, err = newRef("rvip")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan.Total = int64(len(items))
	plan.Digest, err = revisionImpactDigest(plan, s.actorID, items)
	if err != nil {
		return commandOutcome{}, err
	}
	var id string
	if tx.QueryRow(ctx, queryRevisionImpactInsert, pgx.StrictNamedArgs{"ref": plan.Ref, "organization_id": s.organizationID, "actor_id": s.actorID, "kind": plan.Kind, "snapshot": asJSON(plan), "digest": plan.Digest}).Scan(&id, &plan.CreatedAt, &plan.ExpiresAt) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	for _, item := range items {
		if _, err = tx.Exec(ctx, queryRevisionImpactInsertItem, pgx.StrictNamedArgs{"plan_id": id, "ref": item.Ref, "snapshot": asJSON(item)}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	return commandOutcome{result: command.Result{RevisionImpactPlan: &plan}, projectID: mustProjectID(ctx, tx, s.organizationID, draft.ProjectRef), projectRef: draft.ProjectRef,
		resourceKind: "RUNTIME_ENVIRONMENT_DRAFT", resourceRef: draft.Ref, summary: "i18n:RUNTIME_ENVIRONMENT_IMPACT_PREPARED"}, nil
}

func (r *Repository) environmentDraftImpactForPublish(ctx context.Context, tx pgx.Tx, s scope, draft entity.RuntimeEnvironmentDraft, payload command.RuntimeEnvironmentDraftInput) (revisionImpactRow, []entity.RevisionImpactItem, error) {
	row, err := r.revisionImpact(ctx, tx, s, payload.PlanRef)
	if err != nil {
		return row, nil, err
	}
	if err = r.revisionImpactAccess(ctx, tx, s, row); err != nil {
		return row, nil, err
	}
	if row.plan.Kind != "RUNTIME_ENVIRONMENT" || row.plan.State != "PREPARED" || row.plan.DraftRef != draft.Ref || row.plan.DraftVersion != draft.Version ||
		row.plan.SourceRef != draft.EnvironmentRef || row.plan.SourceVersion != draft.ExpectedEnvironmentVersion || row.plan.SourceRevisionRef != draft.BaseVersionRef || row.plan.TargetDigest != draft.ValidationDigest {
		return row, nil, errs.ErrConflict
	}
	items, err := r.revisionImpactItems(ctx, tx, row, s.actorID)
	if err != nil {
		return row, nil, err
	}
	if len(payload.SelectedItemRefs) > maximumRevisionImpactItems {
		return row, nil, errs.ErrInvalid
	}
	known := map[string]bool{}
	for _, item := range items {
		known[item.Ref] = true
	}
	selected := map[string]bool{}
	for _, ref := range payload.SelectedItemRefs {
		if !known[ref] || selected[ref] {
			return row, nil, errs.ErrInvalid
		}
		selected[ref] = true
	}
	for i := range items {
		if !selected[items[i].Ref] {
			items[i].Outcome = "NOT_SELECTED"
		}
	}
	return row, items, nil
}

func (r *Repository) applyEnvironmentDraftImpact(ctx context.Context, tx pgx.Tx, s scope, input command.Command, row revisionImpactRow, items []entity.RevisionImpactItem, environment entity.RuntimeEnvironmentSet) (entity.RevisionImpactPlan, error) {
	for i := range items {
		item := &items[i]
		if item.Outcome == "PENDING" {
			attempt, err := tx.Begin(ctx)
			if err != nil {
				return row.plan, errs.ErrUnavailable
			}
			nested := input
			nested.Kind = command.RebindRuntimeEnvironment
			nested.Mutation.ExpectedVersion = &environment.Version
			nested.Payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref, Consumers: []entity.RuntimeEnvironmentConsumer{{AgentRef: item.ConsumerRef, AgentVersion: item.ConsumerVersion, BindingRef: item.BindingRef, BindingVersion: item.BindingVersion, VersionRef: item.SourceRevisionRef, ProjectRef: item.ProjectRef}}}
			outcome, applyErr := r.rebindRuntimeEnvironment(ctx, attempt, s, nested)
			if applyErr != nil {
				_ = attempt.Rollback(ctx)
				item.Outcome = secretDraftImpactError(applyErr)
				if item.Outcome == "" {
					return row.plan, applyErr
				}
			} else {
				if len(outcome.result.EnvironmentBindings) != 1 {
					_ = attempt.Rollback(ctx)
					return row.plan, errs.ErrUnavailable
				}
				binding := outcome.result.EnvironmentBindings[0]
				if binding.Ref != item.BindingRef || binding.Version <= item.BindingVersion || binding.VersionRef != environment.CurrentVersion.Ref {
					_ = attempt.Rollback(ctx)
					return row.plan, errs.ErrUnavailable
				}
				view, err := r.getRuntimeConfigurationViewTx(ctx, attempt, s, item.ConsumerRef)
				if err != nil {
					_ = attempt.Rollback(ctx)
					return row.plan, err
				}
				if err = attempt.Commit(ctx); err != nil {
					return row.plan, errs.ErrUnavailable
				}
				item.Outcome, item.ResultRevisionRef, item.ResultBindingRef, item.ResultBindingVersion, item.ResultConsumerVersion = "APPLIED", binding.VersionRef, binding.Ref, binding.Version, view.AgentVersion
			}
		}
		tag, err := tx.Exec(ctx, queryRevisionImpactOutcome, pgx.StrictNamedArgs{"plan_id": row.id, "ref": item.Ref, "outcome": item.Outcome, "revision_ref": item.ResultRevisionRef, "binding_ref": item.ResultBindingRef, "binding_version": item.ResultBindingVersion, "consumer_version": item.ResultConsumerVersion})
		if err != nil || tag.RowsAffected() != 1 {
			return row.plan, errs.ErrUnavailable
		}
	}
	tag, err := tx.Exec(ctx, queryRevisionImpactFinish, pgx.StrictNamedArgs{"plan_id": row.id, "revision_ref": environment.CurrentVersion.Ref})
	if err != nil {
		return row.plan, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return row.plan, errs.ErrConflict
	}
	row.plan.Version, row.plan.State, row.plan.PublishedRevisionRef = 2, "APPLIED", environment.CurrentVersion.Ref
	return row.plan, nil
}
