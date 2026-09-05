package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"
)

// ServiceTemplateRevision изменяется при изменении порядка, формы или семантики блоков.
const ServiceTemplateRevision = "prompt-service-v2"

type SemanticSlot string

const (
	SlotWorkflow       SemanticSlot = "WORKFLOW"
	SlotStage          SemanticSlot = "STAGE"
	SlotPurpose        SemanticSlot = "PURPOSE"
	SlotExpectedResult SemanticSlot = "EXPECTED_RESULT"
	SlotInput          SemanticSlot = "INPUT"
	SlotConstraints    SemanticSlot = "CONSTRAINTS"
	SlotCapabilities   SemanticSlot = "EFFECTIVE_CAPABILITIES"
	SlotFiles          SemanticSlot = "FILES"
	SlotTools          SemanticSlot = "TOOLS"
	SlotIntegrations   SemanticSlot = "INTEGRATIONS"
	SlotRuntimeChanges SemanticSlot = "RUNTIME_CHANGES"
)

var semanticOrder = []SemanticSlot{SlotWorkflow, SlotStage, SlotPurpose, SlotExpectedResult, SlotInput,
	SlotConstraints, SlotCapabilities, SlotFiles, SlotTools, SlotIntegrations, SlotRuntimeChanges}

type SlotProvenance struct {
	Slot     SemanticSlot `json:"slot"`
	Source   string       `json:"source"`
	Position int32        `json:"position"`
}

type Section struct {
	Source  string       `json:"source"`
	Slot    SemanticSlot `json:"slot,omitempty"`
	Content string       `json:"content"`
}

type semanticEnvelope struct {
	Revision string    `json:"revision"`
	Locale   string    `json:"locale"`
	Sections []Section `json:"sections"`
}

func validateSlot(name string) (string, error) {
	if !slices.Contains(semanticOrder, SemanticSlot(name)) {
		return "", ErrInvalid
	}
	return "", nil
}

func requiredSlots(kind string) []SemanticSlot {
	result := make([]SemanticSlot, 0, len(semanticOrder))
	for _, slot := range semanticOrder {
		if (slot == SlotWorkflow || slot == SlotStage || slot == SlotExpectedResult) && kind != TargetWorkflowStage {
			continue
		}
		if slot == SlotRuntimeChanges && kind != TargetSessionContinuation {
			continue
		}
		result = append(result, slot)
	}
	return result
}

