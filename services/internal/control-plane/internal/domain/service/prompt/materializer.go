// Package prompt материализует version-pinned prompt одинаково для preview и RuntimeRevision.
package prompt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ErrInvalid marks invalid templates, incomplete pinned snapshots or rejected materialization
// without exposing raw template execution diagnostics.
// ErrInvalid обозначает неверные шаблоны, неполные закреплённые снимки либо отклонённую
// материализацию без раскрытия исходной диагностики исполнения шаблона.
var ErrInvalid = errors.New("invalid prompt template")

const (
	// TargetAgent identifies direct agent prompt materialization in the closed target-kind registry.
	// TargetAgent обозначает прямую материализацию prompt агента в закрытом реестре видов цели.
	TargetAgent = "AGENT"
	// TargetWorkflowStage identifies workflow-stage prompts with workflow, stage and expected-result
	// semantic slots.
	// TargetWorkflowStage обозначает prompt стадии workflow с семантическими слотами workflow, стадии
	// и ожидаемого результата.
	TargetWorkflowStage = "WORKFLOW_STAGE"
	// TargetAutomation identifies automation prompt materialization without granting the template
	// scheduler or execution authority.
	// TargetAutomation обозначает материализацию prompt автоматизации без предоставления шаблону
	// полномочий планировщика либо выполнения.
	TargetAutomation = "AUTOMATION"
	// TargetSessionContinuation identifies continuation prompts that may include the pinned
	// runtime-change slot and validated runtime diff.
	// TargetSessionContinuation обозначает prompt продолжения с возможными закреплённым слотом
	// runtime-изменений и проверенным runtime diff.
	TargetSessionContinuation = "SESSION_CONTINUATION"
)

// Diagnostic carries a safe code/message and optional variable plus one-based source position; it
// does not contain the raw execution error or template body.
// Diagnostic содержит безопасные код и сообщение, необязательную переменную и позицию от единицы
// без исходной ошибки исполнения либо тела шаблона.
type Diagnostic struct {
	VariableName            string
	Severity, Code, Message string
	Line, Column            int32
}

// Snapshot contains one immutable owner revision with verified context, templates, values and
// applicable capability layers; callers must preserve its referenced maps and slices.
// Snapshot содержит одну immutable-ревизию владельца с проверенными контекстом, шаблонами,
// значениями и применимыми слоями capabilities; вызывающий сохраняет неизменность вложенных maps и
// slices.
type Snapshot struct {
	AssistantCore                                         *entity.PromptAssistantCore `json:",omitempty"`
	ExtraTemplates                                        []entity.PromptUserTemplate `json:",omitempty"`
	ContextPin                                            entity.PromptContextPin
	UnavailableVariables                                  map[string]string
	StagePurposeTemplate, StageExpectedResultTemplate     string
	ServiceTemplateRevision, Locale                       string
	SemanticValues                                        map[SemanticSlot]string
	TargetKind, TargetRef, ProjectRef, RunRef, SessionRef string
	TemplateRef, TemplateDigest                           string
	Variables                                             map[string]string
	StructuredVariables                                   map[string]any
	UserCapabilities, AgentCapabilities                   []string
	WorkflowCapabilities, ConnectionCapabilities          []string
	HumanGateCapabilities                                 []string
	WorkflowStage, Automation, SessionContinuation        string
}

// Materialization holds full and redacted prompt output, provenance, pinned digests, effective
// capabilities and diagnostics; incomplete semantic results withhold executable text and digest.
// Materialization хранит полный и скрытый вывод prompt, происхождение, закреплённые дайджесты,
// effective capabilities и диагностику; неполные семантические результаты не выдают исполняемый
// текст и дайджест.
type Materialization struct {
	Complete                                                                       bool
	ContextPin                                                                     entity.PromptContextPin
	RuntimeDiff                                                                    *RuntimeDiff
	ServiceTemplateRevision, ServiceTemplateDigest, VariableSnapshotDigest, Locale string
	Slots                                                                          []SlotProvenance
	Sections                                                                       []Section
	FullSections                                                                   []Section
	Prompt, SafePrompt, Digest                                                     string
	TemplateRef, TemplateDigest                                                    string
	EffectiveCapabilities                                                          []string
	Diagnostics                                                                    []Diagnostic
}

