package prompt

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// TestTemplateAnalysisFollowsLexicalScopes exercises real Go rendering with named templates, aliases, branches and item scopes.
// TestTemplateAnalysisFollowsLexicalScopes проверяет настоящий Go-renderer с именованными шаблонами, псевдонимами, ветвями и областями элементов.
func TestTemplateAnalysisFollowsLexicalScopes(t *testing.T) {
	cases := []struct {
		text, output string
		refs         []string
	}{
		{`{{with .project}}{{printf "%s" .name}}{{end}}`, "project-current", []string{"project", "project.name"}},
		{`{{$project := .project}}{{if .task}}{{$project.name}}{{else}}{{.agent.name}}{{end}}`, "project-current", []string{"project", "project.name", "task", "agent.name"}},
		{`{{define "project"}}{{.name}}{{end}}{{template "project" .project}}`, "project-current", []string{"project", "project.name"}},
		{`{{define "root"}}{{with .project}}{{template "name" .}}{{end}}{{end}}{{define "name"}}{{.name}}{{end}}{{template "root" .}}`, "project-current", []string{"project", "project.name"}},
		{`{{range $index, $file := .input.files}}{{printf "%d:%s:%s" $index $file.name $.project.name}}{{end}}`, "0:example.txt:project-current", []string{"input.files", "project.name"}},
		{`{{$name := .project.name}}{{if .task}}{{$name = .agent.name}}{{end}}{{$name}}`, "agent-current", []string{"project.name", "task", "agent.name"}},
		{`{{range .input.files}}{{template "file" .}}{{end}}{{define "file"}}{{.name}}{{end}}`, "example.txt", []string{"input.files"}},
		{`{{$value := "literal"}}{{with $value}}{{.}}{{end}}`, "literal", nil},
		{`{{$root := .}}{{with .project}}{{$root.agent.name}}{{end}}`, "agent-current", []string{"project", "agent.name"}},
		{`{{with (or .project .agent)}}{{.name}}{{end}}`, "project-current", []string{"project", "agent", "project.name", "agent.name"}},
		{`{{with (.project | or .agent)}}{{.name}}{{end}}`, "agent-current", []string{"project", "agent", "project.name", "agent.name"}},
	}
	data := validationTemplateData()
	data["project"].(map[string]any)["name"] = "project-current"
	data["agent"].(map[string]any)["name"] = "agent-current"
	data["task"] = "current-task"
	for _, test := range cases {
		if diagnostics := Validate(test.text, Catalog()); len(diagnostics) != 0 {
			t.Fatalf("valid template rejected: %s %+v", test.text, diagnostics)
		}
		parsed, err := parseTemplate(test.text)
		if err != nil {
			t.Fatal(err)
		}
		result, err := executeTemplate(parsed, data)
		if err != nil || result != test.output {
			t.Fatalf("render mismatch: %q %v expected %q", result, err, test.output)
		}
		refs := templateVariableReferences(parsed)
		for _, name := range test.refs {
			if !slices.Contains(refs, name) {
				t.Fatalf("dependency %s missing from %v", name, refs)
			}
		}
	}
}

