package platform

import (
	"bytes"
	"slices"
	"testing"
	"text/template"
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
	for _, forbidden := range []string{"agent.name", "project.name", "runtime.config.ref"} {
		if slices.Contains(names, forbidden) {
			t.Fatalf("unmaterialized variable %q is advertised", forbidden)
		}
	}
	for _, required := range []string{
		"agent.ref", "input.files", "session.files", "run.files", "workflow.files", "project.files",
		"project.ref", "run.ref", "session.ref", "turn.ref", "runtime.environment.ref",
		"runtime.environment.image.reference", "runtime.environment.image.digest", "runtime.environment.tools",
	} {
		if !slices.Contains(names, required) {
			t.Fatalf("materialized variable %q is missing", required)
		}
	}
}

func TestTemplateVariableCatalogExamplesRenderAgainstCanonicalShape(t *testing.T) {
	file := map[string]any{"path": "/workspace/input/example.txt"}
	fileScope := map[string]any{"files": []map[string]any{file}, "files_count": 1,
		"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"}
	variables := map[string]any{
		"agent": map[string]any{"ref": "agt_example"},
		"input": fileScope,
		"project": map[string]any{"ref": "prj_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace/knowledge", "manifest_path": "/workspace/input/manifest.json"},
		"run": map[string]any{"ref": "run_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace/input", "manifest_path": "/workspace/input/manifest.json"},
		"runtime": map[string]any{"environment": map[string]any{"ref": "renv_example",
			"image": map[string]any{"reference": "registry.example/runtime@sha256:example", "digest": "sha256:example"},
			"tools": []map[string]any{{"name": "GitHub CLI", "description": "GitHub API"}}}},
		"session": map[string]any{"ref": "ses_example", "files": []map[string]any{file}, "files_count": 1,
			"files_dir": "/workspace", "manifest_path": "/workspace/input/manifest.json"},
		"turn":     map[string]any{"ref": "trn_example"},
		"workflow": fileScope,
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
