package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_impact_consumers.sql
var queryPromptImpactConsumers string

//go:embed sql/prompt_impact_lock_consumer.sql
var queryPromptImpactLockConsumer string

func readPromptImpactConsumers(ctx context.Context, tx pgx.Tx, s scope, set managedSet) ([]entity.RevisionImpactItem, error) {
	rows, err := tx.Query(ctx, queryPromptImpactConsumers, pgx.StrictNamedArgs{"organization_id": s.organizationID, "configuration_id": set.id})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := []entity.RevisionImpactItem{}
	for rows.Next() {
		var item entity.RevisionImpactItem
		item.Outcome = "PENDING"
		if rows.Scan(&item.ProjectRef, &item.ConsumerKind, &item.ConsumerRef, &item.ConsumerVersion, &item.BindingRef, &item.BindingVersion, &item.SourceRevisionRef) != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func (r *Repository) preparePromptTemplateImpact(ctx context.Context, tx pgx.Tx, s scope, set managedSet, input command.Command) (commandOutcome, error) {
	p := input.Payload.(command.ManagedConfigurationInput)
	if set.ManagedBy != "UI" || p.PlanRef != "" || len(p.SelectedItemRefs) != 0 {
		return commandOutcome{}, errs.ErrConflict
	}
	revision, err := r.lockManagedRevision(ctx, tx, s, set, p.RevisionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if revision.State != "VALID" {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err = r.validatePromptScopeTx(ctx, tx, s, revision.ManagedConfigurationRevision); err != nil {
		return commandOutcome{}, err
	}
	all, err := readPromptImpactConsumers(ctx, tx, s, set)
	if err != nil {
		return commandOutcome{}, err
	}
	if len(all) > maximumRevisionImpactItems {
		return commandOutcome{}, errs.ErrConflict
	}
	items := []entity.RevisionImpactItem{}
	for _, item := range all {
		if err = r.revisionImpactItemAccess(ctx, tx, s, item); err != nil {
			if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
				continue
			}
			return commandOutcome{}, err
		}
		item.Ref, err = newRef("rvit")
		if err != nil {
			return commandOutcome{}, err
		}
		items = append(items, item)
	}
	source := ""
	if set.CurrentRevision != nil {
		source = set.CurrentRevision.Ref
	}
	plan, err := persistRevisionImpact(ctx, tx, s, entity.RevisionImpactPlan{Kind: "PROMPT_TEMPLATE", SourceRef: set.Ref, SourceVersion: set.Version, SourceRevisionRef: source, DraftRef: revision.Ref, DraftVersion: revision.Revision, TargetDigest: revision.Digest}, items)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{RevisionImpactPlan: &plan}, projectID: set.projectID, projectRef: set.ProjectRef, resourceKind: "MANAGED_CONFIGURATION", resourceRef: set.Ref, summary: "i18n:PROMPT_TEMPLATE_IMPACT_PREPARED"}, nil
}

func (r *Repository) authorizePromptImpactPublish(ctx context.Context, tx pgx.Tx, s scope, input command.Command) error {
	p, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok {
		return errs.ErrInvalid
	}
	row, err := r.revisionImpact(ctx, tx, s, p.PlanRef)
	if err != nil {
		return err
	}
	if row.plan.Kind != "PROMPT_TEMPLATE" || row.plan.SourceRef != p.ConfigurationRef || row.plan.DraftRef != p.RevisionRef {
		return errs.ErrNotFound
	}
	if err = r.revisionImpactAccess(ctx, tx, s, row); err != nil {
		return err
	}
	set, err := r.resolveManagedSet(ctx, tx, s, p, revisionservice.KindPromptTemplate, false)
	if err != nil {
		return err
	}
	revision, err := r.lockManagedRevision(ctx, tx, s, set, p.RevisionRef)
	if err != nil {
		return err
	}
	if revision.Digest != row.plan.TargetDigest {
		return errs.ErrConflict
	}
	_, err = r.validatePromptScopeTx(ctx, tx, s, revision.ManagedConfigurationRevision)
	return err
}

func (r *Repository) promptPlanForPublish(ctx context.Context, tx pgx.Tx, s scope, set managedSet, revision lockedManagedRevision, p command.ManagedConfigurationInput) (revisionImpactRow, []entity.RevisionImpactItem, error) {
	row, err := r.revisionImpact(ctx, tx, s, p.PlanRef)
	if err != nil {
		return row, nil, err
	}
	if err = r.revisionImpactAccess(ctx, tx, s, row); err != nil {
		return row, nil, err
	}
	source := ""
	if set.CurrentRevision != nil {
		source = set.CurrentRevision.Ref
	}
	if row.plan.Kind != "PROMPT_TEMPLATE" || row.plan.State != "PREPARED" || row.plan.SourceRef != set.Ref || row.plan.SourceVersion != set.Version || row.plan.SourceRevisionRef != source || row.plan.DraftRef != revision.Ref || row.plan.DraftVersion != revision.Revision || row.plan.TargetDigest != revision.Digest {
		return row, nil, errs.ErrConflict
	}
	items, err := r.revisionImpactItems(ctx, tx, row, s.actorID)
	if err != nil {
		return row, nil, err
	}
	if err = selectRevisionImpactItems(items, p.SelectedItemRefs); err != nil {
		return row, nil, err
	}
	return row, items, nil
}

func (r *Repository) applyPromptImpact(ctx context.Context, tx pgx.Tx, s scope, set managedSet, input command.Command, row revisionImpactRow, items []entity.RevisionImpactItem) (managedSet, entity.RevisionImpactPlan, error) {
	published := set.CurrentRevision.Ref
	for i := range items {
		item := &items[i]
		if item.Outcome != "PENDING" {
			continue
		}
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return set, row.plan, errs.ErrUnavailable
		}
		applyErr := r.revisionImpactItemAccess(ctx, attempt, s, *item)
		if applyErr == nil {
			var consumerVersion *int64
			var bindingVersion int64
			lockErr := attempt.QueryRow(ctx, queryPromptImpactLockConsumer, pgx.StrictNamedArgs{"organization_id": s.organizationID, "consumer_kind": item.ConsumerKind, "consumer_ref": item.ConsumerRef, "binding_ref": item.BindingRef}).Scan(&consumerVersion, &bindingVersion)
			if errors.Is(lockErr, pgx.ErrNoRows) || lockErr == nil && (consumerVersion == nil || *consumerVersion != item.ConsumerVersion || bindingVersion != item.BindingVersion) {
				applyErr = errs.ErrConflict
			} else if lockErr != nil {
				applyErr = errs.ErrUnavailable
			}
		}
		if applyErr == nil {
			var currentItems []entity.RevisionImpactItem
			currentItems, applyErr = readPromptImpactConsumers(ctx, attempt, s, set)
			found := false
			for _, current := range currentItems {
				if current.BindingRef == item.BindingRef && current.BindingVersion == item.BindingVersion && current.ConsumerKind == item.ConsumerKind && current.ConsumerRef == item.ConsumerRef && current.ConsumerVersion == item.ConsumerVersion && current.SourceRevisionRef == item.SourceRevisionRef {
					found = true
				}
			}
			if applyErr == nil && !found {
				applyErr = errs.ErrConflict
			}
		}
		var outcome commandOutcome
		if applyErr == nil {
			impact, impactErr := r.managedImpactTx(ctx, attempt, s, set.Ref, published, query.Filter{Page: query.Page{Size: 1}})
			applyErr = impactErr
			if applyErr == nil {
				nested := input
				nested.Kind = command.RebindPromptTemplate
				nested.Mutation.ExpectedVersion = &set.Version
				nested.Payload = command.ManagedConfigurationInput{ConfigurationRef: set.Ref, RevisionRef: published, ImpactDigest: impact.Digest, Consumers: []entity.ManagedConfigurationConsumer{{Kind: item.ConsumerKind, Ref: item.ConsumerRef, RevisionRef: item.SourceRevisionRef, Version: item.BindingVersion}}}
				outcome, applyErr = r.changeManagedConfiguration(ctx, attempt, s, nested)
			}
		}
		if applyErr != nil {
			_ = attempt.Rollback(ctx)
			item.Outcome = secretDraftImpactError(applyErr)
			if item.Outcome == "" {
				return set, row.plan, applyErr
			}
			continue
		}
		currentItems, err := readPromptImpactConsumers(ctx, attempt, s, set)
		if err != nil {
			_ = attempt.Rollback(ctx)
			return set, row.plan, err
		}
		found := false
		for _, current := range currentItems {
			if current.BindingRef == item.BindingRef && current.ConsumerKind == item.ConsumerKind && current.ConsumerRef == item.ConsumerRef && current.BindingVersion > item.BindingVersion && current.SourceRevisionRef == published {
				found = true
				item.Outcome = "APPLIED"
				item.ResultRevisionRef = published
				item.ResultBindingRef = current.BindingRef
				item.ResultBindingVersion = current.BindingVersion
				item.ResultConsumerVersion = current.ConsumerVersion
			}
		}
		if !found || outcome.result.ManagedConfiguration == nil {
			_ = attempt.Rollback(ctx)
			return set, row.plan, errs.ErrUnavailable
		}
		if err = attempt.Commit(ctx); err != nil {
			return set, row.plan, errs.ErrUnavailable
		}
		set.ManagedConfigurationSet = *outcome.result.ManagedConfiguration
	}
	plan, err := finishRevisionImpact(ctx, tx, row, items, published)
	return set, plan, err
}
