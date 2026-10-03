package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/catalog_project_cards.sql
var queryCatalogProjectCards string

//go:embed sql/catalog_agent_cards.sql
var queryCatalogAgentCards string

//go:embed sql/catalog_workflow_cards.sql
var queryCatalogWorkflowCards string

func cardArgs(current scope, refs []string) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "role": current.role, "refs": refs}
}

// Каталог, single read и command readback используют один batch projection.
// Отсутствующие/скрытые зависимости не считаются видимыми нулевыми snapshots.
func projectProjectCards(ctx context.Context, runner queryRunner, current scope, items []*entity.Project) error {
	if len(items) == 0 {
		return nil
	}
	refs := make([]string, 0, len(items))
	byRef := make(map[string]*entity.Project, len(items))
	for _, item := range items {
		refs = append(refs, item.Ref)
		byRef[item.Ref] = item
	}
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

func projectWorkflowCards(ctx context.Context, runner queryRunner, current scope, items []*entity.Workflow) error {
	if len(items) == 0 {
		return nil
	}
	refs := make([]string, 0, len(items))
	byRef := make(map[string]*entity.Workflow, len(items))
	for _, item := range items {
		refs = append(refs, item.Ref)
		byRef[item.Ref] = item
	}
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