// FromSnapshot maps the immutable domain snapshot into the renderer shape without changing its
// provenance or deep-copying shared templates, maps and capability layers.
// FromSnapshot переносит immutable-снимок домена в форму renderer без изменения происхождения и
// глубокого копирования общих шаблонов, maps и слоёв capabilities.
func FromSnapshot(snapshot entity.PromptMaterializationSnapshot) Snapshot {
	values := make(map[SemanticSlot]string, len(snapshot.SemanticValues))
	for name, value := range snapshot.SemanticValues {
		values[SemanticSlot(name)] = value
	}
	return Snapshot{ServiceTemplateRevision: snapshot.ServiceTemplateRevision, Locale: snapshot.Locale, SemanticValues: values,
		AssistantCore:        snapshot.AssistantCore,
		ExtraTemplates:       snapshot.ExtraTemplates,
		ContextPin:           snapshot.ContextPin,
		UnavailableVariables: snapshot.UnavailableVariables,
		StagePurposeTemplate: snapshot.StagePurposeTemplate, StageExpectedResultTemplate: snapshot.StageExpectedResultTemplate,
		TargetKind: snapshot.TargetKind, TargetRef: snapshot.TargetRef, ProjectRef: snapshot.ProjectRef, RunRef: snapshot.RunRef,
		SessionRef: snapshot.SessionRef, TemplateRef: snapshot.TemplateRef, TemplateDigest: snapshot.TemplateDigest,
		Variables: snapshot.Variables, StructuredVariables: snapshot.StructuredVariables, UserCapabilities: snapshot.UserCapabilities,
		AgentCapabilities: snapshot.AgentCapabilities, WorkflowCapabilities: snapshot.WorkflowCapabilities,
		ConnectionCapabilities: snapshot.ConnectionCapabilities, HumanGateCapabilities: snapshot.HumanGateCapabilities,
		WorkflowStage: snapshot.WorkflowStage, Automation: snapshot.Automation, SessionContinuation: snapshot.SessionContinuation}
}

// Validate checks the nonempty 100000-byte template, syntax, closed variable/function and slot
// rules and bounded execution against canonical fixture data, returning safe positional
// diagnostics.
// Validate проверяет непустой шаблон до 100000 байт, синтаксис, закрытые правила переменных,
// функций и слотов и ограниченное исполнение на канонических данных с безопасной позиционной
// диагностикой.
func Validate(templateText string, allowedVariables map[string]string) []Diagnostic {
	if strings.TrimSpace(templateText) == "" || len(templateText) > 100_000 {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_INVALID", Message: "Prompt template is empty or exceeds the size limit", Line: 1, Column: 1}}
	}
	parsed, err := parseTemplate(templateText)
	if err != nil {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_SYNTAX_INVALID", Message: "Prompt template syntax is invalid", Line: 1, Column: 1}}
	}
	if unknown, position := firstUnknownTemplateField(parsed.Tree.Root, allowedVariables); unknown != "" {
		line, column := templateLineColumn(templateText, position)
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_VARIABLE_UNKNOWN", Message: "Prompt template contains an unknown variable", Line: line, Column: column, VariableName: unknown}}
	}
	if _, valid := templateSlots(parsed.Tree.Root); !valid {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_SLOT_INVALID", Message: "Prompt slots require a standalone literal insertion", Line: 1, Column: 1}}
	}
	if _, err := executeTemplate(parsed, validationTemplateData()); err != nil {
		variable, position := executionDiagnosticField(parsed.Tree.Root, err)
		line, column := templateLineColumn(templateText, position)
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_EXECUTION_INVALID", Message: "Prompt template cannot be executed with the canonical variable shape", Line: line, Column: column, VariableName: variable}}
	}
	return nil
}

// Materialize selects semantic rendering only from the snapshot service revision, using legacy
// rendering solely when that revision is absent.
// Materialize выбирает семантическое построение только по service revision снимка и использует
// историческое построение исключительно при отсутствии этой ревизии.
func Materialize(templateText string, snapshot Snapshot) (Materialization, error) {
	if snapshot.ServiceTemplateRevision != "" {
		return materializeSemantic(templateText, snapshot)
	}
	return materializeLegacy(templateText, snapshot)
}

