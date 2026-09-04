package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestPromptStructuredVariablesUseCanonicalWorkspacePathsAndExactRuntime(t *testing.T) {
	artifacts := []map[string]any{
		promptArtifactFixture("artifact_abcdefgh", "input.txt", runtimecontract.AttachmentScopeInput, "aset_abcdefgh", 1),
		promptArtifactFixture("artifact_ijklmnop", "prior.txt", runtimecontract.AttachmentScopeSession, "aset_ijklmnop", 1),
		promptArtifactFixture("artifact_qrstuvwx", "knowledge.md", runtimecontract.AttachmentScopeKnowledge, "", 1),
	}
	variables, err := promptStructuredVariables(artifacts,
		[]runtimecontract.RuntimeEnvironmentTool{{Name: "gh", Command: "gh", Description: "GitHub CLI"}},
		runtimecontract.RuntimeEnvironmentImage{Reference: "registry.example/runtime@sha256:example", Digest: "sha256:example"},
		"renv_example", "aset_abcdefgh", "wfl_example")
	if err != nil {
		t.Fatalf("build prompt variables: %v", err)
	}
	input := variables["input"].(map[string]any)
	if input["files_count"] != 1 || input["files_dir"] != "/workspace/input/aset_abcdefgh/files" ||
		input["manifest_path"] != "/workspace/input/aset_abcdefgh/manifest.json" {
		t.Fatalf("input scope = %#v", input)
	}
	file := input["files"].([]any)[0].(map[string]any)
	if path := file["path"].(string); !strings.HasPrefix(path, "/workspace/input/aset_abcdefgh/files/0001-") {
		t.Fatalf("input path = %q", path)
	}
	if variables["session"].(map[string]any)["files_count"] != 2 ||
		variables["run"].(map[string]any)["files_count"] != 3 ||
		variables["workflow"].(map[string]any)["files_count"] != 3 ||
		variables["project"].(map[string]any)["files_count"] != 1 {
		t.Fatalf("file scopes = %#v", variables)
	}
	runtimeEnvironment := variables["runtime"].(map[string]any)["environment"].(map[string]any)
	if runtimeEnvironment["ref"] != "renv_example" || len(runtimeEnvironment["tools"].([]any)) != 1 {
		t.Fatalf("runtime environment = %#v", runtimeEnvironment)
	}
}

func TestPromptStructuredVariablesRejectInvalidArtifact(t *testing.T) {
	artifact := promptArtifactFixture("artifact_abcdefgh", "../secret", runtimecontract.AttachmentScopeInput, "aset_abcdefgh", 1)
	if _, err := promptStructuredVariables([]map[string]any{artifact}, nil, runtimecontract.RuntimeEnvironmentImage{}, "renv_example", "aset_abcdefgh", ""); err == nil {
		t.Fatal("invalid artifact path was accepted")
	}
}

func promptArtifactFixture(ref, name, scope, setRef string, position int64) map[string]any {
	return map[string]any{
		"ref": ref, "fileName": name, "mediaType": "text/plain", "sizeBytes": float64(1),
		"digest": strings.Repeat("a", 64), "revision": float64(1), "version": float64(1),
		"source": "CONTROL_CENTER", "scope": scope, "position": float64(position),
		"attachmentSetRef": setRef, "attachmentPurpose": "RUN_INPUT", "provenance": "CURRENT_TURN",
	}
}
