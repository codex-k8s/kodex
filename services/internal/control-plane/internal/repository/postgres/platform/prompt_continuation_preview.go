package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_continuation_session.sql
var queryPromptContinuationSession string

func (repository *Repository) promptContinuationPreviewTx(ctx context.Context, tx pgx.Tx, current scope, sessionRef string, input query.PromptPreviewContext) (entity.PromptMaterializationSnapshot, error) {
	return repository.promptContinuationPreviewForActorTx(ctx, tx, current, current, sessionRef, input)
}

func (repository *Repository) promptContinuationPreviewForActorTx(ctx context.Context, tx pgx.Tx, current, execution scope, sessionRef string, input query.PromptPreviewContext) (entity.PromptMaterializationSnapshot, error) {
	var sessionID, projectID, projectRef, targetType, targetRef, accountRef, previousRunRef, previousRef string
	if err := tx.QueryRow(ctx, queryPromptContinuationSession, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_ref": sessionRef}).Scan(
		&sessionID, &projectID, &projectRef, &targetType, &targetRef, &accountRef, &previousRunRef, &previousRef); err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	if projectRef != "" {
		_, target, err := repository.resolveCommandTarget(ctx, tx, current, "run.view", "RUN", previousRunRef, "")
		if err != nil || repository.requireAccess(ctx, tx, current, "run.view", target) != nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
		}
	}
	if strings.TrimSpace(input.Task) == "" {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	selection := input
	selection.AttachmentSetRef = ""
	kind := "AGENT"
	if targetType == "WORKFLOW" {
		kind = "WORKFLOW_STAGE"
		workflow, err := scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, current.organizationID, targetRef, current.role, current.actorID), true)
		if err != nil || workflow.Published == nil {
			return entity.PromptMaterializationSnapshot{}, errs.ErrConflict
		}
		if selection.WorkflowRevisionRef != "" && selection.WorkflowRevisionRef != workflow.Published.Ref {
			return entity.PromptMaterializationSnapshot{}, errs.ErrVersionMismatch
		}
		if selection.WorkflowStageKey != "" && selection.WorkflowStageKey != "workflow.coordinator.initial" {
			return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
		}
		selection.WorkflowRevisionRef = workflow.Published.Ref
		selection.WorkflowStageKey = "workflow.coordinator.initial"
	} else if targetType != "AGENT" {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	snapshot, err := repository.promptPreviewContextForActorTx(ctx, tx, current, execution, kind, targetRef, selection)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation agent context: %w", err)
	}
	if snapshot.ProjectRef != projectRef {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	agentRef := snapshot.ContextPin.AgentRef
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, agentRef)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	candidate, retained, err := checkedSessionModelCatalogSnapshot(ctx, tx, current.organizationID, sessionID, accountRef, view.Configuration, view.PublishedOverlay.Content, false)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation catalog: %w", err)
	}
	parsedOverlay, err := runtimecontract.ParseConfigOverlay(view.PublishedOverlay.Content)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("parse continuation overlay: %w", errs.ErrConflict)
	}
	effort := parsedOverlay.ModelReasoningEffort
	if effort == "" {
		effort = candidate.DefaultReasoningEffort
	}
	mode := runtimecontract.ReasoningSupported
	if effort == "" {
		mode = runtimecontract.ReasoningUnsupported
	}
	if input.AttachmentSetRef != "" {
		snapshot.TargetKind = promptservice.TargetSessionContinuation
		if err := repository.promptPreviewAttachmentsTx(ctx, tx, current, projectID, &snapshot, input.AttachmentSetRef); err != nil {
			return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation input files: %w", err)
		}
		snapshot.TargetKind = kind
	}
	if err := repository.promptPreviewSessionFilesTx(ctx, tx, current, sessionID, projectID, input.AttachmentSetRef, &snapshot); err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation history files: %w", err)
	}
	contextSnapshot, err := repository.runtimeContextSnapshotForActor(ctx, tx, execution, projectRef, agentRef)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation skills and memory: %w", err)
	}
	if execution.actorID != current.actorID {
		visible, err := repository.runtimeContextSnapshotForActor(ctx, tx, current, projectRef, agentRef)
		if err != nil {
			return entity.PromptMaterializationSnapshot{}, err
		}
		skills, memories := map[string]bool{}, map[string]bool{}
		for _, item := range visible.Skills {
			skills[item.RevisionRef] = true
		}
		for _, item := range visible.Memories {
			memories[item.RevisionRef] = true
		}
		for _, item := range contextSnapshot.Skills {
			if !skills[item.RevisionRef] {
				return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
			}
		}
		for _, item := range contextSnapshot.Memories {
			if !memories[item.RevisionRef] {
				return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
			}
		}
	}
	userCapabilities, grants, err := repository.agentCapabilityAuthority(ctx, tx, execution, projectRef, agentRef, snapshot.AgentCapabilities)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation capability authority: %w", err)
	}
	snapshot.UserCapabilities = userCapabilities
	materialized, err := promptservice.Materialize(snapshot.TemplateContent, promptservice.FromSnapshot(snapshot))
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("materialize continuation base: %w", errs.ErrConflict)
	}
	policy := view.Configuration.ProviderPolicy
	if retained != nil {
		policy.Ref, policy.Version, policy.Digest = retained.PolicyRef, retained.PolicyVersion, retained.PolicyDigest
	}
	next := map[string]any{"promptTemplateRef": snapshot.TemplateRef, "promptTemplateDigest": snapshot.TemplateDigest,
		"promptServiceTemplateRevision": materialized.ServiceTemplateRevision, "promptServiceTemplateDigest": materialized.ServiceTemplateDigest, "promptSnapshot": snapshot,
		"runtimeProvider": view.Configuration.Provider, "runtimeModel": view.Configuration.Model, "reasoningMode": mode, "effectiveReasoningEffort": effort,
		"imageManifestDigest":   view.Environment.CurrentVersion.Image.Digest,
		"runtimeEnvironmentRef": view.Environment.CurrentVersion.Ref, "runtimeEnvironmentVersion": view.Environment.CurrentVersion.Revision, "runtimeEnvironmentDigest": view.Environment.CurrentVersion.Digest,
		"environmentBindingRef": view.EnvironmentBinding.Ref, "environmentBindingVersion": view.EnvironmentBinding.Version, "environmentBindingDigest": view.EnvironmentBinding.Digest,
		"runtimeConfigRef": view.Configuration.Ref, "runtimeConfigVersion": view.Configuration.Version, "runtimeConfigDigest": view.Configuration.Digest,
		"providerPolicyRef": policy.Ref, "providerPolicyVersion": policy.Version, "providerPolicyDigest": policy.Digest,
		"configOverlayRef": view.PublishedOverlay.Ref, "configOverlayVersion": view.PublishedOverlay.Revision, "configOverlayDigest": view.PublishedOverlay.Digest,
		"contextSnapshot": contextSnapshot, "environmentTools": view.Environment.CurrentVersion.Tools, "integrationGrants": grants, "capabilities": materialized.EffectiveCapabilities,
		"artifacts": []any{}}
	// Файлы берутся из того же проверенного typed descriptor, без locator.
	for _, item := range snapshot.Artifacts {
		next["artifacts"] = append(next["artifacts"].([]any), map[string]any{"ref": item.Ref, "revision": item.Revision, "digest": item.Digest})
	}
	var previousID, readbackRef string
	var raw []byte
	if err := tx.QueryRow(ctx, queryPromptContinuationPrevious, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_ref": sessionRef}).Scan(&previousID, &readbackRef, &raw); err != nil || readbackRef != previousRef {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("resolve continuation predecessor: %w", errs.ErrConflict)
	}
	var previous map[string]any
	if json.Unmarshal(raw, &previous) != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrConflict
	}
	prior, err := continuationComponents(previous)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("project previous continuation components: %w", err)
	}
	currentComponents, err := continuationComponents(next)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("project current continuation components: %w", err)
	}
	diff, err := promptservice.CompareProspectiveRuntimeContexts(prior, currentComponents, promptservice.RuntimeDiff{PreviousRevisionRef: previousRef, SessionRef: sessionRef})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("compare prospective runtime descriptors: %v: %w", err, errs.ErrConflict)
	}
	snapshot.TemplateRef = defaultContinuationTemplateRef
	snapshot.TemplateContent = defaultContinuationTemplate
	digest := sha256.Sum256([]byte(snapshot.TemplateContent))
	snapshot.TemplateDigest = hex.EncodeToString(digest[:])
	err = tx.QueryRow(ctx, queryPromptContinuationTemplate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": agentRef}).Scan(&snapshot.TemplateRef, &snapshot.TemplateDigest, &snapshot.TemplateContent)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	digest = sha256.Sum256([]byte(snapshot.TemplateContent))
	if len(snapshot.TemplateContent) == 0 || len(snapshot.TemplateContent) > 16<<10 || !utf8.ValidString(snapshot.TemplateContent) ||
		strings.ContainsRune(snapshot.TemplateContent, 0) || snapshot.TemplateDigest != hex.EncodeToString(digest[:]) {
		return entity.PromptMaterializationSnapshot{}, fmt.Errorf("verify continuation template: %w", errs.ErrConflict)
	}
	snapshot.TargetKind, snapshot.TargetRef, snapshot.SessionRef = promptservice.TargetSessionContinuation, sessionRef, sessionRef
	snapshot.StagePurposeTemplate, snapshot.StageExpectedResultTemplate = "", ""
	snapshot.ExtraTemplates = nil
	snapshot.ContextPin.PreviousRuntimeRevisionRef = previousRef
	snapshot.ContextPin.Digest = ""
	dependencyPin := snapshot.ContextPin
	dependencyPin.PreviousRuntimeRevisionRef, dependencyPin.DependencyDigest = "", ""
	dependencyRaw, err := json.Marshal(struct {
		Actor, Session, Task, AttachmentSet, TemplateRef, TemplateDigest string
		Components                                                       map[string][]promptservice.RuntimeDescriptor
		ContextPin                                                       entity.PromptContextPin
	}{current.actorRef, sessionRef, input.Task, input.AttachmentSetRef, snapshot.TemplateRef, snapshot.TemplateDigest, currentComponents, dependencyPin})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	dependencyDigest := sha256.Sum256(dependencyRaw)
	snapshot.ContextPin.DependencyDigest = hex.EncodeToString(dependencyDigest[:])
	delete(snapshot.UnavailableVariables, "session.ref")
	raw, err = json.Marshal(diff)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	snapshot.SessionContinuation = string(raw)
	raw, err = json.Marshal(struct {
		Actor    string
		Snapshot entity.PromptMaterializationSnapshot
		Current  map[string]any
	}{current.actorRef, snapshot, next})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	digest = sha256.Sum256(raw)
	snapshot.ContextPin.Digest = hex.EncodeToString(digest[:])
	return snapshot, nil
}
