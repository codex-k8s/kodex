package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestPreviewFileScopesMatchRuntimeAndPreserveContext(t *testing.T) {
	items := []map[string]any{
		promptArtifactFixture("artifact_abcdefgh", "input.txt", runtimecontract.AttachmentScopeInput, "aset_abcdefgh", 1),
		promptArtifactFixture("artifact_ijklmnop", "history.txt", runtimecontract.AttachmentScopeSession, "aset_ijklmnop", 1),
		promptArtifactFixture("artifact_qrstuvwx", "knowledge.txt", runtimecontract.AttachmentScopeKnowledge, "", 1),
	}
	expected, err := promptStructuredVariables(items, nil, runtimecontract.RuntimeEnvironmentImage{}, "env_version", "aset_abcdefgh", "workflow_example")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := entity.PromptMaterializationSnapshot{AgentCapabilities: []string{runtimecontract.ArtifactCapability}, ContextPin: entity.PromptContextPin{EnvironmentVersionRef: "env_version", AttachmentSetRef: "aset_abcdefgh", WorkflowRef: "workflow_example"},
		StructuredVariables: map[string]any{"workflow": map[string]any{"ref": "workflow_example"}, "input": map[string]any{"values": map[string]any{"ticket": "123"}}, "runtime": map[string]any{"retained": true}}}
	for _, raw := range items {
		item, _, err := promptArtifactDescriptor(raw)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Artifacts = append(snapshot.Artifacts, item)
	}
	if err := refreshPromptFileScopes(&snapshot); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"input", "run", "session", "workflow", "gate", "project"} {
		for key, value := range expected[scope].(map[string]any) {
			if !reflect.DeepEqual(value, snapshot.StructuredVariables[scope].(map[string]any)[key]) {
				t.Fatalf("preview/runtime differ at %s.%s", scope, key)
			}
		}
	}
	if snapshot.StructuredVariables["workflow"].(map[string]any)["ref"] != "workflow_example" || snapshot.StructuredVariables["input"].(map[string]any)["values"] == nil || snapshot.StructuredVariables["runtime"].(map[string]any)["retained"] != true {
		t.Fatal("file scope replaced owner context")
	}
	snapshot.Artifacts[0].Revision = 9007199254740992
	if refreshPromptFileScopes(&snapshot) == nil {
		t.Fatal("imprecise revision accepted")
	}
	snapshot.AgentCapabilities = nil
	if err := refreshPromptFileScopes(&snapshot); err != nil || len(snapshot.Artifacts) != 0 {
		t.Fatal("disabled Agent files opt-in exposed descriptors")
	}
	for _, scope := range []string{"input", "run", "session", "workflow", "gate", "project"} {
		for _, field := range []string{"files", "files_count", "files_dir", "manifest_path"} {
			if snapshot.UnavailableVariables[scope+"."+field] != "CAPABILITY_REQUIRED" {
				t.Fatalf("disabled file family lost reason: %s.%s", scope, field)
			}
		}
	}
}
