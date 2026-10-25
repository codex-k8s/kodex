package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPromptRuntimeContext embeds exact node locale/agent version and pinned root workflow
// revision/spec lookup within the organization. Published row identity is returned separately from
// the version number carried in the stored specification.
// queryPromptRuntimeContext встраивает поиск locale и версии агента точного узла и закреплённой
// ревизии и спецификации корневого workflow внутри организации. Идентичность опубликованной строки
// возвращается отдельно от номера версии внутри сохранённой спецификации.
//
//go:embed sql/prompt_runtime_context.sql
var queryPromptRuntimeContext string

// hydrateRuntimePromptContext loads current node metadata, binds platform instructions and
// assistant core, then validates any pinned workflow specification and exact step/agent match. It
// overwrites published revision identity from database columns and hydrates workflow
// template/execution context in order. Errors may leave a partial snapshot; no runtime revision,
// grant or commit is created.
// hydrateRuntimePromptContext загружает данные текущего узла, связывает платформенные инструкции и
// ядро ассистента, затем проверяет закреплённую спецификацию workflow и точное совпадение шага и
// агента. Назначает опубликованную идентичность ревизии из колонок БД и последовательно добавляет
// контекст шаблона и исполнения. Ошибки могут оставлять частичный снимок; runtime-ревизия, grant и
// Commit не создаются.
func (repository *Repository) hydrateRuntimePromptContext(ctx context.Context, tx pgx.Tx, current scope, nodeRef string, snapshot *entity.PromptMaterializationSnapshot) error {
	var workflowRef, revisionRef string
	var revision int64
	var raw []byte
	if err := tx.QueryRow(ctx, queryPromptRuntimeContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_ref": nodeRef}).Scan(
		&snapshot.Locale, &snapshot.ContextPin.AgentVersion, &workflowRef, &revisionRef, &revision, &raw); err != nil {
		return errs.ErrConflict
	}
	snapshot.ServiceTemplateRevision = promptservice.ServiceTemplateRevision
	snapshot.ContextPin.AgentRef = snapshot.Variables["agent.ref"]
	if err := bindPlatformInstructions(snapshot); err != nil {
		return err
	}
	if err := repository.hydrateAssistantCoreTx(ctx, tx, current, snapshot); err != nil {
		return err
	}
	if workflowRef == "" {
		return nil
	}
	var version entity.WorkflowVersion
	if json.Unmarshal(raw, &version) != nil || !validWorkflowVersion(version) {
		return errs.ErrConflict
	}
	version.Ref = revisionRef
	// Spec сохраняет draft number; published identity принадлежит строке version.
	version.VersionNumber = int32(revision)
	step, ok := promptWorkflowStep(version, snapshot.Variables["workflow.stage.key"])
	if !ok || step.AgentRef != snapshot.Variables["agent.ref"] {
		return errs.ErrConflict
	}
	applyWorkflowPromptContext(snapshot, workflowRef, revision, version, step)
	if err := repository.hydrateWorkflowPromptTemplateTx(ctx, tx, current, snapshot); err != nil {
		return err
	}
	return repository.hydrateWorkflowExecutionPromptTx(ctx, tx, current, nodeRef, snapshot)
}

// promptWorkflowStep returns an explicit matching step or synthesizes the initial/coordinator
// continuation step for a canonical positive 32-bit attempt suffix. Coordinator instructions fall
// back to workflow purpose and request platform.run.delegate; the returned capability requirement
// is not a delegation grant.
// promptWorkflowStep возвращает явно совпавший шаг либо синтезирует initial/coordinator
// continuation шаг для канонического положительного 32-битного номера попытки. Инструкции
// координатора при пустоте заменяются purpose workflow и требуют platform.run.delegate; это
// требование capability, а не разрешение делегирования.
func promptWorkflowStep(version entity.WorkflowVersion, key string) (entity.WorkflowStep, bool) {
	for _, step := range version.Steps {
		if step.Key == key {
			return step, true
		}
	}
	continuation := strings.TrimPrefix(key, "workflow.coordinator.continue.")
	attempt, parseErr := strconv.ParseInt(continuation, 10, 32)
	if key == "workflow.coordinator.initial" || (continuation != key && parseErr == nil && attempt > 0 && strconv.FormatInt(attempt, 10) == continuation) {
		instructions := version.Instructions
		if strings.TrimSpace(instructions) == "" {
			instructions = version.Purpose
		}
		return entity.WorkflowStep{Key: key, Name: version.Name, AgentRef: version.CoordinatorAgentRef, Instructions: instructions,
			ExpectedResult: version.CompletionCriteria, RequiredCapabilityKeys: []string{"platform.run.delegate"}}, true
	}
	return entity.WorkflowStep{}, false
}

