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

// assistantResolvedWorkflowMetadataTx accepts only a direct WORKFLOW catalog lookup, resolves the
// same assistant/visibility boundary as workflow reads and enforces the source project when
// applicable. It returns bounded published metadata with a digest; it neither exposes private
// instructions nor grants launch/delegation authority.
// assistantResolvedWorkflowMetadataTx принимает только прямой WORKFLOW запрос каталога, разрешает
// ту же границу контекста и видимости, что чтение workflow, и при необходимости требует исходный
// проект. Возвращает ограниченные опубликованные метаданные с дайджестом; не раскрывает закрытые
// инструкции и не выдаёт право запуска или делегирования.
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

// assistantWorkflowMetadataJSON validates an available published workflow structure and serializes
// only public graph/configuration metadata, with copied dependency/gate slices and a 1 MiB result
// limit. Without publication it returns empty published fields/steps; private instructions,
// draft/default/input/result fields are excluded.
// assistantWorkflowMetadataJSON проверяет имеющуюся опубликованную структуру workflow и
// сериализует только публичные метаданные графа и конфигурации, копируя срезы зависимостей и gates
// и ограничивая результат 1 МиБ. Без публикации поля и шаги публикации пусты; закрытые инструкции,
// черновик, defaults, input и result исключены.
func assistantWorkflowMetadataJSON(workflow entity.Workflow) ([]byte, error) {
	steps := []any{}
	coordinator, publishedRef := "", ""
	var concurrency, timeout, revision int64
	if p := workflow.Published; p != nil {
		if p.Ref == "" || p.VersionNumber < 1 || workflowVersionStructureInvalidField(*p) != "" {
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
