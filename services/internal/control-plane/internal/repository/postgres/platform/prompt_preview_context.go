package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_preview_agent.sql
var queryPromptPreviewAgent string

//go:embed sql/prompt_preview_attachment_items.sql
var queryPromptPreviewAttachmentItems string

func (repository *Repository) GetPromptPreviewContextSnapshot(ctx context.Context, principal value.Principal,
	kind, ref string, input query.PromptPreviewContext,
) (entity.PromptMaterializationSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if (kind != promptservice.TargetAgent && kind != promptservice.TargetWorkflowStage && kind != promptservice.TargetSessionContinuation) || ref == "" ||
		input.ExpectedAgentVersion < 0 || input.ExpectedWorkflowVersion < 0 || !validBoundedRunInput(input.Input) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, err := repository.promptPreviewContextTx(ctx, tx, current, kind, ref, input)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	return snapshot, nil
}

func (repository *Repository) promptPreviewContextTx(ctx context.Context, tx pgx.Tx, current scope,
	kind, ref string, input query.PromptPreviewContext,
) (entity.PromptMaterializationSnapshot, error) {
	return repository.promptPreviewContextForActorTx(ctx, tx, current, current, kind, ref, input)
}

// Viewer сохраняет собственные права чтения; execution назначается только owner.
func (repository *Repository) promptPreviewContextForActorTx(ctx context.Context, tx pgx.Tx, current, execution scope,
	kind, ref string, input query.PromptPreviewContext,
) (entity.PromptMaterializationSnapshot, error) {
	if kind == promptservice.TargetSessionContinuation {
		return repository.promptContinuationPreviewForActorTx(ctx, tx, current, execution, ref, input)
	}
	if (kind != promptservice.TargetAgent && kind != promptservice.TargetWorkflowStage) || ref == "" ||
		input.ExpectedAgentVersion < 0 || input.ExpectedAgentVersion > 9007199254740991 || input.ExpectedWorkflowVersion < 0 || input.ExpectedWorkflowVersion > 9007199254740991 ||
		!validBoundedRunInput(input.Input) || len(input.Task) > 64<<10 || !utf8.ValidString(input.Task) || strings.ContainsRune(input.Task, 0) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	var workflow entity.Workflow
	var err error
	var version *entity.WorkflowVersion
	var step *entity.WorkflowStep
	agentRef := ref
	if kind == promptservice.TargetWorkflowStage {
		if input.WorkflowStageKey == "" {
			return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
		}
		_, target, resolveErr := repository.resolveCommandTarget(ctx, tx, current, "workflow.view", "WORKFLOW", ref, "")
		if resolveErr != nil || repository.requireAccess(ctx, tx, current, "workflow.view", target) != nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
		workflow, err = scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, current.organizationID, ref, current.role, current.actorID), true)
		if err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		if input.ExpectedWorkflowVersion > 0 && input.ExpectedWorkflowVersion != workflow.Version {
			return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
		}
		for _, candidate := range []*entity.WorkflowVersion{workflow.Draft, workflow.Published} {
			if candidate != nil && candidate.Ref == input.WorkflowRevisionRef {
				version = candidate
				break
			}
		}
		if input.WorkflowRevisionRef == "" {
			version = workflow.Draft
		}
		if version == nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
		}
		if selected, ok := promptWorkflowStep(*version, input.WorkflowStageKey); ok {
			step = &selected
		}
		if step == nil || !input.ScopeOnly && !validWorkflowRunInput(version.Inputs, input.Input) {
			return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
		}
		agentRef = step.AgentRef
	} else if input.WorkflowRevisionRef != "" || input.WorkflowStageKey != "" || input.ExpectedWorkflowVersion != 0 {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	if input.AgentRef != "" && input.AgentRef != agentRef {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agentRef)
	if err != nil || repository.requireAccess(ctx, tx, current, permission, target) != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return entity.PromptMaterializationSnapshot{}, errs.ErrForbidden
	}
	snapshot, err := repository.promptPreviewAgentForActorTx(ctx, tx, current, execution, agentRef)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if input.ExpectedAgentVersion > 0 && input.ExpectedAgentVersion != snapshot.ContextPin.AgentVersion {
		return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
	}
	if kind == promptservice.TargetWorkflowStage {
		if snapshot.ProjectRef != workflow.ProjectRef {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
		applyWorkflowPromptContext(&snapshot, workflow.Ref, workflow.Version, *version, *step)
		if err := repository.hydrateWorkflowPromptTemplateTx(ctx, tx, current, &snapshot); err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		if err := refreshPromptFileScopes(&snapshot); err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		snapshot.Variables["task"] = step.Instructions
		snapshot.WorkflowStage = step.Name
	}
	snapshot.StructuredVariables["input"].(map[string]any)["values"] = input.Input
	if input.Task != "" {
		snapshot.Variables["task"] = input.Task
	} else {
		snapshot.UnavailableVariables["task"] = "RUNTIME_CONTEXT_REQUIRED"
	}
	narrowPromptIntegrationScope(&snapshot)
	if input.AttachmentSetRef != "" {
		if err := repository.promptPreviewAttachmentsTx(ctx, tx, current, target.projectID, &snapshot, input.AttachmentSetRef); err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
	}
	// Context pin исключает будущие server-issued Run/Turn refs, но связывает
	// весь выбранный immutable input и фактическую authority текущего actor.
	raw, err := json.Marshal(struct {
		Actor    string
		Snapshot entity.PromptMaterializationSnapshot
	}{current.actorRef, snapshot})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	snapshot.ContextPin.Digest = hex.EncodeToString(digest[:])
	return snapshot, nil
}

