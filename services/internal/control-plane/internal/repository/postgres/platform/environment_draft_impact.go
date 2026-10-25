package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// authorizeEnvironmentDraftImpact validates the typed draft and fresh validation digest; publish
// additionally requires matching plan kind/draft/digest and current plan access. It does not
// perform the full PREPARED/source-version/selection checks of publication or grant authority from
// PlanRef.
// authorizeEnvironmentDraftImpact проверяет typed draft и свежий validation digest; publish
// дополнительно требует совпадения plan kind/draft/digest и текущего доступа к плану. Не выполняет
// полные PREPARED/source-version/selection проверки публикации и не выдаёт права из PlanRef.
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

// prepareEnvironmentDraftImpact locks a VALID owner-accessible draft, checks OCC, fresh digest and
// previous environment version, then captures bounded eligible consumers with versions/bindings.
// It persists an actor-bound PREPARED digest plan and items in the caller transaction. New
// environments have no previous consumers; preparation neither publishes nor rebinds anything.
// prepareEnvironmentDraftImpact блокирует доступный владельцу VALID draft, проверяет OCC, свежий
// digest и прежнюю environment version, затем фиксирует ограниченный набор допустимых consumers с
// версиями и bindings. Сохраняет actor-bound PREPARED digest plan и items в переданной транзакции.
// Новое environment не имеет прежних consumers; подготовка ничего не публикует и не
// перепривязывает.
func (r *Repository) prepareEnvironmentDraftImpact(ctx context.Context, tx pgx.Tx, s scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentDraftInput)
	if !ok || payload.PlanRef != "" || len(payload.SelectedItemRefs) != 0 {
		return commandOutcome{}, errs.ErrInvalid
	}
	draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftLock, s.organizationID, payload.DraftRef))
	if err != nil {
		return commandOutcome{}, err
	}
	if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, draft.ScopeKind, draft.ProjectRef); err != nil {
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
			if rows.Scan(&item.ProjectRef, &item.ConsumerRef, &item.ConsumerVersion, &item.BindingRef, &item.BindingVersion, &item.SourceRevisionRef, &item.ScopeKind, &item.OrganizationRef) != nil {
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

// environmentDraftImpactForPublish rechecks plan access and exact PREPARED
// source/draft/base-version/digest identity, loads actor-bound items and delegates bounded
// known/distinct selection to the shared helper. Unselected items become NOT_SELECTED only after
// all refs validate; failures return no item list. Current consumer eligibility is rechecked while
// applying.
// environmentDraftImpactForPublish перепроверяет доступ и точные PREPARED
// source/draft/base-version/digest плана, читает actor-bound items и передаёт ограниченный выбор
// известных distinct refs общему helper. Невыбранные items становятся NOT_SELECTED лишь после
// проверки всех refs; ошибка не возвращает список items. Текущая допустимость consumers
// перепроверяется при применении.
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
	if err := selectRevisionImpactItems(items, payload.SelectedItemRefs); err != nil {
		return row, nil, err
	}
	return row, items, nil
}

// applyEnvironmentDraftImpact attempts each PENDING rebind in a savepoint against the published
// version and exact prior binding tuple. Known failures become item outcomes; unexpected failures
// abort the outer owner transaction. Successful binding/readback is verified before savepoint
// commit, each outcome requires one row and the plan finishes APPLIED even with classified failed
// items. It does not independently commit the publication.
// applyEnvironmentDraftImpact пытается перепривязать каждый PENDING item в savepoint к
// опубликованной версии по точному прежнему binding tuple. Известные ошибки становятся item
// outcomes, неожиданные прерывают внешнюю owner transaction. Успешные binding/readback проверяются
// до savepoint commit, каждый outcome требует одну строку, план завершается APPLIED также с
// классифицированными неуспешными items. Отдельно публикацию не фиксирует.
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
			nested.Payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref, Consumers: []entity.RuntimeEnvironmentConsumer{{AgentRef: item.ConsumerRef, AgentVersion: item.ConsumerVersion, BindingRef: item.BindingRef, BindingVersion: item.BindingVersion, VersionRef: item.SourceRevisionRef, ProjectRef: item.ProjectRef, ScopeKind: item.ScopeKind, OrganizationRef: item.OrganizationRef}}}
			outcome, applyErr := r.rebindRuntimeEnvironmentFromPublication(ctx, attempt, s, nested, row.plan.SourceRevisionRef)
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
