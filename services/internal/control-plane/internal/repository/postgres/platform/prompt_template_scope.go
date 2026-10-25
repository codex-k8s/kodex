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

// queryPromptScopeInsert embeds scope insertion only for a tenant-owned DRAFT revision of a
// PROMPT_TEMPLATE set, storing the declared target/template kind and context pin. It neither
// publishes the revision nor performs actor access checks itself.
// queryPromptScopeInsert встраивает scope insertion только для tenant-owned DRAFT revision набора
// PROMPT_TEMPLATE с declared target/template kind и context pin. Не публикует revision и
// самостоятельно не проверяет actor access.
//
//go:embed sql/prompt_scope_insert.sql
var queryPromptScopeInsert string

// queryPromptScopeGet embeds declared scope lookup by exact organization/revision ref through the
// revision join. It returns stored context JSON without proving current agent access or pin
// freshness.
// queryPromptScopeGet встраивает declared scope lookup по exact organization/revision ref через
// revision join. Возвращает stored context JSON без доказательства current agent access либо pin
// freshness.
//
//go:embed sql/prompt_scope_get.sql
var queryPromptScopeGet string

// promptDeclaredContextTx accepts only agent/workflow-stage instruction or continuation
// declarations, delegates authorized scope-only preview assembly and optionally compares its
// context digest. Continuation changes the target kind and removes stage/extra templates; it
// creates no runtime context, publication or execution authority.
// promptDeclaredContextTx принимает только agent/workflow-stage instruction либо continuation
// declarations, делегирует authorized scope-only preview assembly и optional context digest
// comparison. Continuation меняет target kind и удаляет stage/extra templates; runtime context,
// publication либо execution authority не создаёт.
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

// savePromptScopeTx treats nil declaration as no-op, resolves its current context and hides a
// project mismatch before inserting exactly one draft scope row. SQL/cardinality failure is
// Conflict; only a successful insert attaches the scope to the supplied revision pointer, without
// committing.
// savePromptScopeTx считает nil declaration no-op, разрешает current context и скрывает project
// mismatch до вставки ровно одной draft scope row. SQL/cardinality failure даёт Conflict; только
// successful insert присоединяет scope к revision pointer, без commit.
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

// hydratePromptScopeTx skips nil revisions and absent scope rows; stored JSON must decode with a
// nonempty digest before current agent target/access checks. Resolution/access failures are hidden
// as NotFound, backend/shape failure is Unavailable. It sets the pointer only on success and does
// not clear an existing pointer for an absent row.
// hydratePromptScopeTx пропускает nil revisions и absent scope rows; stored JSON должен
// декодироваться с непустым digest до current agent target/access checks. Resolution/access
// failures скрываются как NotFound, backend/shape failure дают Unavailable. Назначает pointer
// только при успехе и не очищает existing pointer при absent row.
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

// validatePromptScopeTx hydrates declared scope on a local revision copy and either validates
// unscoped syntax with a consumer-context diagnostic or rematerializes the exact pinned declared
// context. It preserves materializer diagnostics, maps render failure to Invalid, permits only
// designated deferred variables and warns that continuation still needs actual Session context; it
// does not publish or commit.
// validatePromptScopeTx добавляет declared scope в local revision copy и либо проверяет unscoped
// syntax с consumer-context diagnostic, либо перематериализует exact pinned declared context.
// Сохраняет materializer diagnostics, отображает render failure в Invalid, допускает только
// designated deferred variables и предупреждает, что continuation требует actual Session context;
// publication и commit отсутствуют.
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

// validateAgentPromptContextTx builds an authorized scope-only agent snapshot, optionally changes
// it to continuation and materializes the supplied content with deferred runtime-variable
// allowance. Rendering failure is Invalid; no draft is written and no actual Session context is
// synthesized.
// validateAgentPromptContextTx строит authorized scope-only agent snapshot, при необходимости
// меняет его на continuation и материализует content с deferred runtime-variable allowance.
// Rendering failure даёт Invalid; draft не записывается, actual Session context не создаётся.
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

// validateWorkflowPromptContextTx requires a readable workflow with a published version, then
// validates content separately for initial/continuation coordinators and every published step
// using exact version pins. Existing extra templates are removed so only the new workflow section
// is checked; the first preview/render/availability error stops validation without writes or
// commit.
// validateWorkflowPromptContextTx требует читаемый workflow с published version, затем отдельно
// проверяет content для initial/continuation coordinators и каждого published step с exact version
// pins. Existing extra templates удаляются для проверки только новой workflow section; первая
// preview/render/availability error останавливает validation без writes либо commit.
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

// validatePromptAvailability accepts complete materialization immediately; otherwise requires
// nonempty diagnostics consisting only of allowed RUNTIME_CONTEXT_REQUIRED
// task/run/session/turn/node variables when deferral is enabled. Other missing context or any
// other diagnostic is Invalid; this predicate is not a runtime readiness or permission check.
// validatePromptAvailability сразу принимает complete materialization; иначе требует непустые
// diagnostics только с допустимыми RUNTIME_CONTEXT_REQUIRED task/run/session/turn/node variables
// при enabled deferral. Other missing context и любой другой diagnostic дают Invalid; predicate не
// является runtime readiness либо permission check.
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
