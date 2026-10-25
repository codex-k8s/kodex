package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryCatalogProjectCards embeds batch project summaries using only visible agents, workflows,
// root runs and integrations, plus current project-assistant creation eligibility. Counts and
// readiness hints do not authorize subsequent commands.
// queryCatalogProjectCards встраивает batch-сводки проектов только по видимым agents, workflows,
// root runs и integrations и текущую допустимость создания project assistant. Счётчики и readiness
// hints не разрешают последующие команды.
//
//go:embed sql/catalog_project_cards.sql
var queryCatalogProjectCards string

// queryCatalogAgentCards embeds the latest visible active execution run per requested agent and
// server-derived assistant backing. It does not claim that every requested ref exists or that the
// run has a live execution lease.
// queryCatalogAgentCards встраивает последний видимый активный execution run запрошенного агента и
// серверный признак assistant backing. Не доказывает существование каждого запрошенного ref и
// действующую execution lease запуска.
//
//go:embed sql/catalog_agent_cards.sql
var queryCatalogAgentCards string

// queryCatalogWorkflowCards embeds published-or-draft workflow topology summaries and visible root
// run/gate activity. Unique-agent counts respect visibility; the card is metadata rather than an
// executable graph or delegation grant.
// queryCatalogWorkflowCards встраивает сводки опубликованной либо черновой topology workflow и
// активности видимых root runs/gates. Число уникальных агентов учитывает видимость; карточка
// содержит метаданные, а не исполняемый граф и delegation grant.
//
//go:embed sql/catalog_workflow_cards.sql
var queryCatalogWorkflowCards string

// cardArgs binds the already resolved organization, actor, role and authority-project with
// requested refs using StrictNamedArgs. It does not derive identity or verify that the refs were
// individually authorized.
// cardArgs связывает уже разрешённые organization, actor, role и authority-project с запрошенными
// refs через StrictNamedArgs. Не выводит идентичность и не проверяет отдельный доступ к refs.
func cardArgs(current scope, refs []string) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "role": current.role, "refs": refs}
}

// catalogCardRefs collects refs in input order and indexes the original card pointers for
// projection. Duplicate refs remain in the query input and the last pointer wins, preserving
// prior behavior. It does not authorize, copy or mutate cards; callers supply nonnil entries.
// catalogCardRefs собирает refs в исходном порядке и индексирует прежние указатели карточек
// для проекции. Повторные refs остаются во входе запроса, последний указатель имеет приоритет,
// сохраняя прежнее поведение. Не проверяет доступ, не копирует и не меняет карточки;
// вызывающий код передаёт ненулевые элементы.
func catalogCardRefs[T any](items []*T, ref func(*T) string) ([]string, map[string]*T) {
	refs := make([]string, 0, len(items))
	byRef := make(map[string]*T, len(items))
	for _, item := range items {
		key := ref(item)
		refs = append(refs, key)
		byRef[key] = item
	}
	return refs, byRef
}

// projectProjectCards mutates original project pointers with batch counts/activity/integration
// hints and replaces only CREATE_PROJECT_ASSISTANT from current eligibility. Empty input is a
// no-op; unknown result refs conflict, missing distinct refs are NotFound and row failures are
// Unavailable. Partial mutations on error must be discarded; catalog/single/readback share this
// projection.
// projectProjectCards меняет прежние указатели проектов batch-счётчиками, активностью и
// integration hints, заменяя только CREATE_PROJECT_ASSISTANT по текущей допустимости. Пустой вход
// ничего не делает; неизвестный ref результата даёт Conflict, отсутствие distinct ref — NotFound,
// ошибки rows — Unavailable. Частичные изменения при ошибке отбрасываются; каталог, single read и
// readback используют одну проекцию.
func projectProjectCards(ctx context.Context, runner queryRunner, current scope, items []*entity.Project) error {
	if len(items) == 0 {
		return nil
	}
	refs, byRef := catalogCardRefs(items, func(item *entity.Project) string { return item.Ref })
	rows, err := runner.Query(ctx, queryCatalogProjectCards, cardArgs(current, refs))
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var card entity.Project
		var canCreateAssistant bool
		if err := rows.Scan(&ref, &card.AgentCount, &card.WorkflowCount, &card.ActiveRunCount, &card.PendingGateCount, &card.LastActivityAt, &card.IntegrationState, &canCreateAssistant); err != nil {
			return errs.ErrUnavailable
		}
		item, ok := byRef[ref]
		if !ok {
			return errs.ErrConflict
		}
		item.AgentCount, item.WorkflowCount, item.ActiveRunCount, item.PendingGateCount = card.AgentCount, card.WorkflowCount, card.ActiveRunCount, card.PendingGateCount
		item.LastActivityAt, item.IntegrationState = card.LastActivityAt, card.IntegrationState
		freshActions := make([]string, 0, len(item.NextActions)+1)
		for _, action := range item.NextActions {
			if action != "CREATE_PROJECT_ASSISTANT" {
				freshActions = append(freshActions, action)
			}
		}
		item.NextActions = freshActions
		if canCreateAssistant {
			item.NextActions = append(item.NextActions, "CREATE_PROJECT_ASSISTANT")
		}
		delete(byRef, ref)
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	if len(byRef) != 0 {
		return errs.ErrNotFound
	}
	return nil
}