func (repository *Repository) promptPreviewAttachmentsTx(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	snapshot *entity.PromptMaterializationSnapshot, ref string,
) error {
	purpose := "RUN_INPUT"
	if snapshot.TargetKind == promptservice.TargetWorkflowStage {
		purpose = "WORKFLOW_INPUT"
	}
	if snapshot.TargetKind == promptservice.TargetSessionContinuation {
		purpose = "SESSION_TURN"
	}
	var set sealedAttachmentSet
	if err := tx.QueryRow(ctx, queryAttachmentSetsResolveFinalized, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"project_id": projectID, "attachment_set_ref": ref, "purpose": purpose}).Scan(&set.ID, &set.Ref, &set.ManifestDigest, &set.Purpose, &set.ItemCount, &set.TotalSizeBytes); err != nil {
		return errs.ErrNotFound
	}
	if err := repository.promptPreviewAttachmentSetTx(ctx, tx, current, projectID, snapshot, set, false); err != nil {
		return err
	}
	snapshot.ContextPin.AttachmentSetRef, snapshot.ContextPin.AttachmentManifestDigest = set.Ref, set.ManifestDigest
	return refreshPromptFileScopes(snapshot)
}

func (repository *Repository) promptPreviewAttachmentSetTx(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	snapshot *entity.PromptMaterializationSnapshot, set sealedAttachmentSet, historical bool,
) error {
	rows, err := tx.Query(ctx, queryPromptPreviewAttachmentItems, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "attachment_set_id": set.ID})
	if err != nil {
		return errs.ErrUnavailable
	}
	var refs []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || int64(len(refs)) != set.ItemCount {
		return errs.ErrConflict
	}
	if err := repository.authorizePromptArtifactsTx(ctx, tx, current, snapshot.ProjectRef, refs); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			disablePromptFileScopes(snapshot, "PERMISSION_REQUIRED")
			return refreshPromptFileScopes(snapshot)
		}
		return err
	}
	items, err := repository.listAttachmentSetItemsTx(ctx, tx, set.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		item.AttachmentSetRef, item.AttachmentPurpose, item.Provenance = set.Ref, set.Purpose, "CURRENT_TURN"
		if historical {
			item.Scope, item.Provenance = runtimecontract.AttachmentScopeSession, "SESSION_HISTORY"
		}
		snapshot.Artifacts = append(snapshot.Artifacts, item.RunnerInputArtifact)
	}
	if len(snapshot.Artifacts) > 2048 {
		return errs.ErrConflict
	}
	return nil
}

func (repository *Repository) promptPreviewAgentTx(ctx context.Context, tx pgx.Tx, current scope, ref string) (entity.PromptMaterializationSnapshot, error) {
	return repository.promptPreviewAgentForActorTx(ctx, tx, current, current, ref)
}