// materializeLegacy replays historical snapshots with canonical values, separately redacted
// rendering and mandatory service/capability blocks, pinning output and every authority layer in
// the digest.
// materializeLegacy воспроизводит исторические снимки с каноническими значениями, отдельным
// скрытым построением и обязательными service и capability блоками, закрепляя вывод и каждый слой
// полномочий в дайджесте.
func materializeLegacy(templateText string, snapshot Snapshot) (Materialization, error) {
	variables := copyVariables(snapshot.Variables)
	variables["project.ref"] = snapshot.ProjectRef
	variables["run.ref"] = snapshot.RunRef
	variables["session.ref"] = snapshot.SessionRef
	variables["target.ref"] = snapshot.TargetRef
	diagnostics := Validate(templateText, Catalog())
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "ERROR" {
			return Materialization{Diagnostics: diagnostics}, ErrInvalid
		}
	}
	if !validTargetKind(snapshot.TargetKind) || snapshot.TargetRef == "" || snapshot.TemplateRef == "" || !validDigest(snapshot.TemplateDigest) {
		return Materialization{Diagnostics: []Diagnostic{{Severity: "ERROR", Code: "PROMPT_SNAPSHOT_INVALID", Message: "Prompt snapshot is incomplete", Line: 1, Column: 1}}}, ErrInvalid
	}
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	data := canonicalTemplateData(snapshot.StructuredVariables)
	for _, name := range names {
		setNestedTemplateValue(data, name, variables[name])
	}
	parsed, err := parseTemplate(templateText)
	if err != nil {
		return Materialization{Diagnostics: diagnostics}, ErrInvalid
	}
	rendered, err := executeTemplate(parsed, data)
	if err != nil {
		return Materialization{Diagnostics: []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_EXECUTION_INVALID", Message: "Prompt template cannot be executed with this snapshot", Line: 1, Column: 1}}}, ErrInvalid
	}
	safeRendered, err := executeTemplate(parsed, redactTemplateData(data, "").(map[string]any))
	if err != nil || len(rendered) > 256<<10 || len(safeRendered) > 256<<10 {
		return Materialization{Diagnostics: []Diagnostic{{Severity: "ERROR", Code: "PROMPT_MATERIALIZATION_TOO_LARGE", Message: "Materialized prompt exceeds the size limit", Line: 1, Column: 1}}}, ErrInvalid
	}
	// Agent и server-owned connection grants образуют допустимый набор для
	// исполняемого субъекта. User, Workflow и Human Gate сужают его только в тех
	// контекстах, где слой материализован (nil означает неприменимый слой).
	eligible := Union(snapshot.AgentCapabilities, snapshot.ConnectionCapabilities)
	effective := Intersection(snapshot.UserCapabilities, eligible, snapshot.WorkflowCapabilities, snapshot.HumanGateCapabilities)
	rendered += "\n\n" + serviceBlock("workflow-stage", snapshot.WorkflowStage)
	rendered += "\n" + serviceBlock("automation", snapshot.Automation)
	rendered += "\n" + serviceBlock("session-continuation", snapshot.SessionContinuation)
	rendered += "\n" + listBlock("effective-capabilities", effective)
	safeRendered += "\n\n" + safeServiceBlock("workflow-stage", snapshot.WorkflowStage)
	safeRendered += "\n" + safeServiceBlock("automation", snapshot.Automation)
	safeRendered += "\n" + safeServiceBlock("session-continuation", snapshot.SessionContinuation)
	safeRendered += "\n" + listBlock("effective-capabilities", effective)
	digestMaterial := []string{
		snapshot.TemplateRef, snapshot.TemplateDigest, snapshot.TargetKind, snapshot.TargetRef,
		snapshot.ProjectRef, snapshot.RunRef, snapshot.SessionRef, rendered,
		capabilityLayer(snapshot.UserCapabilities), capabilityLayer(snapshot.AgentCapabilities),
		capabilityLayer(snapshot.WorkflowCapabilities), capabilityLayer(snapshot.ConnectionCapabilities),
		capabilityLayer(snapshot.HumanGateCapabilities),
	}
	for _, name := range names {
		digestMaterial = append(digestMaterial, name+"="+variables[name])
	}
	structured, _ := json.Marshal(snapshot.StructuredVariables)
	digestMaterial = append(digestMaterial, string(structured))
	digest := sha256.Sum256([]byte(strings.Join(digestMaterial, "\x00")))
	return Materialization{Complete: true, Prompt: rendered, SafePrompt: safeRendered, Digest: hex.EncodeToString(digest[:]), TemplateRef: snapshot.TemplateRef,
		TemplateDigest: snapshot.TemplateDigest, EffectiveCapabilities: effective, Diagnostics: diagnostics}, nil
}

