package platform

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantWorkflowEditableFields lists the workflow form fields accepted by hydration and
// edited-plan reconstruction, in stable copy order. Workflow/project refs and the retained typed
// draft are handled separately; membership in this list grants no permission and does not validate
// a supplied value.
// assistantWorkflowEditableFields перечисляет поля формы workflow для hydration и восстановления
// edited plan в устойчивом порядке копирования. Workflow/project refs и сохранённый typed draft
// обрабатываются отдельно; наличие в списке не выдаёт полномочий и не проверяет переданное
// значение.
var assistantWorkflowEditableFields = []string{
	"name", "purpose", "coordinatorAgentRef", "instructions", "completionCriteria",
	"maxConcurrency", "timeoutSeconds", "inputFields", "steps",
}

// assistantWorkflowGraphFields projects typed workflow inputs/steps into fresh JSON-shaped maps
// and nonnil slices, including options, gate decisions and required capabilities. Numeric
// parallel-group/timeouts become float64. This editor projection omits defaults and dependency
// edges; those remain in the retained draft and are preserved separately during graph
// reconstruction.
// assistantWorkflowGraphFields проецирует типизированные workflow inputs/steps в новые
// JSON-подобные maps и ненулевые slices, включая options, gate decisions и required capabilities.
// Числовые parallel-group/timeouts становятся float64. Проекция редактора не включает defaults и
// dependency edges: они остаются в сохранённом draft и отдельно сохраняются при восстановлении
// графа.
func assistantWorkflowGraphFields(draft entity.WorkflowVersion) ([]any, []any) {
	fields := make([]any, 0, len(draft.Inputs))
	for _, field := range draft.Inputs {
		options := make([]any, 0, len(field.Options))
		for _, option := range field.Options {
			options = append(options, option)
		}
		fields = append(fields, map[string]any{"key": field.Key, "label": field.Label,
			"description": field.Help, "valueType": field.Type, "required": field.Required, "options": options})
	}
	steps := make([]any, 0, len(draft.Steps))
	for _, step := range draft.Steps {
		decisions := make([]any, 0, len(step.GateDecisions))
		for _, decision := range step.GateDecisions {
			decisions = append(decisions, decision)
		}
		capabilities := make([]any, 0, len(step.RequiredCapabilityKeys))
		for _, capability := range step.RequiredCapabilityKeys {
			capabilities = append(capabilities, capability)
		}
		steps = append(steps, map[string]any{"key": step.Key, "name": step.Name,
			"purpose": step.Instructions, "agentRef": step.AgentRef, "parallel": step.Parallel,
			"parallelGroup": float64(step.ParallelGroup), "timeoutSeconds": float64(step.TimeoutSeconds),
			"expectedResult": step.ExpectedResult, "humanGate": step.HumanGateAfter,
			"gateDecisions": decisions, "requiredCapabilityKeys": capabilities})
	}
	return fields, steps
}

// readAssistantWorkflowSnapshot resolves the exact workflow assistant context in the caller
// transaction and requires UPDATE_WORKFLOW before reading the tenant/ref projection. It rejects a
// different project, archived workflow, missing draft or absent EDIT action as NotFound. The
// returned snapshot includes JSON-round-tripped draft and editable graph fields with the
// authoritative workflow version; encoding/decoding failure is Unavailable. It creates no
// transaction, grant or write.
// readAssistantWorkflowSnapshot разрешает точный assistant context workflow в транзакции
// вызывающего и требует UPDATE_WORKFLOW до чтения tenant/ref projection. Чужой проект, архивный
// workflow, отсутствие draft или EDIT action отклоняет как NotFound. Snapshot содержит draft после
// JSON round-trip и редактируемые graph fields с авторитетной workflow version; отказ
// кодирования/декодирования даёт Unavailable. Транзакцию, grant или запись не создаёт.
func (repository *Repository) readAssistantWorkflowSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, workflowRef string,
) (map[string]any, int64, error) {
	projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "WORKFLOW", EntityRef: workflowRef}, projectRef)
	if err != nil {
		return nil, 0, err
	}
	if !contains(projection.AllowedOperations, "UPDATE_WORKFLOW") {
		return nil, 0, errs.ErrForbidden
	}
	workflow, err := scanWorkflow(tx.QueryRow(ctx, queryCommandsChangeworkflowSelectAuthoritativeReadback,
		actorScope.organizationID, workflowRef), false)
	if err != nil {
		return nil, 0, err
	}
	if workflow.ProjectRef != projectRef || workflow.State == "ARCHIVED" || workflow.Draft == nil ||
		!contains(workflow.NextActions, "EDIT") {
		return nil, 0, errs.ErrNotFound
	}
	raw, err := json.Marshal(workflow.Draft)
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	var draft map[string]any
	if json.Unmarshal(raw, &draft) != nil || draft == nil {
		return nil, 0, errs.ErrUnavailable
	}
	fields, steps := assistantWorkflowGraphFields(*workflow.Draft)
	return map[string]any{
		"workflowRef": workflow.Ref, "projectRef": workflow.ProjectRef,
		"name": workflow.Draft.Name, "purpose": workflow.Draft.Purpose,
		"coordinatorAgentRef": workflow.Draft.CoordinatorAgentRef,
		"instructions":        workflow.Draft.Instructions, "completionCriteria": workflow.Draft.CompletionCriteria,
		"maxConcurrency": float64(workflow.Draft.Concurrency), "timeoutSeconds": float64(workflow.Draft.TimeoutSeconds),
		"inputFields": fields, "steps": steps,
		"draft": draft,
	}, workflow.Version, nil
}

