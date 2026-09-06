package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_workflow_template.sql
var queryPromptWorkflowTemplate string

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
