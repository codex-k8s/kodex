package prompt

import (
	"maps"
	"reflect"
	"strings"
	"testing"
)

// TestDependencyClassificationPreservesLiveResolution exercises classification and actual rendering from fresh and replayed snapshots.
// TestDependencyClassificationPreservesLiveResolution проверяет классификацию и реальный текст свежих и повторённых снимков.
func TestDependencyClassificationPreservesLiveResolution(t *testing.T) {
	snapshot := semanticFixture()
	snapshot.ContextPin.Digest = strings.Repeat("c", 64)
	snapshot.ContextPin.AgentRef, snapshot.ContextPin.AgentVersion = "agt_fixture", 3
	snapshot.ContextPin.EnvironmentVersionRef, snapshot.ContextPin.EnvironmentDigest = "env_fixture", strings.Repeat("e", 64)
	snapshot.Variables = maps.Clone(snapshot.Variables)
	snapshot.Variables["task"] = "synthetic-live-before-2108"
	source := `{{define "body"}}{{with .agent}}{{.name}}{{end}}{{end}}{{template "body" .}}{{if .task}}{{.task}}{{end}}{{range .input.files}}{{.name}}{{end}}{{.runtime.environment.image.digest}}{{slot "TOOLS"}}`
	first, err := Materialize(source, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Dependency{}
	for _, entry := range first.Dependencies {
		byPath[entry.Path] = entry
		if entry.Sensitivity != "PRIVATE" {
			t.Fatal("private source lost sensitivity")
		}
	}
	for path, stability := range map[string]string{"$literal": "STATIC_LITERAL", "agent.name": "VERSION_PINNED", "task": "TURN_LIVE", "input.files": "TURN_LIVE", "runtime.environment.image.digest": "VERSION_PINNED", "slot.TOOLS": "TURN_LIVE"} {
		if byPath[path].Stability != stability {
			t.Fatalf("dependency %s: %+v", path, byPath[path])
		}
	}
	fresh := snapshot
	fresh.Variables = maps.Clone(snapshot.Variables)
	fresh.Variables["task"] = "synthetic-live-after-2108"
	second, err := Materialize(source, fresh)
	if err != nil || first.Prompt == second.Prompt || !strings.Contains(second.Prompt, "synthetic-live-after-2108") || strings.Contains(second.Prompt, "synthetic-live-before-2108") {
		t.Fatalf("fresh live value did not reach rendered prompt: %v", err)
	}
	replayed, err := Materialize(source, snapshot)
	if err != nil || replayed.Prompt != first.Prompt || !reflect.DeepEqual(replayed.Dependencies, first.Dependencies) {
		t.Fatal("immutable replay changed")
	}
	if !reflect.DeepEqual(first.Dependencies, second.Dependencies) {
		t.Fatal("classification leaked or depended on resolved values")
	}
	snapshot.ContextPin.Digest = ""
	unknown, err := templateDependencies(source, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range unknown {
		if entry.Path != "$literal" && (entry.Stability != "UNKNOWN" || entry.SourceDigest != "") {
			t.Fatal("missing owner pin was declared stable")
		}
	}
}