// TestTemplateAnalysisRejectsUnknownScopeAndUnboundedWork rejects inaccessible fields, recursive calls and empty-output CPU loops before rendering.
// TestTemplateAnalysisRejectsUnknownScopeAndUnboundedWork отклоняет недопустимые поля, рекурсию и циклы с пустым выводом до исполнения.
func TestTemplateAnalysisRejectsUnknownScopeAndUnboundedWork(t *testing.T) {
	for _, text := range []string{
		`{{with .project}}{{.path}}{{end}}`,
		`{{define "bad"}}{{.unknown}}{{end}}{{template "bad" .project}}`,
		`{{define "cycle"}}{{template "cycle" .}}{{end}}{{template "cycle" .}}`,
		`{{range 1000000000}}{{end}}`,
		`{{range .input.files_count}}{{end}}`,
		`{{range (len .input.files)}}{{end}}`,
		`{{define "root"}}{{printf "%v" .}}{{end}}{{template "root" .}}`,
		`{{$p := .project}}{{define "nested"}}{{$p.name}}{{end}}{{template "nested" .}}`,
		`{{with .project}}{{.agent.name}}{{end}}`,
		`{{define "bad"}}{{if slot "TOOLS"}}hidden{{end}}{{end}}{{template "bad" .}}`,
	} {
		if diagnostics := Validate(text, Catalog()); len(diagnostics) == 0 {
			t.Fatalf("unsafe template accepted: %s", text)
		}
	}
	parsed, err := parseTemplate(`{{range .input.files}}{{range $.input.files}}{{end}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	data := validationTemplateData()
	data["input"].(map[string]any)["files"] = make([]any, 1000)
	if _, err := executeTemplate(parsed, data); err == nil {
		t.Fatal("nested empty-output work exceeded runtime budget")
	}
}

// TestTemplateAnalysisPreservesFreshValuesAndPinnedReplay proves changed branch/item inputs alter the actual materialized prompt without mutating prior snapshots.
// TestTemplateAnalysisPreservesFreshValuesAndPinnedReplay доказывает изменение настоящего prompt при смене условий и элементов без изменения прежних снимков.
func TestTemplateAnalysisPreservesFreshValuesAndPinnedReplay(t *testing.T) {
	text := `{{define "project"}}{{.name}}{{end}}{{if .task}}{{template "project" .project}}{{end}}:{{range .input.files}}{{.name}};{{end}}`
	original := semanticFixture()
	original.StructuredVariables = validationTemplateData()
	original.StructuredVariables["project"].(map[string]any)["name"] = "original-project"
	original.StructuredVariables["task"] = "present"
	first, err := Materialize(text, original)
	if err != nil || !first.Complete {
		t.Fatalf("initial materialization: %v %+v", err, first.Diagnostics)
	}
	current := original
	current.StructuredVariables = validationTemplateData()
	current.StructuredVariables["project"].(map[string]any)["name"] = "changed-project"
	current.StructuredVariables["task"] = "present"
	current.StructuredVariables["input"].(map[string]any)["files"] = []any{map[string]any{"name": "changed-file"}}
	second, err := Materialize(text, current)
	if err != nil || !second.Complete {
		t.Fatalf("fresh materialization: %v %+v", err, second.Diagnostics)
	}
	if first.Digest == second.Digest || !strings.Contains(first.Prompt, "original-project") || !strings.Contains(second.Prompt, "changed-project:changed-file;") {
		t.Fatal("changed values did not reach actual prompt")
	}
	replay, err := Materialize(text, original)
	if err != nil || replay.Prompt != first.Prompt || replay.Digest != first.Digest {
		t.Fatal("pinned materialization changed")
	}
	current.Variables = maps.Clone(original.Variables)
	current.Variables["task"] = ""
	branch, err := Materialize(text, current)
	if err != nil || strings.Contains(branch.Prompt, "changed-project:changed-file") {
		t.Fatal("fresh condition did not affect rendered branch")
	}
}

// TestTemplateAnalysisNestedUnavailableContextWithholdsExecutablePrompt checks named-template aliases cannot bypass unavailable-context diagnostics.
// TestTemplateAnalysisNestedUnavailableContextWithholdsExecutablePrompt проверяет невозможность обхода недоступного контекста через вложенный шаблон и псевдоним.
func TestTemplateAnalysisNestedUnavailableContextWithholdsExecutablePrompt(t *testing.T) {
	snapshot := semanticFixture()
	snapshot.UnavailableVariables = map[string]string{"run.ref": "RUNTIME_CONTEXT_REQUIRED"}
	result, err := Materialize(`{{define "run"}}{{$r := .}}{{if $r.ref}}{{$r.ref}}{{end}}{{end}}{{template "run" .run}}`, snapshot)
	if err != nil || result.Complete || result.Prompt != "" || result.Digest != "" || len(result.Diagnostics) == 0 {
		t.Fatalf("unavailable nested reference accepted: %v %+v", err, result.Diagnostics)
	}
}