// hydrateAssistantWorkflowOperation accepts only workflowRef plus the closed editable fields under
// a nonempty server-supplied project, using field-specific shape/ref errors. It reads the
// authoritative owner-scoped snapshot/version in the supplied transaction and delegates pure
// hydration. Parameter refs locate a resource and never replace context authorization.
// hydrateAssistantWorkflowOperation принимает только workflowRef и закрытые editable fields при
// непустом server-supplied project с адресными shape/ref errors. Читает авторитетные owner-scoped
// snapshot/version в переданной транзакции и делегирует чистую hydration. Parameter refs служат
// locator ресурса и не заменяют авторизацию контекста.
func (repository *Repository) hydrateAssistantWorkflowOperation(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, append([]string{"workflowRef"}, assistantWorkflowEditableFields...)...) {
		return entity.AssistantPlanOperation{}, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_SHAPE")
	}
	ref := assistantString(operation.Parameters, "workflowRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_REF")
	}
	before, version, err := repository.readAssistantWorkflowSnapshot(ctx, tx, actorScope, projectRef, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantWorkflowFields(before, version, operation)
}

// hydrateAssistantWorkflowFields builds a fresh editable map from the snapshot, trims supplied
// text, bounds concurrency/timeouts and requires JSON-shaped graph arrays. Unchanged values are
// Conflict. It restores Before/target/version, selects the operation, clears Input and validates
// the typed workflow command before deriving canonical After with graph/draft. Map copies remain
// shallow; this pure transformation performs no authorization, persistence or publication.
// hydrateAssistantWorkflowFields строит новую editable map из snapshot, обрезает пробелы
// переданного текста, ограничивает concurrency/timeouts и требует JSON-подобные graph arrays.
// Неизменённые значения дают Conflict. Восстанавливает Before/target/version, выбирает operation,
// очищает Input и проверяет typed workflow command перед построением canonical After с
// graph/draft. Копии maps остаются поверхностными; чистое преобразование не выполняет авторизацию,
// сохранение или публикацию.
func hydrateAssistantWorkflowFields(before map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	after := make(map[string]any, len(assistantWorkflowEditableFields)+2)
	for _, key := range append([]string{"workflowRef", "projectRef"}, assistantWorkflowEditableFields...) {
		after[key] = before[key]
	}
	changed := false
	for _, key := range assistantWorkflowEditableFields {
		value, supplied := operation.Parameters[key]
		if !supplied {
			continue
		}
		switch key {
		case "maxConcurrency", "timeoutSeconds":
			integer, valid := assistantInt64(operation.Parameters, key)
			if !valid || integer < 1 || (key == "maxConcurrency" && integer > 100) ||
				(key == "timeoutSeconds" && integer > 7*24*60*60) {
				field := "MAX_CONCURRENCY"
				if key == "timeoutSeconds" {
					field = "TIMEOUT_SECONDS"
				}
				return entity.AssistantPlanOperation{}, errs.WithAssistantPlanField(errs.ErrInvalid, field)
			}
			value = float64(integer)
		case "inputFields", "steps":
			if _, valid := value.([]any); !valid {
				field := "INPUT_FIELDS"
				if key == "steps" {
					field = "STEPS"
				}
				return entity.AssistantPlanOperation{}, errs.WithAssistantPlanField(errs.ErrInvalid, field)
			}
		default:
			text, valid := value.(string)
			if !valid {
				return entity.AssistantPlanOperation{}, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_TEXT")
			}
			value = strings.TrimSpace(text)
		}
		changed = changed || !reflect.DeepEqual(after[key], value)
		after[key] = value
	}
	if !changed {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.Parameters = after
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "WORKFLOW", Ref: assistantString(before, "workflowRef"),
		Name: assistantString(before, "name"), Version: &version}
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	payload, _, err := assistantUpdateWorkflow(operation)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	operation.After, err = assistantWorkflowAfterSnapshot(operation.Parameters, *payload.Draft)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return operation, nil
}

