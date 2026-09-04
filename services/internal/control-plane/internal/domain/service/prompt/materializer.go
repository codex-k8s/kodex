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
)

var ErrInvalid = errors.New("invalid prompt template")

const (
	TargetAgent               = "AGENT"
	TargetWorkflowStage       = "WORKFLOW_STAGE"
	TargetAutomation          = "AUTOMATION"
	TargetSessionContinuation = "SESSION_CONTINUATION"
)

type Diagnostic struct {
	Severity, Code, Message string
	Line, Column            int32
}

// Snapshot содержит только проверенные server-owned значения одной immutable revision.
type Snapshot struct {
	TargetKind, TargetRef, ProjectRef, RunRef, SessionRef string
	TemplateRef, TemplateDigest                           string
	Variables                                             map[string]string
	StructuredVariables                                   map[string]any
	UserCapabilities, AgentCapabilities                   []string
	WorkflowCapabilities, ConnectionCapabilities          []string
	HumanGateCapabilities                                 []string
	WorkflowStage, Automation, SessionContinuation        string
}

type Materialization struct {
	Prompt, SafePrompt, Digest  string
	TemplateRef, TemplateDigest string
	EffectiveCapabilities       []string
	Diagnostics                 []Diagnostic
}

func Validate(templateText string, allowedVariables map[string]string) []Diagnostic {
	if strings.TrimSpace(templateText) == "" || len(templateText) > 100_000 {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_INVALID", Message: "Prompt template is empty or exceeds the size limit", Line: 1, Column: 1}}
	}
	parsed, err := parseTemplate(templateText)
	if err != nil {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_SYNTAX_INVALID", Message: "Prompt template syntax is invalid", Line: 1, Column: 1}}
	}
	if unknown := firstUnknownTemplateField(parsed.Tree.Root, allowedVariables); unknown != "" {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_VARIABLE_UNKNOWN", Message: "Prompt template contains an unknown variable", Line: 1, Column: 1}}
	}
	if _, err := executeTemplate(parsed, validationTemplateData()); err != nil {
		return []Diagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_EXECUTION_INVALID", Message: "Prompt template cannot be executed with the canonical variable shape", Line: 1, Column: 1}}
	}
	return nil
}

func Materialize(templateText string, snapshot Snapshot) (Materialization, error) {
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
	return Materialization{Prompt: rendered, SafePrompt: safeRendered, Digest: hex.EncodeToString(digest[:]), TemplateRef: snapshot.TemplateRef,
		TemplateDigest: snapshot.TemplateDigest, EffectiveCapabilities: effective, Diagnostics: diagnostics}, nil
}

// Intersection закрыто пересекает authority layers. nil означает неприменимый
// слой, а непустой или явно пустой slice является authority-ограничением.
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

// Union объединяет только server-owned grants разных capability namespaces.
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

func SafePreview(value string) string {
	const limit = 4_096
	value = strings.ReplaceAll(value, "<secret", "<redacted-secret")
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
}

func serviceBlock(name, value string) string {
	if strings.TrimSpace(value) == "" {
		return fmt.Sprintf("<%s used=\"false\">unused</%s>", name, name)
	}
	return fmt.Sprintf("<%s used=\"true\">\n%s\n</%s>", name, strings.TrimSpace(value), name)
}

func safeServiceBlock(name, value string) string {
	if strings.TrimSpace(value) == "" {
		return serviceBlock(name, "")
	}
	return serviceBlock(name, "configured")
}

func listBlock(name string, values []string) string {
	if len(values) == 0 {
		return fmt.Sprintf("<%s used=\"false\">unused</%s>", name, name)
	}
	return fmt.Sprintf("<%s used=\"true\">%s</%s>", name, strings.Join(values, ","), name)
}

func capabilityLayer(values []string) string {
	if values == nil {
		return "<not-applicable>"
	}
	return strings.Join(Union(values), ",")
}

func copyVariables(input map[string]string) map[string]string {
	result := make(map[string]string, len(input)+4)
	for name, value := range input {
		result[name] = value
	}
	return result
}

func Catalog() map[string]string {
	return map[string]string{
		"user.ref": "", "user.name": "", "organization.ref": "", "organization.name": "",
		"project.ref": "", "project.name": "", "agent.ref": "", "agent.name": "",
		"workflow.ref": "", "workflow.stage.key": "", "automation.ref": "", "run.ref": "",
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

func parseTemplate(templateText string) (*template.Template, error) {
	return template.New("prompt").Option("missingkey=error").Parse(templateText)
}

func firstUnknownTemplateField(node parse.Node, allowed map[string]string) string {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
		return ""
	}
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			if unknown := firstUnknownTemplateField(child, allowed); unknown != "" {
				return unknown
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
		return "template"
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			if unknown := firstUnknownTemplateField(command, allowed); unknown != "" {
				return unknown
			}
		}
	case *parse.CommandNode:
		for _, argument := range current.Args {
			if unknown := firstUnknownTemplateField(argument, allowed); unknown != "" {
				return unknown
			}
		}
	case *parse.FieldNode:
		name := strings.Join(current.Ident, ".")
		if !allowedTemplateField(name, allowed) {
			return name
		}
	case *parse.ChainNode:
		if unknown := firstUnknownTemplateField(current.Node, allowed); unknown != "" {
			return unknown
		}
		if name := strings.Join(current.Field, "."); name != "" && !allowedTemplateItemField(name) {
			return name
		}
	case *parse.VariableNode:
		if len(current.Ident) == 1 {
			return current.Ident[0]
		}
		name := strings.Join(current.Ident[1:], ".")
		if !allowedTemplateItemField(name) {
			return name
		}
	case *parse.IdentifierNode:
		if !allowedTemplateFunction(current.Ident) {
			return current.Ident
		}
	case *parse.DotNode:
		return "."
	}
	return ""
}

func allowedTemplateFunction(name string) bool {
	switch name {
	case "and", "or", "not", "eq", "ne", "lt", "le", "gt", "ge", "len", "print", "printf":
		return true
	default:
		return false
	}
}

func firstUnknownBranchField(branch *parse.BranchNode, allowed map[string]string) string {
	for _, node := range []parse.Node{branch.Pipe, branch.List, branch.ElseList} {
		if unknown := firstUnknownTemplateField(node, allowed); unknown != "" {
			return unknown
		}
	}
	return ""
}

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

func allowedTemplateItemField(name string) bool {
	switch name {
	case "artifact_ref", "revision_ref", "name", "media_type", "size", "sha256", "path", "source", "version", "purpose", "description":
		return true
	default:
		return false
	}
}

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
		"input":        map[string]any{},
		"workflow":     map[string]any{"ref": "", "stage": map[string]any{"key": ""}},
		"automation":   map[string]any{"ref": ""},
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

func executeTemplate(parsed *template.Template, data map[string]any) (string, error) {
	var output boundedPromptBuffer
	if err := parsed.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}

type boundedPromptBuffer struct {
	bytes.Buffer
}

func (buffer *boundedPromptBuffer) Write(value []byte) (int, error) {
	if buffer.Len()+len(value) > 256<<10 {
		return 0, errors.New("materialized prompt exceeds the size limit")
	}
	return buffer.Buffer.Write(value)
}

func validTargetKind(value string) bool {
	switch value {
	case TargetAgent, TargetWorkflowStage, TargetAutomation, TargetSessionContinuation:
		return true
	default:
		return false
	}
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