// Intersection returns sorted distinct capabilities present in every applicable layer; nil skips a
// layer, whereas an explicit empty layer denies all capabilities.
// Intersection возвращает отсортированные уникальные capabilities всех применимых слоёв; nil
// пропускает слой, а явно пустой слой запрещает все capabilities.
func Intersection(layers ...[]string) []string {
	var effective map[string]struct{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		current := make(map[string]struct{}, len(layer))
		for _, capability := range layer {
			capability = strings.TrimSpace(capability)
			if capability != "" {
				current[capability] = struct{}{}
			}
		}
		if effective == nil {
			effective = current
			continue
		}
		for capability := range effective {
			if _, ok := current[capability]; !ok {
				delete(effective, capability)
			}
		}
	}
	result := make([]string, 0, len(effective))
	for capability := range effective {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

// Union normalizes and sorts distinct capability names from server-owned grant layers; it combines
// already trusted namespaces rather than deciding eligibility.
// Union нормализует и сортирует уникальные имена capabilities слоёв grants сервера; объединяет уже
// доверенные пространства, не принимая решение о доступности.
func Union(layers ...[]string) []string {
	values := make(map[string]struct{})
	for _, layer := range layers {
		for _, capability := range layer {
			capability = strings.TrimSpace(capability)
			if capability != "" {
				values[capability] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(values))
	for capability := range values {
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

// SafePreview replaces the literal secret-tag prefix and truncates to 4096 bytes plus a marker; it
// neither performs complete secret detection nor preserves rune boundaries.
// SafePreview заменяет буквальный префикс тега секрета и ограничивает вывод 4096 байтами с
// маркером; не выполняет полное обнаружение секретов и не сохраняет границу Unicode-символа.
func SafePreview(value string) string {
	const limit = 4_096
	value = strings.ReplaceAll(value, "<secret", "<redacted-secret")
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
}

// serviceBlock emits a trimmed legacy service block or an explicit unused marker when its supplied
// value is blank.
// serviceBlock выдаёт исторический service-блок с обрезанными краевыми пробелами либо явный маркер
// unused при пустом значении.
func serviceBlock(name, value string) string {
	if strings.TrimSpace(value) == "" {
		return fmt.Sprintf("<%s used=\"false\">unused</%s>", name, name)
	}
	return fmt.Sprintf("<%s used=\"true\">\n%s\n</%s>", name, strings.TrimSpace(value), name)
}

// safeServiceBlock preserves only whether the legacy service block is configured, replacing any
// nonblank value with a fixed marker.
// safeServiceBlock сохраняет только признак настройки исторического service-блока, заменяя
// непустое значение фиксированным маркером.
func safeServiceBlock(name, value string) string {
	if strings.TrimSpace(value) == "" {
		return serviceBlock(name, "")
	}
	return serviceBlock(name, "configured")
}

// listBlock emits a comma-joined legacy list block or an explicit unused marker without validating
// its already prepared values.
// listBlock выдаёт исторический блок списка с соединением через запятую либо явный unused без
// проверки уже подготовленных значений.
func listBlock(name string, values []string) string {
	if len(values) == 0 {
		return fmt.Sprintf("<%s used=\"false\">unused</%s>", name, name)
	}
	return fmt.Sprintf("<%s used=\"true\">%s</%s>", name, strings.Join(values, ","), name)
}

// capabilityLayer encodes nil as not applicable and other layers as a normalized sorted list,
// preserving that distinction in historical materialization digests.
// capabilityLayer кодирует nil как неприменимый слой, а остальные слои как нормализованный
// отсортированный список, сохраняя различие в исторических дайджестах материализации.
func capabilityLayer(values []string) string {
	if values == nil {
		return "<not-applicable>"
	}
	return strings.Join(Union(values), ",")
}

// copyVariables copies the flat string variable map so renderer-added reserved references do not
// mutate the caller map.
// copyVariables копирует плоскую map строковых переменных, чтобы добавление зарезервированных
// ссылок renderer не меняло map вызывающего.
func copyVariables(input map[string]string) map[string]string {
	result := make(map[string]string, len(input)+4)
	for name, value := range input {
		result[name] = value
	}
	return result
}

// Catalog returns a fresh closed map of canonical dotted prompt variables; empty values describe
// names, not availability or authority.
// Catalog возвращает новую закрытую map канонических переменных prompt с точками; пустые значения
// описывают имена, а не доступность либо полномочия.
func Catalog() map[string]string {
	return map[string]string{
		"user.ref": "", "user.name": "", "organization.ref": "", "organization.name": "",
		"project.ref": "", "project.name": "", "agent.ref": "", "agent.name": "",
		"workflow.name": "", "workflow.purpose": "", "step.key": "", "step.name": "", "step.purpose": "", "step.expected_result": "",
		"integrations.items": "", "integrations.summary": "", "input.values": "",
		"workflow.ref": "", "workflow.stage.key": "", "automation.ref": "", "run.ref": "",
		"automation.name": "", "automation.task": "", "automation.scheduled_at": "", "automation.timezone": "", "automation.revision": "",
		"session.ref": "", "turn.ref": "", "task": "", "node.ref": "", "target.ref": "",
		"environment.ref": "", "tools.summary": "", "input.files": "", "input.files_count": "",
		"input.files_dir": "", "input.manifest_path": "", "session.files": "", "session.files_count": "",
		"session.files_dir": "", "session.manifest_path": "", "run.files": "", "run.files_count": "",
		"run.files_dir": "", "run.manifest_path": "", "workflow.files": "", "workflow.files_count": "",
		"workflow.files_dir": "", "workflow.manifest_path": "", "gate.files": "", "gate.files_count": "",
		"gate.files_dir": "", "gate.manifest_path": "", "project.files": "", "project.files_count": "",
		"project.files_dir": "", "project.manifest_path": "", "runtime.environment.ref": "",
		"runtime.environment.image.reference": "", "runtime.environment.image.digest": "",
		"runtime.environment.tools": "",
	}
}

// parseTemplate parses a named Go template with missing-key errors and the validating slot
// function; separate AST checks enforce the allowed language subset.
// parseTemplate разбирает именованный Go-шаблон с ошибками отсутствующих ключей и валидирующей
// функцией slot; допустимое подмножество языка ограничивают отдельные проверки AST.
func parseTemplate(templateText string) (*template.Template, error) {
	return template.New("prompt").Option("missingkey=error").Funcs(template.FuncMap{"slot": validateSlot}).Parse(templateText)
}

// firstUnknownTemplateField walks template branches in source order and returns the first
// disallowed variable, item field, function or nested-template use with its parser position.
// firstUnknownTemplateField обходит ветви шаблона в исходном порядке и возвращает первую
// запрещённую переменную, поле элемента, функцию либо вложенный template с позицией parser.
func firstUnknownTemplateField(node parse.Node, allowed map[string]string) (string, parse.Pos) {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
		return "", 0
	}
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			if unknown, position := firstUnknownTemplateField(child, allowed); unknown != "" {
				return unknown, position
			}
		}
	case *parse.ActionNode:
		return firstUnknownTemplateField(current.Pipe, allowed)
	case *parse.IfNode:
		return firstUnknownBranchField(&current.BranchNode, allowed)
	case *parse.RangeNode:
		return firstUnknownBranchField(&current.BranchNode, allowed)
	case *parse.WithNode:
		return firstUnknownBranchField(&current.BranchNode, allowed)
	case *parse.TemplateNode:
		return "template", current.Position()
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			if unknown, position := firstUnknownTemplateField(command, allowed); unknown != "" {
				return unknown, position
			}
		}
	case *parse.CommandNode:
		if len(current.Args) > 0 {
			if function, ok := current.Args[0].(*parse.IdentifierNode); ok && function.Ident == "slot" {
				if len(current.Args) != 2 {
					return "slot", current.Position()
				}
				name, ok := current.Args[1].(*parse.StringNode)
				if !ok {
					return "slot", current.Position()
				}
				if _, err := validateSlot(name.Text); err != nil {
					return "slot", current.Position()
				}
			}
		}
		for _, argument := range current.Args {
			if unknown, position := firstUnknownTemplateField(argument, allowed); unknown != "" {
				return unknown, position
			}
		}
	case *parse.FieldNode:
		name := strings.Join(current.Ident, ".")
		if !allowedTemplateField(name, allowed) {
			return name, current.Position()
		}
	case *parse.ChainNode:
		if unknown, position := firstUnknownTemplateField(current.Node, allowed); unknown != "" {
			return unknown, position
		}
		if name := strings.Join(current.Field, "."); name != "" && !allowedTemplateItemField(name) {
			return name, current.Position()
		}
	case *parse.VariableNode:
		if len(current.Ident) == 1 {
			return current.Ident[0], current.Position()
		}
		name := strings.Join(current.Ident[1:], ".")
		if !allowedTemplateItemField(name) {
			return name, current.Position()
		}
	case *parse.IdentifierNode:
		if !allowedTemplateFunction(current.Ident) {
			return current.Ident, current.Position()
		}
	case *parse.DotNode:
		return ".", current.Position()
	}
	return "", 0
}

// templateLineColumn converts a one-based parser byte position into one-based line and Unicode
// column, falling back to the first position for invalid offsets.
// templateLineColumn преобразует байтовую позицию parser от единицы в строку и Unicode-столбец от
// единицы с возвратом первой позиции при неверном смещении.
func templateLineColumn(templateText string, position parse.Pos) (int32, int32) {
	if position < 1 || int(position) > len(templateText)+1 {
		return 1, 1
	}
	prefix := templateText[:int(position)-1]
	line := strings.Count(prefix, "\n") + 1
	lineStart := strings.LastIndex(prefix, "\n") + 1
	column := utf8.RuneCountInString(prefix[lineStart:]) + 1
	return int32(line), int32(column)
}

// executionDiagnosticField finds the first field AST node whose expression appears in the
// execution error, returning only its name and position rather than exposing error text.
// executionDiagnosticField находит первый узел поля AST с выражением из ошибки исполнения и
// возвращает только имя и позицию без раскрытия текста ошибки.
func executionDiagnosticField(node parse.Node, executionErr error) (string, parse.Pos) {
	if node == nil || executionErr == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
		return "", 0
	}
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			if name, position := executionDiagnosticField(child, executionErr); name != "" {
				return name, position
			}
		}
	case *parse.ActionNode:
		return executionDiagnosticField(current.Pipe, executionErr)
	case *parse.IfNode:
		return executionDiagnosticBranchField(&current.BranchNode, executionErr)
	case *parse.RangeNode:
		return executionDiagnosticBranchField(&current.BranchNode, executionErr)
	case *parse.WithNode:
		return executionDiagnosticBranchField(&current.BranchNode, executionErr)
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			if name, position := executionDiagnosticField(command, executionErr); name != "" {
				return name, position
			}
		}
	case *parse.CommandNode:
		for _, argument := range current.Args {
			if name, position := executionDiagnosticField(argument, executionErr); name != "" {
				return name, position
			}
		}
	case *parse.FieldNode:
		name := strings.Join(current.Ident, ".")
		if strings.Contains(executionErr.Error(), "<."+name+">") {
			return name, current.Position()
		}
	case *parse.ChainNode:
		return executionDiagnosticField(current.Node, executionErr)
	}
	return "", 0
}