// assistantWorkflowAfterSnapshot JSON-round-trips the typed draft, shallow-copies parameters and
// replaces inputFields/steps with the typed editor projection plus the normalized draft. It maps
// marshal/unmarshal failure to Invalid and retains unrelated parameter values/aliases. The result
// is a display/comparison snapshot, not caller authority.
// assistantWorkflowAfterSnapshot выполняет JSON round-trip typed draft, поверхностно копирует
// parameters и заменяет inputFields/steps типизированной editor projection с normalized draft.
// Отказ marshal/unmarshal отображает в Invalid, остальные parameter values/aliases сохраняет.
// Результат является snapshot для отображения/сравнения, а не полномочиями вызывающего.
func assistantWorkflowAfterSnapshot(parameters map[string]any, draft entity.WorkflowVersion) (map[string]any, error) {
	raw, err := json.Marshal(draft)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	var normalized map[string]any
	if json.Unmarshal(raw, &normalized) != nil {
		return nil, errs.ErrInvalid
	}
	after := cloneAssistantFields(parameters)
	after["inputFields"], after["steps"] = assistantWorkflowGraphFields(draft)
	after["draft"] = normalized
	return after, nil
}

// assistantUpdateWorkflow parses Input when present or parameters with the expected-version
// fallback, requiring the exact workflow field set, positive version and refs matching Before. It
// rebuilds the retained valid draft, applies typed text/numeric edits and reconstructs a changed
// graph while preserving protected defaults/dependencies, then validates the final workflow. It
// returns a specialized WorkflowInput/version with field diagnostics; it does not itself resolve
// current ownership or commit a command.
// assistantUpdateWorkflow разбирает Input при наличии либо parameters с fallback expected version,
// требуя точный workflow field set, положительную version и refs, совпадающие с Before.
// Восстанавливает сохранённый допустимый draft, применяет typed text/numeric edits и перестраивает
// изменённый граф с сохранением защищённых defaults/dependencies, затем валидирует итоговый
// workflow. Возвращает специализированные WorkflowInput/version с field diagnostics; сам не
// разрешает текущее владение и не фиксирует команду.
func assistantUpdateWorkflow(operation entity.AssistantPlanOperation) (command.WorkflowInput, int64, error) {
	input := operation.Input
	if input == nil {
		input = withAssistantExpectedVersion(operation.Parameters, valueOrZero(operation.ExpectedVersion))
	}
	if !onlyAssistantFields(input, "workflowRef", "projectRef", "name", "purpose", "instructions",
		"coordinatorAgentRef", "completionCriteria", "maxConcurrency", "timeoutSeconds", "inputFields", "steps", "expectedVersion") ||
		!hasAssistantFields(input, "workflowRef", "projectRef", "name", "purpose", "instructions",
			"coordinatorAgentRef", "completionCriteria", "maxConcurrency", "timeoutSeconds", "inputFields", "steps", "expectedVersion") {
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_SHAPE")
	}
	version, valid := assistantInt64(input, "expectedVersion")
	if !valid || version < 1 || assistantString(input, "workflowRef") == "" ||
		assistantString(input, "projectRef") == "" || assistantString(input, "workflowRef") != assistantString(operation.Before, "workflowRef") ||
		assistantString(input, "projectRef") != assistantString(operation.Before, "projectRef") {
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_BINDING")
	}
	concurrency, validConcurrency := assistantInt64(input, "maxConcurrency")
	timeout, validTimeout := assistantInt64(input, "timeoutSeconds")
	if !validConcurrency || !validTimeout || concurrency < 1 || concurrency > 100 || timeout < 1 || timeout > 7*24*60*60 {
		field := "TIMEOUT_SECONDS"
		if !validConcurrency || concurrency < 1 || concurrency > 100 {
			field = "MAX_CONCURRENCY"
		}
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, field)
	}
	raw, err := json.Marshal(operation.Before["draft"])
	if err != nil {
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_DRAFT")
	}
	var draft entity.WorkflowVersion
	if json.Unmarshal(raw, &draft) != nil || !validWorkflowVersion(draft) {
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_DRAFT")
	}
	for _, key := range []string{"name", "purpose", "instructions", "completionCriteria"} {
		value, ok := input[key].(string)
		if !ok {
			return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_TEXT")
		}
		switch key {
		case "name":
			draft.Name = value
		case "purpose":
			draft.Purpose = value
		case "instructions":
			draft.Instructions = value
		case "completionCriteria":
			draft.CompletionCriteria = value
		}
	}
	draft.Concurrency, draft.TimeoutSeconds = int32(concurrency), timeout
	if coordinator, ok := input["coordinatorAgentRef"].(string); ok {
		draft.CoordinatorAgentRef = strings.TrimSpace(coordinator)
	} else {
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_TEXT")
	}
	if !reflect.DeepEqual(input["inputFields"], operation.Before["inputFields"]) ||
		!reflect.DeepEqual(input["steps"], operation.Before["steps"]) {
		if err := assistantUpdateWorkflowGraph(input, &draft); err != nil {
			return command.WorkflowInput{}, 0, err
		}
	}
	if !validWorkflowVersion(draft) {
		return command.WorkflowInput{}, 0, errs.WithAssistantPlanField(errs.ErrInvalid, workflowVersionInvalidField(draft))
	}
	return command.WorkflowInput{Ref: assistantString(input, "workflowRef"), ProjectRef: assistantString(input, "projectRef"),
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: draft.CoordinatorAgentRef, Draft: &draft}, version, nil
}