func (repository *Repository) promptPreviewAgentForActorTx(ctx context.Context, tx pgx.Tx, current, execution scope, ref string) (entity.PromptMaterializationSnapshot, error) {
	snapshot := entity.PromptMaterializationSnapshot{ServiceTemplateRevision: promptservice.ServiceTemplateRevision,
		TargetKind: promptservice.TargetAgent, TargetRef: ref, Variables: map[string]string{}}
	snapshot.UnavailableVariables = map[string]string{"run.ref": "RUNTIME_CONTEXT_REQUIRED", "session.ref": "RUNTIME_CONTEXT_REQUIRED", "turn.ref": "RUNTIME_CONTEXT_REQUIRED", "node.ref": "RUNTIME_CONTEXT_REQUIRED"}
	var name, purpose, projectName string
	err := tx.QueryRow(ctx, queryPromptPreviewAgent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": ref}).Scan(
		&snapshot.ContextPin.AgentRef, &name, &purpose, &snapshot.ContextPin.AgentVersion, &snapshot.ProjectRef, &projectName, &snapshot.Locale,
		&snapshot.TemplateRef, &snapshot.TemplateContent, &snapshot.TemplateDigest, &snapshot.AgentCapabilities)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, ref)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	snapshot.ContextPin.RuntimeConfigurationRef, snapshot.ContextPin.RuntimeConfigurationDigest = view.Configuration.Ref, view.Configuration.Digest
	snapshot.ContextPin.EnvironmentBindingRef, snapshot.ContextPin.EnvironmentBindingVersion = view.EnvironmentBinding.Ref, view.EnvironmentBinding.Version
	snapshot.ContextPin.EnvironmentVersionRef, snapshot.ContextPin.EnvironmentDigest = view.Environment.CurrentVersion.Ref, view.Environment.CurrentVersion.Digest
	snapshot.Variables["agent.ref"], snapshot.Variables["agent.name"], snapshot.Variables["project.ref"], snapshot.Variables["project.name"] = ref, name, snapshot.ProjectRef, projectName
	snapshot.Variables["organization.ref"], snapshot.Variables["user.ref"], snapshot.Variables["task"] = current.organizationRef, execution.actorRef, purpose
	snapshot.Variables["user.name"] = execution.actorName
	snapshot.Variables["environment.ref"] = view.Environment.CurrentVersion.Ref
	tools := make([]runtimecontract.RuntimeEnvironmentTool, 0, len(view.Environment.CurrentVersion.Tools))
	for _, tool := range view.Environment.CurrentVersion.Tools {
		tools = append(tools, runtimecontract.RuntimeEnvironmentTool{Name: tool.Name, Description: tool.Description, Command: tool.Command})
	}
	image := runtimecontract.RuntimeEnvironmentImage{Reference: view.Environment.CurrentVersion.Image.Reference, Digest: view.Environment.CurrentVersion.Image.Digest}
	snapshot.StructuredVariables, err = promptStructuredVariables(nil, tools, image, view.Environment.CurrentVersion.Ref, "", "")
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	if err := repository.promptPreviewKnowledgeTx(ctx, tx, execution, &snapshot); err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	if execution.actorID != current.actorID {
		refs := make([]string, 0, len(snapshot.Artifacts))
		for _, item := range snapshot.Artifacts {
			refs = append(refs, item.Ref)
		}
		if err := repository.authorizePromptArtifactsTx(ctx, tx, current, snapshot.ProjectRef, refs); err != nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
	}
	var grants []map[string]string
	snapshot.UserCapabilities, grants, err = repository.agentCapabilityAuthority(ctx, tx, execution, snapshot.ProjectRef, ref, snapshot.AgentCapabilities)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	for _, grant := range grants {
		if key := grant["capabilityKey"]; key != "" {
			snapshot.ConnectionCapabilities = append(snapshot.ConnectionCapabilities, key)
		}
	}
	snapshot.StructuredVariables["integrations"] = promptIntegrationScope(grants, promptservice.Intersection(snapshot.UserCapabilities, promptservice.Union(snapshot.AgentCapabilities, snapshot.ConnectionCapabilities)))
	return snapshot, nil
}