// executionDiagnosticBranchField searches a branch condition, main body and else body in order for
// a field associated with the execution error.
// executionDiagnosticBranchField по порядку ищет связанное с ошибкой исполнения поле в условии
// ветви, основном теле и теле else.
func executionDiagnosticBranchField(branch *parse.BranchNode, executionErr error) (string, parse.Pos) {
	for _, node := range []parse.Node{branch.Pipe, branch.List, branch.ElseList} {
		if name, position := executionDiagnosticField(node, executionErr); name != "" {
			return name, position
		}
	}
	return "", 0
}

// allowedTemplateFunction accepts only the closed logic, comparison, length, print/printf and slot
// function subset used by the prompt language.
// allowedTemplateFunction принимает только закрытое подмножество логики, сравнений, длины,
// print/printf и slot языка prompt.
func allowedTemplateFunction(name string) bool {
	switch name {
	case "and", "or", "not", "eq", "ne", "lt", "le", "gt", "ge", "len", "print", "printf", "slot":
		return true
	default:
		return false
	}
}

// firstUnknownBranchField checks the condition, main body and else body in order for the first
// disallowed template field or operation.
// firstUnknownBranchField по порядку проверяет условие, основное тело и else на первое запрещённое
// поле либо операцию шаблона.
func firstUnknownBranchField(branch *parse.BranchNode, allowed map[string]string) (string, parse.Pos) {
	for _, node := range []parse.Node{branch.Pipe, branch.List, branch.ElseList} {
		if unknown, position := firstUnknownTemplateField(node, allowed); unknown != "" {
			return unknown, position
		}
	}
	return "", 0
}