// assistantUpdateWorkflowGraph parses bounded input/step arrays with closed shapes, rejects
// unknown explicit or duplicate keys and assigns unused server keys to new entries. It
// reconstructs the typed graph, restores matching input defaults, remaps new dependency keys and
// retains original dependency edges before replacing draft inputs/steps. Errors carry the existing
// field diagnostics and leave that final replacement unapplied; no stored workflow is written.
// assistantUpdateWorkflowGraph разбирает ограниченные input/step arrays с закрытыми shapes,
// запрещает неизвестные явные или повторные keys и назначает свободные server keys новым
// элементам. Восстанавливает typed graph, возвращает соответствующие input defaults, переназначает
// новые dependency keys и сохраняет исходные dependency edges до замены draft inputs/steps. Ошибки
// несут прежние field diagnostics и не выполняют эту итоговую замену; сохранённый workflow не
// записывается.
func assistantUpdateWorkflowGraph(input map[string]any, draft *entity.WorkflowVersion) error {
	fields, fieldsOK := input["inputFields"].([]any)
	steps, stepsOK := input["steps"].([]any)
	if !fieldsOK || !stepsOK || len(fields) > 100 || len(steps) == 0 || len(steps) > 200 {
		return errs.WithAssistantPlanField(errs.ErrInvalid, "WORKFLOW_SHAPE")
	}
	previousFields := make(map[string]struct{}, len(draft.Inputs))
	for _, field := range draft.Inputs {
		previousFields[field.Key] = struct{}{}
	}
	previousSteps := make(map[string]struct{}, len(draft.Steps))
	for _, step := range draft.Steps {
		previousSteps[step.Key] = struct{}{}
	}
	fieldKeys, stepKeys := make([]string, len(fields)), make([]string, len(steps))
	usedFields, usedSteps := map[string]struct{}{}, map[string]struct{}{}
	cleanFields, cleanSteps := make([]any, 0, len(fields)), make([]any, 0, len(steps))
	for index, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok || !onlyAssistantFields(field, "key", "label", "description", "valueType", "required", "options") {
			return errs.WithAssistantPlanField(errs.ErrInvalid, "INPUT_FIELDS")
		}
		key := assistantString(field, "key")
		if key != "" {
			if _, known := previousFields[key]; !known {
				return errs.WithAssistantPlanField(errs.ErrInvalid, "INPUT_FIELD_KEY")
			}
		} else {
			for candidate := 1; ; candidate++ {
				key = "field-" + leftPad(candidate, 3)
				_, previous := previousFields[key]
				_, used := usedFields[key]
				if !previous && !used {
					break
				}
			}
		}
		if _, duplicate := usedFields[key]; duplicate {
			return errs.WithAssistantPlanField(errs.ErrInvalid, "INPUT_FIELD_KEY")
		}
		usedFields[key], fieldKeys[index] = struct{}{}, key
		clean := cloneAssistantFields(field)
		delete(clean, "key")
		cleanFields = append(cleanFields, clean)
	}
	for index, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok || !onlyAssistantFields(step, "key", "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") {
			return errs.WithAssistantPlanField(errs.ErrInvalid, "STEP_SHAPE")
		}
		key := assistantString(step, "key")
		if key != "" {
			if _, known := previousSteps[key]; !known {
				return errs.WithAssistantPlanField(errs.ErrInvalid, "STEP_KEY")
			}
		} else {
			for candidate := 1; ; candidate++ {
				key = "step-" + leftPad(candidate, 3)
				_, previous := previousSteps[key]
				_, used := usedSteps[key]
				if !previous && !used {
					break
				}
			}
		}
		if _, duplicate := usedSteps[key]; duplicate {
			return errs.WithAssistantPlanField(errs.ErrInvalid, "STEP_KEY")
		}
		usedSteps[key], stepKeys[index] = struct{}{}, key
		clean := cloneAssistantFields(step)
		delete(clean, "key")
		cleanSteps = append(cleanSteps, clean)
	}
	proposal := map[string]any{"projectRef": assistantString(input, "projectRef"),
		"name": assistantString(input, "name"), "purpose": assistantString(input, "purpose"),
		"coordinatorAgentRef": assistantString(input, "coordinatorAgentRef"),
		"inputFields":         cleanFields, "steps": cleanSteps,
		"maxConcurrency": input["maxConcurrency"], "timeoutSeconds": input["timeoutSeconds"],
		"completionCriteria": assistantString(input, "completionCriteria")}
	parsed, err := assistantWorkflow(proposal)
	if err != nil || parsed.Draft == nil {
		field := errs.AssistantPlanField(err)
		if field == "" {
			field = "WORKFLOW_INVARIANTS"
		}
		return errs.WithAssistantPlanField(errs.ErrInvalid, field)
	}
	for index := range parsed.Draft.Inputs {
		parsed.Draft.Inputs[index].Key = fieldKeys[index]
		for _, old := range draft.Inputs {
			if old.Key == fieldKeys[index] {
				parsed.Draft.Inputs[index].DefaultValue = old.DefaultValue
				break
			}
		}
	}
	remap := make(map[string]string, len(stepKeys))
	for index := range parsed.Draft.Steps {
		remap[parsed.Draft.Steps[index].Key] = stepKeys[index]
	}
	for index := range parsed.Draft.Steps {
		step := &parsed.Draft.Steps[index]
		step.Key = stepKeys[index]
		for dependencyIndex, dependency := range step.DependsOn {
			step.DependsOn[dependencyIndex] = remap[dependency]
		}
	}
	if err := retainAssistantWorkflowDependencies(draft.Steps, parsed.Draft.Steps); err != nil {
		return errs.WithAssistantPlanField(err, "STEPS_GRAPH")
	}
	draft.Inputs, draft.Steps = parsed.Draft.Inputs, parsed.Draft.Steps
	return nil
}

