package platform

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"text/template"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
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
		"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"}
	variables := map[string]any{
		"agent":        map[string]any{"ref": "agt_example", "name": "Агент"},
		"automation":   map[string]any{"ref": "sch_example"},
		"environment":  map[string]any{"ref": "renv_example"},
		"gate":         fileScope,
		"input":        fileScope,
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
		"workflow": map[string]any{"ref": "wfl_example", "stage": map[string]any{"key": "step"},
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
	token := encodeTemplateVariableCursor("prj_example", "agent", "agent.ref")
	cursor, err := decodeTemplateVariableCursor(token, "prj_example", "agent")
	if err != nil || cursor.Name != "agent.ref" {
		t.Fatalf("decode template variable cursor: cursor=%#v err=%v", cursor, err)
	}
	for _, changed := range []struct{ project, query string }{{"prj_other", "agent"}, {"prj_example", "project"}} {
		if _, err := decodeTemplateVariableCursor(token, changed.project, changed.query); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("template variable cursor accepted another filter: %#v err=%v", changed, err)
		}
	}
}
