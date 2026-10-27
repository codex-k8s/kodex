package platform

import (
	"context"
	"errors"
	"sort"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// bindSecretDraftImpact requires an unbound PREPARED plan with exact draft/secret versions,
// validates selected item membership and uniqueness, then binds it to the same-owner operation and
// stores PENDING/NOT_SELECTED choices. SQL also checks plan expiry; failures require caller
// rollback and no independent commit occurs.
// bindSecretDraftImpact требует непривязанный PREPARED план с точными версиями черновика и
// секрета, проверяет принадлежность и уникальность выбранных элементов, затем связывает с
// операцией того же владельца и сохраняет PENDING/NOT_SELECTED. SQL также проверяет срок плана;
// ошибки требуют rollback вызывающего кода, отдельного Commit нет.
func (r *Repository) bindSecretDraftImpact(ctx context.Context, tx pgx.Tx, s scope, d secretDraftRow, input repoport.RuntimeSecretDraftPrepareInput, operationRef string) error {
	plan, err := r.secretDraftImpact(ctx, tx, s, input.ImpactPlanRef, "", "")
	if err != nil {
		return err
	}
	if plan.public.State != "PREPARED" || plan.operationID != "" || plan.public.DraftRef != d.public.Ref || input.Mutation.ExpectedVersion == nil || plan.public.DraftVersion != *input.Mutation.ExpectedVersion || plan.public.SecretVersion != input.ExpectedSecretVersion {
		return errs.ErrConflict
	}
	items, err := r.secretDraftImpactItems(ctx, tx, plan.id)
	if err != nil {
		return err
	}
	allowed := make(map[string]bool, len(items))
	for _, item := range items {
		allowed[item.Ref] = true
	}
	selected := make(map[string]bool, len(input.SelectedItemRefs))
	for _, ref := range input.SelectedItemRefs {
		if !allowed[ref] || selected[ref] {
			return errs.ErrInvalid
		}
		selected[ref] = true
	}
	var id string
	if tx.QueryRow(ctx, querySecretDraftImpactBind, pgx.StrictNamedArgs{"plan_ref": plan.public.Ref, "operation_ref": operationRef, "draft_version": plan.public.DraftVersion, "secret_version": plan.public.SecretVersion}).Scan(&id) != nil {
		return errs.ErrConflict
	}
	refs := input.SelectedItemRefs
	if refs == nil {
		refs = []string{}
	}
	if _, err := tx.Exec(ctx, querySecretDraftImpactSelect, pgx.StrictNamedArgs{"plan_id": id, "selected": refs}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// secretDraftImpactError classifies Forbidden/NotFound as FORBIDDEN and
// Conflict/VersionMismatch/Invalid as CONFLICT through errors.Is. An empty result means the error
// is not a supported per-item outcome and must propagate, not be silently skipped.
// secretDraftImpactError классифицирует Forbidden/NotFound как FORBIDDEN,
// Conflict/VersionMismatch/Invalid — как CONFLICT через errors.Is. Пустой результат означает
// неподдерживаемый исход элемента: ошибка должна передаваться выше, а не незаметно пропускаться.
func secretDraftImpactError(err error) string {
	if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
		return "FORBIDDEN"
	}
	if errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrVersionMismatch) || errors.Is(err, errs.ErrInvalid) {
		return "CONFLICT"
	}
	return ""
}

// recordSecretDraftImpactItem conditionally records outcome and result environment/binding pins
// for a still-PENDING plan item. It maps SQL failure to Unavailable but does not require a nonzero
// affected-row count; caller plan/state ownership remains essential.
// recordSecretDraftImpactItem условно записывает исход и результирующие environment/binding pins
// ещё PENDING элемента плана. Ошибку SQL преобразует в Unavailable, но не требует ненулевое число
// изменённых строк; владение планом и состоянием обеспечивает вызывающий код.
func (r *Repository) recordSecretDraftImpactItem(ctx context.Context, tx pgx.Tx, id string, item entity.RuntimeSecretDraftImpactItem) error {
	_, err := tx.Exec(ctx, querySecretDraftImpactOutcome, pgx.StrictNamedArgs{"plan_id": id, "item_ref": item.Ref, "outcome": item.Outcome, "environment_version_ref": item.ResultEnvironmentVersionRef, "binding_ref": item.ResultBindingRef, "binding_version": item.ResultBindingVersion})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// applySecretDraftImpact groups PENDING items by exact environment/source revision, sorts groups
// and publishes each under a savepoint with own-version carry. Selected agent bindings get
// separate savepoints and exact readback; classified failures become per-item outcomes, unexpected
// failures abort the caller path. APPLIED plan state can include forbidden/conflict items, and an
// environment publication can remain even when its binding fails. All effects still belong to the
// outer secret owner transaction, not external retries.
// applySecretDraftImpact группирует PENDING элементы по точным окружению и исходной ревизии,
// сортирует группы и публикует каждую под savepoint с переносом собственной версии. Выбранные
// agent bindings получают отдельные savepoints и точный readback; классифицированные ошибки
// становятся исходами элементов, неожиданные прерывают путь. APPLIED план может содержать
// forbidden/conflict элементы, публикация окружения может сохраниться при ошибке binding. Все
// эффекты остаются во внешней транзакции владельца секрета, а не во внешних повторах.
func (r *Repository) applySecretDraftImpact(ctx context.Context, tx pgx.Tx, s scope, o secretDraftOperationRow, secret entity.RuntimeSecret) error {
	plan, err := r.secretDraftImpact(ctx, tx, s, "", "", o.id)
	if err != nil {
		return err
	}
	if plan.public.State != "PREPARED" {
		return errs.ErrConflict
	}
	items, err := r.secretDraftImpactItems(ctx, tx, plan.id)
	if err != nil {
		return err
	}
	groups := map[string][]entity.RuntimeSecretDraftImpactItem{}
	for _, item := range items {
		if item.Outcome == "PENDING" {
			key := item.Consumer.EnvironmentRef + "/" + item.Consumer.EnvironmentVersionRef
			groups[key] = append(groups[key], item)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	versions := map[string]int64{}
	for _, key := range keys {
		group := groups[key]
		source := group[0].Consumer
		expected := source.EnvironmentVersion
		if ownVersion, ok := versions[source.EnvironmentRef]; ok {
			expected = ownVersion
		}
		batch, err := tx.Begin(ctx)
		if err != nil {
			return errs.ErrUnavailable
		}
		base := command.Command{Kind: command.RebindRuntimeSecret, Principal: value.Principal{CorrelationRef: o.correlation}, Mutation: value.Mutation{ExpectedVersion: &secret.Version}, Payload: command.RuntimeSecretRebindInput{SecretRef: secret.Ref, Revision: secret.CurrentRevision, Selections: []entity.RuntimeSecretRebindSelection{{EnvironmentRef: source.EnvironmentRef, SourceVersionRef: source.EnvironmentVersionRef, ExpectedEnvironmentVersion: expected, ScopeKind: source.ScopeKind, OrganizationRef: source.OrganizationRef, ProjectRef: source.ProjectRef}}}}
		published, applyErr := r.rebindRuntimeSecret(ctx, batch, s, base)
		if applyErr != nil {
			_ = batch.Rollback(ctx)
			outcome := secretDraftImpactError(applyErr)
			if outcome == "" {
				return applyErr
			}
			for _, item := range group {
				item.Outcome = outcome
				if err = r.recordSecretDraftImpactItem(ctx, tx, plan.id, item); err != nil {
					return err
				}
			}
			continue
		}
		if len(published.result.RuntimeEnvironments) != 1 || published.result.RuntimeEnvironments[0].CurrentVersion.Ref == "" {
			_ = batch.Rollback(ctx)
			return errs.ErrUnavailable
		}
		environment := published.result.RuntimeEnvironments[0]
		if batch.Commit(ctx) != nil {
			return errs.ErrUnavailable
		}
		versions[source.EnvironmentRef] = environment.Version
		for _, item := range group {
			item.Outcome = "APPLIED"
			item.ResultEnvironmentVersionRef = environment.CurrentVersion.Ref
			if item.Consumer.Consumer.AgentRef != "" {
				attempt, err := tx.Begin(ctx)
				if err != nil {
					return errs.ErrUnavailable
				}
				bindingCommand := base
				bindingCommand.Kind = command.RebindRuntimeEnvironment
				bindingCommand.Mutation.ExpectedVersion = &environment.Version
				bindingCommand.Payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref, Consumers: []entity.RuntimeEnvironmentConsumer{item.Consumer.Consumer}}
				bound, bindingErr := r.rebindRuntimeEnvironment(ctx, attempt, s, bindingCommand)
				if bindingErr != nil {
					_ = attempt.Rollback(ctx)
					item.Outcome = secretDraftImpactError(bindingErr)
					item.ResultEnvironmentVersionRef = ""
					if item.Outcome == "" {
						return bindingErr
					}
				} else {
					if len(bound.result.EnvironmentBindings) != 1 {
						_ = attempt.Rollback(ctx)
						return errs.ErrUnavailable
					}
					binding := bound.result.EnvironmentBindings[0]
					item.ResultBindingRef = binding.Ref
					item.ResultBindingVersion = binding.Version
					if attempt.Commit(ctx) != nil {
						return errs.ErrUnavailable
					}
				}
			}
			if err = r.recordSecretDraftImpactItem(ctx, tx, plan.id, item); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, querySecretDraftImpactFinish, pgx.StrictNamedArgs{"operation_id": o.id, "state": "APPLIED"}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