func materializeSemantic(text string, snapshot Snapshot) (Materialization, error) {
	invalid := func(code, message string) (Materialization, error) {
		return Materialization{Diagnostics: []Diagnostic{{Severity: "ERROR", Code: code, Message: message, Line: 1, Column: 1}}}, ErrInvalid
	}
	if snapshot.ServiceTemplateRevision != ServiceTemplateRevision || !validTargetKind(snapshot.TargetKind) ||
		snapshot.TargetRef == "" || snapshot.TemplateRef == "" || !validDigest(snapshot.TemplateDigest) {
		return invalid("PROMPT_SNAPSHOT_INVALID", "Prompt snapshot is incomplete")
	}
	locale := snapshot.Locale
	if locale == "" {
		locale = "en"
	}
	if locale != "en" && locale != "ru" {
		return invalid("PROMPT_LOCALE_INVALID", "Prompt locale is unsupported")
	}
	if diagnostics := Validate(text, Catalog()); len(diagnostics) != 0 {
		return Materialization{Diagnostics: diagnostics}, ErrInvalid
	}
	// Одинаковое представление до и после durable JSON readback: typed nil map
	// не должна превращаться из пустого object в null при повторном renderer.
	rawStructured, encodeErr := json.Marshal(snapshot.StructuredVariables)
	var structured map[string]any
	if encodeErr != nil || json.Unmarshal(rawStructured, &structured) != nil {
		return invalid("PROMPT_SNAPSHOT_INVALID", "Prompt snapshot is incomplete")
	}
	data := canonicalTemplateData(structured)
	variables := copyVariables(snapshot.Variables)
	variables["project.ref"], variables["run.ref"], variables["session.ref"], variables["target.ref"] = snapshot.ProjectRef, snapshot.RunRef, snapshot.SessionRef, snapshot.TargetRef
	for name, value := range variables {
		setNestedTemplateValue(data, name, value)
	}
	if snapshot.TargetKind == TargetWorkflowStage && (snapshot.StagePurposeTemplate != "" || snapshot.StageExpectedResultTemplate != "") {
		allowed := Catalog()
		delete(allowed, "step.purpose")
		delete(allowed, "step.expected_result")
		for key, text := range map[string]string{"step.purpose": snapshot.StagePurposeTemplate, "step.expected_result": snapshot.StageExpectedResultTemplate} {
			if len(Validate(text, allowed)) != 0 {
				return invalid("PROMPT_STAGE_TEMPLATE_INVALID", "Workflow stage template is invalid")
			}
			parsed, err := parseTemplate(text)
			if err != nil {
				return invalid("PROMPT_STAGE_TEMPLATE_INVALID", "Workflow stage template is invalid")
			}
			slots, _ := templateSlots(parsed.Tree.Root)
			if len(slots) != 0 {
				return invalid("PROMPT_STAGE_TEMPLATE_INVALID", "Workflow stage fields cannot contain semantic slots")
			}
			value, err := executeTemplate(parsed, data)
			if err != nil {
				return invalid("PROMPT_STAGE_TEMPLATE_INVALID", "Workflow stage template is invalid")
			}
			setNestedTemplateValue(data, key, value)
		}
	}
	effective := Intersection(snapshot.UserCapabilities, Union(snapshot.AgentCapabilities, snapshot.ConnectionCapabilities), snapshot.WorkflowCapabilities, snapshot.HumanGateCapabilities)
	required := requiredSlots(snapshot.TargetKind)
	values := semanticValues(snapshot, data, effective)
	if snapshot.StagePurposeTemplate != "" {
		values[SlotPurpose] = data["step"].(map[string]any)["purpose"].(string)
	}
	if snapshot.StageExpectedResultTemplate != "" {
		values[SlotExpectedResult] = data["step"].(map[string]any)["expected_result"].(string)
	}
	for slot := range snapshot.SemanticValues {
		if !slices.Contains(required, slot) || slot == SlotCapabilities {
			return invalid("PROMPT_SLOT_UNAVAILABLE", "Prompt slot is unavailable in this context")
		}
	}
	parsed, err := parseTemplate(text)
	if err != nil {
		return invalid("PROMPT_TEMPLATE_SYNTAX_INVALID", "Prompt template syntax is invalid")
	}
	used := make(map[SemanticSlot]bool)
	provenance := make([]SlotProvenance, 0, len(required))
	var output boundedPromptBuffer
	sections := make([]Section, 0, len(required)+1)
	userOffset := 0
	flushUser := func() {
		if output.Len() > userOffset {
			sections = append(sections, Section{Source: "USER_TEMPLATE", Content: output.String()[userOffset:]})
			userOffset = output.Len()
		}
	}
	parsed.Funcs(template.FuncMap{"slot": func(name string) (string, error) {
		slot := SemanticSlot(name)
		if !slices.Contains(required, slot) {
			return "", ErrInvalid
		}
		if used[slot] {
			return "", nil
		}
		used[slot] = true
		provenance = append(provenance, SlotProvenance{Slot: slot, Source: "USER_TEMPLATE", Position: int32(len(provenance) + 1)})
		// AST допускает slot только как самостоятельное действие: flush точно
		// сохраняет положение, не смешивая platform value с пользовательским текстом.
		flushUser()
		sections = append(sections, Section{Source: "PLATFORM", Slot: slot, Content: values[slot]})
		return "", nil
	}})
	if err := parsed.Execute(&output, data); err != nil {
		return invalid("PROMPT_TEMPLATE_EXECUTION_INVALID", "Prompt template cannot be executed with this snapshot")
	}
	flushUser()
	// Повторное выполнение с замаскированными значениями меняет условия веток.
	// Projection сохраняет фактический состав, а пользовательский текст скрывает
	// целиком до отдельной проверки permission на полный материализованный текст.
	for _, slot := range required {
		if !used[slot] {
			sections = append(sections, Section{Source: "PLATFORM", Slot: slot, Content: values[slot]})
			provenance = append(provenance, SlotProvenance{Slot: slot, Source: "PLATFORM", Position: int32(len(provenance) + 1)})
		}
	}
	safeSections := make([]Section, 0, len(sections))
	for _, section := range sections {
		content := "[USER_TEMPLATE]"
		if section.Source == "PLATFORM" {
			content = "[" + string(section.Slot) + "]"
		}
		safeSections = append(safeSections, Section{Source: section.Source, Slot: section.Slot, Content: content})
	}
	encoded, err := json.Marshal(semanticEnvelope{Revision: ServiceTemplateRevision, Locale: locale, Sections: sections})
	if err != nil || len(encoded) > 256<<10 {
		return invalid("PROMPT_MATERIALIZATION_TOO_LARGE", "Materialized prompt exceeds the size limit")
	}
	safeEncoded, err := json.Marshal(semanticEnvelope{Revision: ServiceTemplateRevision, Locale: locale, Sections: safeSections})
	if err != nil || len(safeEncoded) > 256<<10 {
		return invalid("PROMPT_MATERIALIZATION_TOO_LARGE", "Materialized prompt exceeds the size limit")
	}
	serviceDigest := semanticDigest(struct {
		Revision, Locale, Kind string
		Slots                  []SemanticSlot
	}{ServiceTemplateRevision, locale, snapshot.TargetKind, required})
	variableDigest := semanticDigest(struct {
		Data      map[string]any
		Values    map[SemanticSlot]string
		Effective []string
	}{data, values, effective})
	digest := semanticDigest(struct {
		Snapshot                     Snapshot
		Service, Variables, Rendered string
	}{snapshot, serviceDigest, variableDigest, string(encoded)})
	if serviceDigest == "" || variableDigest == "" || digest == "" {
		return invalid("PROMPT_SNAPSHOT_INVALID", "Prompt snapshot is incomplete")
	}
	result := Materialization{Complete: true, Prompt: string(encoded), SafePrompt: string(safeEncoded), Digest: digest,
		TemplateRef: snapshot.TemplateRef, TemplateDigest: snapshot.TemplateDigest, EffectiveCapabilities: effective,
		ServiceTemplateRevision: ServiceTemplateRevision, ServiceTemplateDigest: serviceDigest, VariableSnapshotDigest: variableDigest,
		Locale: locale, Slots: provenance, Sections: safeSections, FullSections: sections}
	references := templateVariableReferences(parsed.Tree.Root)
	for _, text := range []string{snapshot.StagePurposeTemplate, snapshot.StageExpectedResultTemplate} {
		if text != "" {
			parsed, err := parseTemplate(text)
			if err == nil {
				references = append(references, templateVariableReferences(parsed.Tree.Root)...)
			}
		}
	}
	slices.Sort(references)
	references = slices.Compact(references)
	for _, name := range references {
		for unavailable, reason := range snapshot.UnavailableVariables {
			if name == unavailable || strings.HasPrefix(unavailable, name+".") {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "ERROR", Code: reason, Message: "Prompt variable requires an available runtime context", Line: 1, Column: 1, VariableName: name})
				break
			}
		}
	}
	if len(result.Diagnostics) > 0 {
		result.Complete = false
		result.Prompt = ""
		result.Digest = ""
		result.FullSections = nil
	}
	return result, nil
}