// applyWorkflowPromptContext mutates a prepared snapshot with exact workflow/stage pins, copied
// required capabilities, stage templates and a compact published DAG. Dependency/gate slices are
// copied and structured workflow maps initialized; the projection describes publication, not live
// frontier or execution authority.
// applyWorkflowPromptContext изменяет подготовленный снимок, назначая точные workflow/stage pins,
// копию требуемых capabilities, шаблоны шага и компактный опубликованный DAG. Срезы зависимостей и
// gates копируются, структурные карты workflow инициализируются; проекция описывает публикацию, а
// не текущий frontier и не полномочия исполнения.
func applyWorkflowPromptContext(snapshot *entity.PromptMaterializationSnapshot, workflowRef string, workflowVersion int64, version entity.WorkflowVersion, step entity.WorkflowStep) {
	snapshot.TargetKind, snapshot.TargetRef = promptservice.TargetWorkflowStage, step.Key
	snapshot.ContextPin.WorkflowRef, snapshot.ContextPin.WorkflowVersion = workflowRef, workflowVersion
	snapshot.ContextPin.WorkflowRevisionRef, snapshot.ContextPin.WorkflowStageKey = version.Ref, step.Key
	snapshot.WorkflowCapabilities = append([]string{}, step.RequiredCapabilityKeys...)
	snapshot.Variables["workflow.ref"], snapshot.Variables["workflow.name"], snapshot.Variables["workflow.purpose"] = workflowRef, version.Name, version.Purpose
	snapshot.Variables["workflow.stage.key"], snapshot.Variables["step.key"], snapshot.Variables["step.name"] = step.Key, step.Key, step.Name
	snapshot.Variables["step.purpose"], snapshot.Variables["step.expected_result"] = step.Instructions, step.ExpectedResult
	snapshot.StagePurposeTemplate, snapshot.StageExpectedResultTemplate = step.Instructions, step.ExpectedResult
	snapshot.WorkflowStage = step.Name
	if snapshot.StructuredVariables == nil {
		snapshot.StructuredVariables = make(map[string]any)
	}
	workflow, ok := snapshot.StructuredVariables["workflow"].(map[string]any)
	if !ok {
		workflow = make(map[string]any)
		snapshot.StructuredVariables["workflow"] = workflow
	}
	// Полный pinned DAG — данные, не текущий frontier и не право делегирования.
	steps := make([]promptWorkflowStepDescriptor, 0, len(version.Steps))
	for _, published := range version.Steps {
		steps = append(steps, promptWorkflowStepDescriptor{
			Key: published.Key, Name: published.Name, AgentRef: published.AgentRef, Position: published.Position,
			DependsOn: append([]string{}, published.DependsOn...), Parallel: published.Parallel, ParallelGroup: published.ParallelGroup,
			HumanGateAfter: published.HumanGateAfter, GateDecisions: append([]string{}, published.GateDecisions...),
		})
	}
	workflow["publication"] = promptWorkflowPublication{
		RevisionRef: version.Ref, VersionNumber: version.VersionNumber, CoordinatorAgentRef: version.CoordinatorAgentRef, Steps: steps,
	}
}

// promptWorkflowPublication carries exact published revision identity/number, coordinator and
// compact steps for prompt navigation. It excludes instructions, input values, result schemas and
// capability grants and does not represent the current runnable frontier.
// promptWorkflowPublication хранит точные идентичность и номер опубликованной ревизии,
// координатора и компактные шаги для навигации в prompt. Исключает инструкции, input values,
// result schemas и grants и не представляет текущий исполнимый frontier.
type promptWorkflowPublication struct {
	RevisionRef         string                         `json:"revision_ref"`
	VersionNumber       int32                          `json:"version_number"`
	CoordinatorAgentRef string                         `json:"coordinator_agent_ref"`
	Steps               []promptWorkflowStepDescriptor `json:"steps"`
}

// promptWorkflowStepDescriptor serializes step identity, agent, position, dependency/parallel and
// human-gate metadata in the compact published DAG. These structural hints do not authorize node
// creation, delegation or a gate decision.
// promptWorkflowStepDescriptor сериализует идентичность шага, агента, позицию, зависимости,
// параллельность и human-gate метаданные компактного опубликованного DAG. Эти структурные сведения
// не разрешают создание узла, делегирование и решение gate.
type promptWorkflowStepDescriptor struct {
	Key            string   `json:"step_key"`
	Name           string   `json:"name"`
	AgentRef       string   `json:"agent_ref"`
	Position       int32    `json:"position"`
	DependsOn      []string `json:"depends_on"`
	Parallel       bool     `json:"parallel"`
	ParallelGroup  int32    `json:"parallel_group"`
	HumanGateAfter bool     `json:"human_gate_after"`
	GateDecisions  []string `json:"gate_decisions"`
}