// retainAssistantWorkflowDependencies mutates proposed steps to retain old edges for existing
// keys, adding reconstructed edges only when key order/parallel structure changed. Every
// dependency must refer to a unique earlier proposed step, and step keys must be unique; otherwise
// it returns Invalid. Original dependency slices are copied, but a failure may leave earlier
// proposed steps modified, so callers discard the failed proposal.
// retainAssistantWorkflowDependencies изменяет proposed steps с сохранением прежних edges для
// существующих keys и добавляет восстановленные edges только при изменении порядка keys/parallel
// structure. Каждая dependency должна ссылаться на уникальный более ранний proposed step, keys
// шагов также уникальны; иначе возвращает Invalid. Исходные dependency slices копируются, но при
// отказе ранние proposed steps могут остаться изменёнными, поэтому вызывающий отбрасывает
// неуспешный proposal.
func retainAssistantWorkflowDependencies(original, proposed []entity.WorkflowStep) error {
	previous := make(map[string]entity.WorkflowStep, len(original))
	unchanged := len(original) == len(proposed)
	for index, step := range original {
		previous[step.Key] = step
		if unchanged && (step.Key != proposed[index].Key || step.Parallel != proposed[index].Parallel || step.ParallelGroup != proposed[index].ParallelGroup) {
			unchanged = false
		}
	}
	seen := make(map[string]bool, len(proposed))
	for index := range proposed {
		step := &proposed[index]
		dependencies := append([]string(nil), step.DependsOn...)
		if old, exists := previous[step.Key]; exists {
			// Изменение структуры добавляет зависимости фронта, не удаляя исходные рёбра.
			step.DependsOn = append([]string(nil), old.DependsOn...)
			if !unchanged {
				for _, dependency := range dependencies {
					if !contains(step.DependsOn, dependency) {
						step.DependsOn = append(step.DependsOn, dependency)
					}
				}
			}
		}
		unique := make(map[string]bool, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			if !seen[dependency] || unique[dependency] {
				return errs.ErrInvalid
			}
			unique[dependency] = true
		}
		if seen[step.Key] {
			return errs.ErrInvalid
		}
		seen[step.Key] = true
	}
	return nil
}