// allowedTemplateField accepts catalog names, their namespace prefixes or closed descriptor item
// fields; it does not prove that a value is available in the current snapshot.
// allowedTemplateField принимает имена каталога, префиксы их пространств либо закрытые поля
// элементов дескриптора без подтверждения доступности значения в текущем снимке.
func allowedTemplateField(name string, allowed map[string]string) bool {
	if _, ok := allowed[name]; ok {
		return true
	}
	for candidate := range allowed {
		if strings.HasPrefix(candidate, name+".") {
			return true
		}
	}
	return allowedTemplateItemField(name)
}

// allowedTemplateItemField accepts only the fixed file/tool descriptor field names usable in
// template iteration.
// allowedTemplateItemField принимает только фиксированные имена полей дескрипторов файлов и
// инструментов, доступные в итерации шаблона.
func allowedTemplateItemField(name string) bool {
	switch name {
	case "artifact_ref", "revision_ref", "name", "media_type", "size", "sha256", "path", "source", "version", "purpose", "description", "ref", "capability":
		return true
	default:
		return false
	}
}

// canonicalTemplateData builds fresh nested namespace defaults and file-context paths, then
// recursively overlays owner values without authorizing their contents.
// canonicalTemplateData создаёт новые вложенные умолчания пространств и пути файлового контекста,
// затем рекурсивно накладывает значения владельца без авторизации содержимого.
func canonicalTemplateData(overrides map[string]any) map[string]any {
	fileScope := func(files []any, directory string) map[string]any {
		return map[string]any{
			"files": files, "files_count": len(files), "files_dir": directory,
			"manifest_path": "/workspace/input/manifest.json",
		}
	}
	data := map[string]any{
		"user":         map[string]any{"ref": "", "name": ""},
		"organization": map[string]any{"ref": "", "name": ""},
		"project":      map[string]any{"ref": "", "name": ""},
		"agent":        map[string]any{"ref": "", "name": ""},
		"input":        map[string]any{"values": map[string]any{}},
		"workflow":     map[string]any{"ref": "", "name": "", "purpose": "", "stage": map[string]any{"key": ""}},
		"step":         map[string]any{"key": "", "name": "", "purpose": "", "expected_result": ""},
		"integrations": map[string]any{"items": []any{}, "summary": ""},
		"automation":   map[string]any{"ref": "", "name": "", "task": "", "scheduled_at": "", "timezone": "", "revision": ""},
		"run":          map[string]any{"ref": ""},
		"session":      map[string]any{"ref": ""},
		"gate":         map[string]any{},
		"turn":         map[string]any{"ref": ""},
		"node":         map[string]any{"ref": ""},
		"target":       map[string]any{"ref": ""},
		"environment":  map[string]any{"ref": ""},
		"tools":        map[string]any{"summary": ""},
		"runtime": map[string]any{"environment": map[string]any{
			"ref": "", "image": map[string]any{"reference": "", "digest": ""}, "tools": []any{},
		}},
		"task": "",
	}
	for name, scope := range map[string]map[string]any{
		"input": fileScope(nil, "/workspace/input"), "session": fileScope(nil, "/workspace/input"),
		"run": fileScope(nil, "/workspace"), "workflow": fileScope(nil, "/workspace"),
		"gate": fileScope(nil, "/workspace/input"), "project": fileScope(nil, "/workspace/knowledge"),
	} {
		mergeTemplateMap(data[name].(map[string]any), scope)
	}
	mergeTemplateMap(data, overrides)
	return data
}

