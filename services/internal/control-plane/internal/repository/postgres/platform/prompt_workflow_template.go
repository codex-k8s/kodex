package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPromptWorkflowTemplate embeds the exact organization/workflow managed PROMPT_TEMPLATE
// binding with matching project/set/revision and a published or superseded revision. It returns
// only ref/digest/content; workflow read authority is established by the caller before hydration.
// queryPromptWorkflowTemplate встраивает точную organization/workflow managed PROMPT_TEMPLATE
// binding с совпадающими project/set/revision и опубликованной либо замещённой revision.
// Возвращает только ref/digest/content; право чтения workflow вызывающий устанавливает до
// hydration.
//
//go:embed sql/prompt_workflow_template.sql
var queryPromptWorkflowTemplate string

// hydrateWorkflowPromptTemplateTx does nothing without a pinned workflow ref or when no managed
// template binding exists. Otherwise it reads the ref/digest/content in the caller transaction and
// appends a WORKFLOW_CONTEXT extra template; SQL failures are Unavailable. It neither clears
// existing extras nor independently checks content digests or workflow access, and it does not
// rewrite historical materializations.
// hydrateWorkflowPromptTemplateTx ничего не делает без pinned workflow ref либо при отсутствии
// managed template binding. Иначе читает ref/digest/content в транзакции вызывающего и добавляет
// WORKFLOW_CONTEXT extra template; SQL-отказ даёт Unavailable. Не очищает прежние extras,
// самостоятельно не проверяет content digest или workflow access и не переписывает исторические
// materializations.
func (repository *Repository) hydrateWorkflowPromptTemplateTx(ctx context.Context, tx pgx.Tx, current scope, snapshot *entity.PromptMaterializationSnapshot) error {
	if snapshot.ContextPin.WorkflowRef == "" {
		return nil
	}
	item := entity.PromptUserTemplate{Kind: "WORKFLOW_CONTEXT"}
	err := tx.QueryRow(ctx, queryPromptWorkflowTemplate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "workflow_ref": snapshot.ContextPin.WorkflowRef}).Scan(&item.Ref, &item.Digest, &item.Content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	snapshot.ExtraTemplates = append(snapshot.ExtraTemplates, item)
	return nil
}