// valueOrZero returns zero for a nil int64 pointer and otherwise the pointed value without
// changing it. The fallback preserves absence for later validation; zero does not become a valid
// optimistic-lock version or an authority decision.
// valueOrZero возвращает ноль для nil int64 pointer, иначе его значение без изменения. Fallback
// сохраняет отсутствие для последующей валидации; ноль не становится допустимой optimistic-lock
// version или решением о полномочиях.
func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// rehydrateEditedAssistantWorkflow requires the original UPDATE_WORKFLOW key/kind/ref and positive
// version, a closed edited field set and unchanged workflow/project refs before invoking the
// shared pure reconstruction. Only supplied editable values are forwarded; the original
// Before/version remain authoritative and the caller selection is restored after workflow-specific
// canonical hydration. Forged snapshot/target/version fields cannot replace the original envelope
// through this path.
// rehydrateEditedAssistantWorkflow требует исходные UPDATE_WORKFLOW key/kind/ref и положительную
// version, закрытый edited field set и неизменённые workflow/project refs до общей чистой
// реконструкции. Передаются только заданные editable values; исходные Before/version остаются
// авторитетными, выбор вызывающего восстанавливается после специализированной canonical hydration
// workflow. Подделанные snapshot/target/version fields не заменяют исходную оболочку этим путём.
func rehydrateEditedAssistantWorkflow(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_WORKFLOW" || original.Key != edited.Key || original.Target.Kind != "WORKFLOW" ||
		original.Target.Ref == "" || original.ExpectedVersion == nil || *original.ExpectedVersion < 1 ||
		edited.Parameters == nil || !onlyAssistantFields(edited.Parameters,
		append([]string{"workflowRef", "projectRef"}, assistantWorkflowEditableFields...)...) ||
		assistantString(edited.Parameters, "workflowRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Before, "projectRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	return rehydrateAssistantEditableFields(original, edited, map[string]any{"workflowRef": original.Target.Ref}, assistantWorkflowEditableFields, hydrateAssistantWorkflowFields)
}

// assistantWorkflowUpdateSnapshotMatches re-reads the current owner-scoped workflow snapshot and
// compares both version pins, target name, complete Before and workflow/project refs. It then
// clears canonical Input, rebuilds the typed command and normalized After and compares JSON
// representations. Read/validation errors propagate; false means mismatch, not authorization or a
// write. Its canonical-After policy deliberately differs from schedule map equality.
// assistantWorkflowUpdateSnapshotMatches повторно читает текущий owner-scoped workflow snapshot и
// сравнивает обе version pins, target name, полный Before и workflow/project refs. Затем очищает
// canonical Input, восстанавливает typed command и normalized After и сравнивает JSON
// representations. Ошибки чтения/валидации передаются выше; false означает несовпадение, а не
// авторизацию или запись. Политика canonical After намеренно отличается от schedule map equality.
func (repository *Repository) assistantWorkflowUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	before, version, err := repository.readAssistantWorkflowSnapshot(ctx, tx, actorScope, projectRef, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	if operation.ExpectedVersion == nil || *operation.ExpectedVersion != version ||
		operation.Target.Version == nil || *operation.Target.Version != version ||
		operation.Target.Name != assistantString(before, "name") || !reflect.DeepEqual(operation.Before, before) ||
		assistantString(operation.Parameters, "workflowRef") != operation.Target.Ref ||
		assistantString(operation.Parameters, "projectRef") != projectRef {
		return false, nil
	}
	// After — только проекция заново проверенного command, не caller authority.
	canonical := operation
	canonical.Input = nil
	payload, _, err := assistantUpdateWorkflow(canonical)
	if err != nil {
		return false, err
	}
	after, err := assistantWorkflowAfterSnapshot(operation.Parameters, *payload.Draft)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(operation.After, after), nil
}