// validationTemplateData adds representative file and tool descriptors to canonical defaults so
// validation exercises collection item fields and execution types.
// validationTemplateData добавляет характерные дескрипторы файла и инструмента к каноническим
// умолчаниям для проверки полей элементов коллекций и типов исполнения.
func validationTemplateData() map[string]any {
	file := map[string]any{
		"artifact_ref": "art_example", "revision_ref": "arv_example", "name": "example.txt",
		"media_type": "text/plain", "size": int64(1), "sha256": strings.Repeat("a", 64),
		"path": "/workspace/input/set_example/files/0001-example.txt", "source": "INPUT",
		"version": int64(1), "purpose": "PROMPT_INPUT",
	}
	tool := map[string]any{"name": "example", "description": "Example tool"}
	data := canonicalTemplateData(nil)
	for _, name := range []string{"input", "session", "run", "workflow", "gate", "project"} {
		scope := data[name].(map[string]any)
		scope["files"] = []any{file}
		scope["files_count"] = 1
	}
	data["runtime"].(map[string]any)["environment"].(map[string]any)["tools"] = []any{tool}
	return data
}

// mergeTemplateMap recursively merges map values into mutable target maps and directly assigns
// non-map values, including slices, without deep-copying every leaf.
// mergeTemplateMap рекурсивно объединяет map-значения в изменяемые целевые maps и прямо
// присваивает остальные значения, включая slices, без глубокого копирования каждого листа.
func mergeTemplateMap(target map[string]any, source map[string]any) {
	for key, value := range source {
		if nested, ok := value.(map[string]any); ok {
			current, exists := target[key].(map[string]any)
			if !exists {
				current = map[string]any{}
				target[key] = current
			}
			mergeTemplateMap(current, nested)
			continue
		}
		target[key] = value
	}
}

