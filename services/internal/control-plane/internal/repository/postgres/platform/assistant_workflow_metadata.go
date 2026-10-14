package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// Полномочия и eligibility совпадают с публичным GetWorkflow; экран не
// назначает READ authority. Project helper сохраняет exact source boundary.
func (repository *Repository) assistantResolvedWorkflowMetadataTx(ctx context.Context, tx pgx.Tx, current scope, sourceScope, sourceProjectRef string, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: "PROJECT", OrganizationRef: current.organizationRef, Entries: []entity.AssistantConfigurationCatalogEntry{}}
	if input.EntityKind != "WORKFLOW" || input.EntityRef == "" || input.Query != "" || input.Offset != 0 || input.AccountRef != "" || input.RuntimeProfileRef != "" {
		return result, errs.ErrInvalid
	}
	if _, err := repository.resolveAssistantContext(ctx, tx, current, entity.AssistantContextDescriptor{EntityKind: "WORKFLOW", EntityRef: input.EntityRef}, ""); err != nil {
		return result, err
	}
	workflow, err := scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, current.organizationID, input.EntityRef, current.role, current.actorID), true)
	if err != nil {
		return result, err
	}
	if workflow.ProjectRef == "" || sourceScope == "PROJECT" && (sourceProjectRef == "" || workflow.ProjectRef != sourceProjectRef) {
		return result, errs.ErrNotFound
	}
	raw, err := assistantWorkflowMetadataJSON(workflow)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(raw)
	result.ProjectRef = workflow.ProjectRef
	result.WorkflowConfiguration = &entity.AssistantWorkflowConfiguration{WorkflowRef: workflow.Ref, ProjectRef: workflow.ProjectRef, Version: workflow.Version, ConfigurationJSON: raw, ConfigurationSHA256: hex.EncodeToString(digest[:])}
	return result, nil
}

// Только published graph, без private инструкции и draft/default/result fields.
func assistantWorkflowMetadataJSON(workflow entity.Workflow) ([]byte, error) {
	steps := []any{}
	coordinator, publishedRef := "", ""
	var concurrency, timeout, revision int64
	if p := workflow.Published; p != nil {
		if p.Ref == "" || p.VersionNumber < 1 || !validWorkflowVersion(*p) {
			return nil, errs.ErrUnavailable
		}
		coordinator, publishedRef = p.CoordinatorAgentRef, p.Ref
		concurrency, timeout, revision = int64(p.Concurrency), p.TimeoutSeconds, int64(p.VersionNumber)
		for _, step := range p.Steps {
			steps = append(steps, map[string]any{"key": step.Key, "position": step.Position, "name": step.Name, "agent_ref": step.AgentRef,
				"timeout_seconds": step.TimeoutSeconds, "parallel": step.Parallel, "parallel_group": step.ParallelGroup,
				"human_gate": step.HumanGateAfter, "gate_decisions": append([]string{}, step.GateDecisions...), "depends_on": append([]string{}, step.DependsOn...)})
		}
	}
	raw, err := json.Marshal(map[string]any{"workflow_ref": workflow.Ref, "project_ref": workflow.ProjectRef, "version": workflow.Version,
		"state": workflow.State, "name": workflow.Name, "published_version_ref": publishedRef, "published_revision": revision,
		"coordinator_agent_ref": coordinator, "max_concurrency": concurrency, "timeout_seconds": timeout, "steps": steps})
	if err != nil || len(raw) > 1<<20 {
		return nil, errs.ErrUnavailable
	}
	return raw, nil
}