// projectAgentCards clears each current-run ref before reading and assigns visible run/assistant
// backing in place, suppressing ARCHIVE/DELETE/PURGE for assistant-backed agents. Unknown result
// refs conflict and row failures are Unavailable; unlike project/workflow cards, missing rows are
// not explicitly rejected and partial mutations can remain on error.
// projectAgentCards очищает current-run ref каждого агента до чтения и назначает видимый run и
// assistant backing на месте, убирая ARCHIVE/DELETE/PURGE для связанных с ассистентом агентов.
// Неизвестный ref результата даёт Conflict, ошибки rows — Unavailable; в отличие от
// project/workflow карточек, отсутствие строк явно не отклоняется, при ошибке возможны частичные
// изменения.
func projectAgentCards(ctx context.Context, runner queryRunner, current scope, items []*entity.Agent) error {
	if len(items) == 0 {
		return nil
	}
	refs := make([]string, 0, len(items))
	byRef := make(map[string]*entity.Agent, len(items))
	for _, item := range items {
		item.CurrentRunRef = ""
		refs = append(refs, item.Ref)
		byRef[item.Ref] = item
	}
	rows, err := runner.Query(ctx, queryCatalogAgentCards, cardArgs(current, refs))
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref, runRef string
		var assistantBacked bool
		if err := rows.Scan(&ref, &runRef, &assistantBacked); err != nil {
			return errs.ErrUnavailable
		}
		item, ok := byRef[ref]
		if !ok {
			return errs.ErrConflict
		}
		item.CurrentRunRef = runRef
		item.AssistantBacked = assistantBacked
		if assistantBacked {
			freshActions := make([]string, 0, len(item.NextActions))
			for _, action := range item.NextActions {
				if action != "ARCHIVE" && action != "DELETE" && action != "PURGE" {
					freshActions = append(freshActions, action)
				}
			}
			item.NextActions = freshActions
		}
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// projectWorkflowCards attaches one freshly scanned summary to each original workflow pointer.
// Empty input is a no-op, unknown/duplicate returned refs conflict and absent distinct refs are
// NotFound; row failures are Unavailable. It does not authorize execution, and callers discard
// partial pointer updates on failure.
// projectWorkflowCards добавляет свежую считанную сводку к каждому прежнему указателю workflow.
// Пустой вход ничего не делает, неизвестный либо повторно возвращённый ref даёт Conflict,
// отсутствие distinct ref — NotFound, ошибки rows — Unavailable. Не разрешает исполнение; caller
// отбрасывает частичные изменения указателей при ошибке.
func projectWorkflowCards(ctx context.Context, runner queryRunner, current scope, items []*entity.Workflow) error {
	if len(items) == 0 {
		return nil
	}
	refs, byRef := catalogCardRefs(items, func(item *entity.Workflow) string { return item.Ref })
	rows, err := runner.Query(ctx, queryCatalogWorkflowCards, cardArgs(current, refs))
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var card entity.WorkflowCardSummary
		if err := rows.Scan(&ref, &card.StageCount, &card.UniqueAgentCount, &card.ParallelGroupCount, &card.HasHumanGate, &card.ActiveRunCount, &card.PendingGateCount, &card.LastActivityAt); err != nil {
			return errs.ErrUnavailable
		}
		item, ok := byRef[ref]
		if !ok {
			return errs.ErrConflict
		}
		item.CardSummary = &card
		delete(byRef, ref)
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	if len(byRef) != 0 {
		return errs.ErrNotFound
	}
	return nil
}
