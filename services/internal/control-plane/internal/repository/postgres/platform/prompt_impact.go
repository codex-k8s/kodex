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

// queryPromptImpactConsumers embeds a deterministic page of at most 1001 live prompt-template
// consumers with consumer/binding versions, source revision and owner tuple. It does not filter by
// actor permissions; access is checked before items enter a plan or are applied.
// queryPromptImpactConsumers встраивает упорядоченную выборку максимум 1001 действующего
// потребителя шаблона с версиями потребителя и привязки, исходной ревизией и владельцем. Не
// фильтрует по правам субъекта; доступ проверяется перед включением в план и применением.
//
//go:embed sql/prompt_impact_consumers.sql
var queryPromptImpactConsumers string

// queryPromptImpactLockConsumer embeds row locks for the exact tenant, consumer kind/ref and
// prompt-template binding, returning current consumer and binding versions. Null or mismatched
// versions require caller rejection; locks alone do not authorize a rebind.
// queryPromptImpactLockConsumer встраивает блокировки строк точных организации, вида и ссылки
// потребителя и привязки шаблона, возвращая текущие версии потребителя и привязки. Null либо
// несовпадение версий требует отказа вызывающего кода; блокировки сами не разрешают перепривязку.
//
//go:embed sql/prompt_impact_lock_consumer.sql
var queryPromptImpactLockConsumer string

// readPromptImpactConsumers reads and scans bounded consumer metadata in the supplied transaction,
// assigning PENDING to each item. Query, scan or rows errors return Unavailable and discard the
// accumulated list; it closes rows but neither authorizes items nor commits.
// readPromptImpactConsumers читает ограниченную выборку метаданных потребителей в переданной
// транзакции, назначая каждому элементу PENDING. Ошибки запроса, сканирования или rows дают
// Unavailable и отбрасывают накопленный список; закрывает rows, но не проверяет доступ к элементам
// и не завершает транзакцию.
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
		if rows.Scan(&item.ProjectRef, &item.ConsumerKind, &item.ConsumerRef, &item.ConsumerVersion, &item.BindingRef, &item.BindingVersion, &item.SourceRevisionRef, &item.ScopeKind, &item.OrganizationRef) != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// preparePromptTemplateImpact requires an already typed managed-configuration input and UI-owned
// set without prior plan/selection, then locks a VALID draft and validates prompt scope. It bounds
// all consumers, omits inaccessible items, assigns refs and persists an exact source/draft/digest
// plan in the caller transaction. It returns an outcome without publishing or committing.
// preparePromptTemplateImpact требует уже типизированный вход managed-конфигурации и набор под
// управлением UI без прежнего плана и выбора, затем блокирует VALID черновик и проверяет область
// шаблона. Ограничивает число всех потребителей, пропускает недоступных, назначает ссылки и
// сохраняет план с точными исходной версией, черновиком и дайджестом в переданной транзакции.
// Возвращает результат без публикации и завершения транзакции.
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

// authorizePromptImpactPublish validates typed input and exact plan kind/source/draft identity,
// rechecks plan access, resolves the managed set and locks the requested revision. It rejects
// digest drift and validates prompt scope; full PREPARED/source-version/selection checks belong to
// the publication path.
// authorizePromptImpactPublish проверяет типизированный вход и точные вид, источник и черновик
// плана, повторно проверяет доступ к плану, разрешает набор и блокирует запрошенную ревизию.
// Отклоняет изменение дайджеста и проверяет область шаблона; полные проверки PREPARED, исходной
// версии и выбора выполняются в пути публикации.
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

// promptPlanForPublish reauthorizes the plan and requires PREPARED plus exact source aggregate
// version/current revision, draft revision/number and target digest. It loads actor-scoped items
// and applies explicit selection validation, returning rows without changing bindings or
// committing.
// promptPlanForPublish повторно проверяет доступ к плану, требует PREPARED и точные версию
// исходного агрегата, текущую ревизию, ссылку и номер черновика и целевой дайджест. Загружает
// элементы с учётом субъекта и проверяет явный выбор, возвращая строки без изменения привязок и
// завершения транзакции.
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

// applyPromptImpact processes PENDING items through nested transaction savepoints: fresh access,
// exact locked versions and source binding are checked before typed rebind. Classified failures
// roll back only that attempt and record an item outcome; unexpected failures abort the caller
// path. Successful attempts require readback of the new binding before savepoint commit, then the
// impact plan is finalized in the outer transaction. Partial local results on error are not
// committed success.
// applyPromptImpact обрабатывает PENDING элементы через вложенные транзакционные savepoint: перед
// типизированной перепривязкой проверяет свежий доступ, точные заблокированные версии и исходную
// привязку. Классифицированные ошибки откатывают попытку и записывают исход элемента; неожиданные
// прерывают вызывающий путь. Успешная попытка требует повторного чтения новой привязки до
// завершения savepoint, затем план завершается во внешней транзакции. Частичный локальный
// результат при ошибке не означает зафиксированный успех.
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