// setNestedTemplateValue sets a dotted string variable, creating or replacing intermediate maps in
// the renderer-owned data tree.
// setNestedTemplateValue назначает строковую переменную с точками, создавая либо заменяя
// промежуточные maps в дереве данных renderer.
func setNestedTemplateValue(data map[string]any, name, value string) {
	parts := strings.Split(name, ".")
	current := data
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

// redactTemplateData rebuilds supported maps/collections with path placeholders and
// numeric/boolean zero values, keeping at most one representative collection item for legacy safe
// rendering.
// redactTemplateData пересоздаёт поддерживаемые maps и коллекции с placeholders пути и нулевыми
// числами и bool, оставляя не более одного характерного элемента для исторического скрытого
// построения.
func redactTemplateData(value any, path string) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, item := range current {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			result[key] = redactTemplateData(item, childPath)
		}
		return result
	case []any:
		if len(current) == 0 {
			return []any{}
		}
		return []any{redactTemplateData(current[0], path+".item")}
	case []map[string]any:
		if len(current) == 0 {
			return []any{}
		}
		return []any{redactTemplateData(current[0], path+".item")}
	case string:
		return "[" + path + "]"
	case bool:
		return false
	case int:
		return 0
	case int8:
		return int8(0)
	case int16:
		return int16(0)
	case int32:
		return int32(0)
	case int64:
		return int64(0)
	case uint:
		return uint(0)
	case uint8:
		return uint8(0)
	case uint16:
		return uint16(0)
	case uint32:
		return uint32(0)
	case uint64:
		return uint64(0)
	case float32:
		return float32(0)
	case float64:
		return float64(0)
	default:
		return current
	}
}

// executeTemplate executes the parsed template into the bounded prompt buffer, discarding partial
// output when template execution or a write fails.
// executeTemplate исполняет разобранный шаблон в ограниченный буфер prompt, отбрасывая частичный
// вывод при ошибке исполнения либо записи.
func executeTemplate(parsed *template.Template, data map[string]any) (string, error) {
	var output boundedPromptBuffer
	if err := parsed.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}

// boundedPromptBuffer wraps a bytes buffer with the renderer output bound enforced before each
// template write.
// boundedPromptBuffer оборачивает байтовый буфер ограничением вывода renderer, проверяемым до
// каждой записи шаблона.
type boundedPromptBuffer struct {
	bytes.Buffer
}

// Write rejects the whole write when it would exceed 256 KiB, leaving the previously buffered
// bytes unchanged on overflow.
// Write отклоняет всю запись при превышении 256 KiB, сохраняя ранее записанные байты без изменений
// при переполнении.
func (buffer *boundedPromptBuffer) Write(value []byte) (int, error) {
	if buffer.Len()+len(value) > 256<<10 {
		return 0, errors.New("materialized prompt exceeds the size limit")
	}
	return buffer.Buffer.Write(value)
}

// validTargetKind accepts only agent, workflow-stage, automation and session-continuation target
// kinds from the local closed registry.
// validTargetKind принимает только виды цели агента, стадии workflow, автоматизации и продолжения
// сессии из локального закрытого реестра.
func validTargetKind(value string) bool {
	switch value {
	case TargetAgent, TargetWorkflowStage, TargetAutomation, TargetSessionContinuation:
		return true
	default:
		return false
	}
}

// validDigest checks exactly 64 decodable hexadecimal characters, accepting either letter case; it
// does not verify content, provenance or owner authority.
// validDigest проверяет ровно 64 декодируемых hex-символа в любом регистре, не подтверждая
// содержимое, происхождение либо полномочия владельца.
func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
