package platform

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"text/template"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func TestTemplateVariableCatalogContainsOnlyMaterializedNamespaces(t *testing.T) {
	catalog := templateVariableCatalog()
	names := make([]string, 0, len(catalog))
	for _, item := range catalog {
		names = append(names, item.Name)
	}
	if !slices.IsSorted(names) {
		t.Fatalf("template variable catalog is not cursor-safe: %v", names)
	}
	for _, forbidden := range []string{"runtime.config.ref"} {
		if slices.Contains(names, forbidden) {
			t.Fatalf("unmaterialized variable %q is advertised", forbidden)
		}
	}
	for _, required := range []string{
		"agent.ref", "agent.name", "user.ref", "organization.ref", "gate.files", "input.files", "session.files", "run.files", "workflow.files", "project.files",
		"project.ref", "project.name", "run.ref", "session.ref", "turn.ref", "runtime.environment.ref",
		"runtime.environment.image.reference", "runtime.environment.image.digest", "runtime.environment.tools",
	} {
		if !slices.Contains(names, required) {
			t.Fatalf("materialized variable %q is missing", required)
		}
	}
}

func TestTemplateVariableCatalogExamplesRenderAgainstCanonicalShape(t *testing.T) {
	file := map[string]any{"name": "example.txt", "path": "/workspace/input/example.txt"}
	fileScope := map[string]any{"files": []map[string]any{file}, "files_count": 1,
		"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json", "values": map[string]any{"title": "Example"}}
	variables := map[string]any{
		"agent":        map[string]any{"ref": "agt_example", "name": "Агент"},
		"automation":   map[string]any{"ref": "sch_example", "name": "Automation", "task": "Task", "scheduled_at": "2026-09-07T12:00:00Z", "timezone": "UTC", "revision": "srev_example"},
		"environment":  map[string]any{"ref": "renv_example"},
		"gate":         fileScope,
		"input":        fileScope,
		"integrations": map[string]any{"summary": "GitHub API", "items": []map[string]any{{"ref": "igr_example", "name": "GitHub API", "version": 1, "description": "Read approved repository", "capability": "github.read"}}},
		"step":         map[string]any{"key": "step", "name": "Analysis", "purpose": "Analyze inputs", "expected_result": "Summary"},
		"node":         map[string]any{"ref": "node_example"},
		"organization": map[string]any{"ref": "org_example", "name": "Организация"},
		"project": map[string]any{"ref": "prj_example", "name": "Проект", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace/knowledge", "manifest_path": "/workspace/input/manifest.json"},
		"run": map[string]any{"ref": "run_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"},
		"runtime": map[string]any{"environment": map[string]any{"ref": "renv_example",
			"image": map[string]any{"reference": "registry.example/runtime@sha256:example", "digest": "sha256:example"},
			"tools": []map[string]any{{"name": "GitHub CLI", "description": "GitHub API"}}}},
		"session": map[string]any{"ref": "ses_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace", "manifest_path": "/workspace/input/manifest.json"},
		"target": map[string]any{"ref": "agt_example"},
		"task":   "Задача",
		"tools":  map[string]any{"summary": "GitHub CLI"},
		"turn":   map[string]any{"ref": "trn_example"},
		"user":   map[string]any{"ref": "usr_example", "name": "Пользователь"},
		"workflow": map[string]any{"ref": "wfl_example", "name": "Analysis", "purpose": "Analyze inputs", "stage": map[string]any{"key": "step"},
			"files": []map[string]any{file}, "files_count": 1, "files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"},
	}
	for _, item := range templateVariableCatalog() {
		parsed, err := template.New(item.Name).Option("missingkey=error").Parse(item.Example)
		if err != nil {
			t.Fatalf("parse example for %s: %v", item.Name, err)
		}
		var output bytes.Buffer
		if err := parsed.Execute(&output, variables); err != nil || output.Len() == 0 {
			t.Fatalf("render example for %s: output=%q err=%v", item.Name, output.String(), err)
		}
	}
}

func TestTemplateVariableCursorIsBoundToCatalogFilter(t *testing.T) {
	current := scope{organizationID: "organization", actorID: "actor"}
	filter := query.Filter{ProjectRef: "prj_example", Query: "agent", TemplateContext: &query.TemplateVariableContext{AgentRef: "agt_example"}}
	filter.Page.Token = encodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter, "agent.ref")
	cursor, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter)
	if err != nil || cursor != "agent.ref" {
		t.Fatalf("decode template variable cursor: cursor=%#v err=%v", cursor, err)
	}
	for _, changed := range []struct{ project, query string }{{"prj_other", "agent"}, {"prj_example", "project"}} {
		other := filter
		other.ProjectRef, other.Query = changed.project, changed.query
		if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", other); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("template variable cursor accepted another filter: %#v err=%v", changed, err)
		}
	}
	other := filter
	other.TemplateContext = &query.TemplateVariableContext{AgentRef: "agt_other"}
	if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", other); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatal("changed target cursor accepted")
	}
	current.actorID = "foreign"
	if _, err := decodeCatalogCursor(current, "TEMPLATE_VARIABLE", filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatal("changed actor cursor accepted")
	}
}

func TestTemplateVariableAvailabilityUsesMaterializedValues(t *testing.T) {
	snapshot := entity.PromptMaterializationSnapshot{RunRef: "run_example", SessionRef: "ses_example",
		Variables:           map[string]string{"agent.ref": "agt_example", "workflow.ref": "", "organization.ref": "org_example"},
		StructuredVariables: map[string]any{"input": map[string]any{"files": []any{}, "files_count": float64(0)}}}
	availability := materializedVariableAvailability(snapshot)
	for _, name := range []string{"run.ref", "session.ref", "agent.ref", "input.files", "input.files_count"} {
		if !availability[name] {
			t.Fatalf("materialized variable disabled: %s", name)
		}
	}
	for _, name := range []string{"project.ref", "workflow.ref", "runtime.environment.ref"} {
		if availability[name] {
			t.Fatalf("absent variable enabled: %s", name)
		}
	}
	for _, variable := range templateVariableCatalog() {
		want := variableNotMaterialized
		if availability[variable.Name] {
			want = variableAvailable
		}
		if got := variableAvailabilityReason(variable, availability, true); got != want {
			t.Fatalf("reason %s: %s", variable.Name, got)
		}
	}
}