func templateVariableReferences(root parse.Node) []string {
	seen := make(map[string]bool)
	var visit func(parse.Node)
	visit = func(node parse.Node) {
		if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
			return
		}
		switch current := node.(type) {
		case *parse.ListNode:
			for _, child := range current.Nodes {
				visit(child)
			}
		case *parse.ActionNode:
			visit(current.Pipe)
		case *parse.IfNode:
			visit(current.Pipe)
			visit(current.List)
			visit(current.ElseList)
		case *parse.RangeNode:
			visit(current.Pipe)
			visit(current.List)
			visit(current.ElseList)
		case *parse.WithNode:
			visit(current.Pipe)
			visit(current.List)
			visit(current.ElseList)
		case *parse.PipeNode:
			for _, command := range current.Cmds {
				visit(command)
			}
		case *parse.CommandNode:
			for _, argument := range current.Args {
				visit(argument)
			}
		case *parse.FieldNode:
			seen[strings.Join(current.Ident, ".")] = true
		case *parse.ChainNode:
			visit(current.Node)
		}
	}
	visit(root)
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

// templateSlots разрешает только явную вставку. Вызов в condition, assignment
// или pipeline не доказывает выдачу значения пользователю и закрыто отклоняется.
func templateSlots(root parse.Node) ([]SemanticSlot, bool) {
	var slots []SemanticSlot
	var visit func(parse.Node) bool
	visit = func(node parse.Node) bool {
		if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
			return true
		}
		switch current := node.(type) {
		case *parse.ListNode:
			for _, child := range current.Nodes {
				if !visit(child) {
					return false
				}
			}
		case *parse.ActionNode:
			if len(current.Pipe.Decl) == 0 && len(current.Pipe.Cmds) == 1 {
				args := current.Pipe.Cmds[0].Args
				if len(args) == 2 {
					function, ok := args[0].(*parse.IdentifierNode)
					name, literal := args[1].(*parse.StringNode)
					if ok && function.Ident == "slot" && literal {
						if _, err := validateSlot(name.Text); err != nil {
							return false
						}
						slots = append(slots, SemanticSlot(name.Text))
						return true
					}
				}
			}
			return visit(current.Pipe)
		case *parse.IfNode:
			return visit(current.Pipe) && visit(current.List) && visit(current.ElseList)
		case *parse.RangeNode:
			return visit(current.Pipe) && visit(current.List) && visit(current.ElseList)
		case *parse.WithNode:
			return visit(current.Pipe) && visit(current.List) && visit(current.ElseList)
		case *parse.PipeNode:
			for _, command := range current.Cmds {
				if !visit(command) {
					return false
				}
			}
		case *parse.CommandNode:
			for _, argument := range current.Args {
				if !visit(argument) {
					return false
				}
			}
		case *parse.IdentifierNode:
			return current.Ident != "slot"
		}
		return true
	}
	valid := visit(root)
	return slots, valid
}

func semanticDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func semanticValues(snapshot Snapshot, data map[string]any, effective []string) map[SemanticSlot]string {
	encode := func(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }
	workflow, _ := data["workflow"].(map[string]any)
	input, _ := data["input"].(map[string]any)
	runtime, _ := data["runtime"].(map[string]any)
	environment, _ := runtime["environment"].(map[string]any)
	files := make(map[string]any)
	for _, name := range []string{"input", "session", "run", "workflow", "project", "gate"} {
		scope, _ := data[name].(map[string]any)
		files[name] = map[string]any{"files": scope["files"], "files_count": scope["files_count"], "files_dir": scope["files_dir"], "manifest_path": scope["manifest_path"]}
	}
	values := map[SemanticSlot]string{
		SlotWorkflow: encode(map[string]any{"ref": workflow["ref"], "name": workflow["name"], "purpose": workflow["purpose"]}), SlotStage: snapshot.WorkflowStage,
		SlotPurpose: snapshot.Variables["task"], SlotExpectedResult: snapshot.Variables["step.expected_result"],
		SlotInput: encode(input["values"]), SlotConstraints: "Only the effective capabilities and provided resources are available.",
		SlotCapabilities: strings.Join(effective, "\n"), SlotFiles: encode(files),
		SlotTools: encode(environment["tools"]), SlotIntegrations: encode(data["integrations"]), SlotRuntimeChanges: snapshot.SessionContinuation,
	}
	for slot, value := range snapshot.SemanticValues {
		if slot != SlotCapabilities {
			values[slot] = value
		}
	}
	if snapshot.Locale == "ru" {
		values[SlotConstraints] = "Доступны только эффективные возможности и предоставленные ресурсы."
	}
	return values
}
