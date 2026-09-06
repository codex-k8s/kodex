package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_scope_insert.sql
var queryPromptScopeInsert string

//go:embed sql/prompt_scope_get.sql
var queryPromptScopeGet string

func (repository *Repository) promptDeclaredContextTx(ctx context.Context, tx pgx.Tx, current scope, input command.PromptTemplateScopeInput) (entity.PromptMaterializationSnapshot, error) {
	if input.TargetKind != promptservice.TargetAgent && input.TargetKind != promptservice.TargetWorkflowStage ||
		input.TemplateKind != "INSTRUCTIONS" && input.TemplateKind != "CONTINUATION" {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	snapshot, err := repository.promptPreviewContextTx(ctx, tx, current, input.TargetKind, input.TargetRef, query.PromptPreviewContext{
		AgentRef: input.AgentRef, WorkflowRevisionRef: input.WorkflowRevisionRef, WorkflowStageKey: input.WorkflowStageKey, ScopeOnly: true})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if input.ExpectedContextDigest != "" && input.ExpectedContextDigest != snapshot.ContextPin.Digest {
		return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
	}
	if input.TemplateKind == "CONTINUATION" {
		snapshot.TargetKind = promptservice.TargetSessionContinuation
		snapshot.ExtraTemplates = nil
		snapshot.StagePurposeTemplate, snapshot.StageExpectedResultTemplate = "", ""
	}
	return snapshot, nil
}

func (repository *Repository) savePromptScopeTx(ctx context.Context, tx pgx.Tx, current scope, projectRef string, revision *entity.ManagedConfigurationRevision, input *command.PromptTemplateScopeInput) error {
	if input == nil {
		return nil
	}
	snapshot, err := repository.promptDeclaredContextTx(ctx, tx, current, *input)
	if err != nil {
		return err
	}
	if snapshot.ProjectRef != projectRef {
		return errs.ErrNotFound
	}
	result := &entity.PromptTemplateScope{TargetKind: input.TargetKind, TargetRef: input.TargetRef, TemplateKind: input.TemplateKind, ContextPin: snapshot.ContextPin}
	updated, err := tx.Exec(ctx, queryPromptScopeInsert, pgx.StrictNamedArgs{"organization_id": current.organizationID, "revision_ref": revision.Ref,
		"target_kind": result.TargetKind, "target_ref": result.TargetRef, "template_kind": result.TemplateKind, "context_pin": asJSON(result.ContextPin)})
	if err != nil || updated.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	revision.PromptScope = result
	return nil
}

func (repository *Repository) hydratePromptScopeTx(ctx context.Context, tx pgx.Tx, current scope, revision *entity.ManagedConfigurationRevision) error {
	if revision == nil {
		return nil
	}
	var result entity.PromptTemplateScope
	var raw []byte
	err := tx.QueryRow(ctx, queryPromptScopeGet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "revision_ref": revision.Ref}).Scan(&result.TargetKind, &result.TargetRef, &result.TemplateKind, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || decodeStrict(raw, &result.ContextPin) != nil || result.ContextPin.Digest == "" {
		return errs.ErrUnavailable
	}
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", result.ContextPin.AgentRef)
	if err != nil || repository.requireAccess(ctx, tx, current, permission, target) != nil {
		return errs.ErrNotFound
	}
	revision.PromptScope = &result
	return nil
}

func (repository *Repository) validatePromptScopeTx(ctx context.Context, tx pgx.Tx, current scope, revision entity.ManagedConfigurationRevision) ([]revisionservice.Diagnostic, error) {
	if err := repository.hydratePromptScopeTx(ctx, tx, current, &revision); err != nil {
		return nil, err
	}
	if revision.PromptScope == nil {
		_, diagnostics, err := revisionservice.Validate(revisionservice.KindPromptTemplate, revision.ContentFormat, revision.Content)
		if err == nil {
			diagnostics = append(diagnostics, revisionservice.Diagnostic{Code: "PROMPT_CONTEXT_NOT_DECLARED", Message: "Prompt context must be checked by the consumer"})
		}
		return diagnostics, err
	}
	scope := revision.PromptScope
	snapshot, err := repository.promptDeclaredContextTx(ctx, tx, current, command.PromptTemplateScopeInput{TargetKind: scope.TargetKind, TargetRef: scope.TargetRef, TemplateKind: scope.TemplateKind,
		AgentRef: scope.ContextPin.AgentRef, WorkflowRevisionRef: scope.ContextPin.WorkflowRevisionRef, WorkflowStageKey: scope.ContextPin.WorkflowStageKey, ExpectedContextDigest: scope.ContextPin.Digest})
	if err != nil {
		return nil, err
	}
	materialized, err := promptservice.Materialize(revision.Content, promptservice.FromSnapshot(snapshot))
	diagnostics := make([]revisionservice.Diagnostic, 0, len(materialized.Diagnostics))
	for _, item := range materialized.Diagnostics {
		diagnostics = append(diagnostics, revisionservice.Diagnostic{Code: item.Code, Message: item.Message})
	}
	if err != nil {
		return diagnostics, errs.ErrInvalid
	}
	if err := validatePromptAvailability(materialized, true); err != nil {
		return diagnostics, err
	}
	if scope.TemplateKind == "CONTINUATION" {
		diagnostics = append(diagnostics, revisionservice.Diagnostic{Code: "RUNTIME_CONTEXT_REQUIRED", Message: "Continuation context is checked for the actual Session before use"})
	}
	return diagnostics, nil
}

func (repository *Repository) validateAgentPromptContextTx(ctx context.Context, tx pgx.Tx, current scope, agentRef, content string, continuation bool) error {
	snapshot, err := repository.promptPreviewContextTx(ctx, tx, current, promptservice.TargetAgent, agentRef, query.PromptPreviewContext{ScopeOnly: true})
	if err != nil {
		return err
	}
	if continuation {
		snapshot.TargetKind = promptservice.TargetSessionContinuation
	}
	materialized, err := promptservice.Materialize(content, promptservice.FromSnapshot(snapshot))
	if err != nil {
		return errs.ErrInvalid
	}
	return validatePromptAvailability(materialized, true)
}

func (repository *Repository) validateWorkflowPromptContextTx(ctx context.Context, tx pgx.Tx, current scope, workflowRef, content string) error {
	workflow, err := scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, current.organizationID, workflowRef, current.role, current.actorID), true)
	if err != nil || workflow.Published == nil {
		return errs.ErrConflict
	}
	keys := []string{"workflow.coordinator.initial", "workflow.coordinator.continue.1"}
	for _, step := range workflow.Published.Steps {
		keys = append(keys, step.Key)
	}
	for _, key := range keys {
		snapshot, err := repository.promptPreviewContextTx(ctx, tx, current, promptservice.TargetWorkflowStage, workflowRef, query.PromptPreviewContext{WorkflowRevisionRef: workflow.Published.Ref, WorkflowStageKey: key, ScopeOnly: true})
		if err != nil {
			return err
		}
		// Новая binding проверяется как единственная Workflow section, а не вместе с прежней.
		snapshot.ExtraTemplates = nil
		materialized, err := promptservice.Materialize(content, promptservice.FromSnapshot(snapshot))
		if err != nil {
			return errs.ErrInvalid
		}
		if err := validatePromptAvailability(materialized, true); err != nil {
			return err
		}
	}
	return nil
}

func validatePromptAvailability(materialized promptservice.Materialization, allowDeferred bool) error {
	if materialized.Complete {
		return nil
	}
	if len(materialized.Diagnostics) == 0 {
		return errs.ErrInvalid
	}
	for _, diagnostic := range materialized.Diagnostics {
		late := diagnostic.VariableName == "task" || diagnostic.VariableName == "run.ref" || diagnostic.VariableName == "session.ref" || diagnostic.VariableName == "turn.ref" || diagnostic.VariableName == "node.ref"
		if !allowDeferred || diagnostic.Code != "RUNTIME_CONTEXT_REQUIRED" || !late {
			return errs.ErrInvalid
		}
	}
	return nil
}
