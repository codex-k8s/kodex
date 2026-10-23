package app

import (
	"bytes"
	"os"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestBuildInputManifestsPreservesValidationOrder checks current/history/knowledge projection and
// both existing caller paths: a bad set fails before full provenance validation, and either failure
// precedes workspace writes or artifact callbacks; these fixtures do not launch a runtime agent.
// TestBuildInputManifestsPreservesValidationOrder проверяет проекцию текущих, исторических и
// справочных файлов и оба прежних вызова: ошибка набора предшествует полной проверке происхождения,
// а любая ошибка — записи workspace или callback артефактов; runtime-агент оснасткой не запускается.
func TestBuildInputManifestsPreservesValidationOrder(t *testing.T) {
	input := bindAttachmentCatalog(model.Input{
		AttachmentSetRef: "aset_abcdefgh", AttachmentContext: "RUN_INPUT",
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			runnerInputArtifact("artifact_abcdefgh", "brief.txt", "text/plain", runtimecontract.AttachmentScopeInput, 1, 2, "CONTROL_CENTER"),
			runnerInputArtifact("artifact_ijklmnop", "prior.txt", "text/plain", runtimecontract.AttachmentScopeSession, 1, 1, "INTERACTION_ATTACHMENT"),
			runnerInputArtifact("artifact_qrstuvwx", "policy.md", "text/markdown", runtimecontract.AttachmentScopeKnowledge, 1, 1, "KNOWLEDGE_SOURCE"),
		},
	})
	for index, set := range input.AttachmentSets {
		manifest, err := runtimecontract.BuildAttachmentSetManifest(set, input.InputArtifacts)
		if err != nil {
			t.Fatal(err)
		}
		input.AttachmentSets[index].ManifestDigest = manifest.Digest
	}
	sets, workspace, err := buildInputManifests(input)
	if err != nil || len(sets) != 2 || len(workspace.Manifest.Files) != 3 {
		t.Fatalf("runtime catalog projection failed: %v", err)
	}
	expected, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil || !bytes.Equal(workspace.Bytes, expected.Bytes) {
		t.Fatalf("canonical workspace bytes changed: %v", err)
	}
	for _, set := range input.AttachmentSets {
		if sets[set.Ref].Digest != set.ManifestDigest {
			t.Fatal("expected set digest was not preserved")
		}
	}
	for _, mode := range []string{"digest", "metadata", "provenance", "both"} {
		t.Run(mode, func(t *testing.T) {
			bad := input
			bad.AttachmentSets = append([]runtimecontract.RunnerAttachmentSet(nil), input.AttachmentSets...)
			bad.InputArtifacts = append([]runtimecontract.RunnerInputArtifact(nil), input.InputArtifacts...)
			bad.WorkspaceRoot = t.TempDir()
			want := "runtime attachment manifest digest is invalid"
			switch mode {
			case "digest":
				bad.AttachmentSets[0].ManifestDigest = "invalid"
			case "metadata":
				bad.InputArtifacts[0].FileName = "../private"
			case "provenance":
				bad.InputArtifacts[0].Provenance = "UNKNOWN"
				want = "build runtime workspace manifest"
			case "both":
				bad.AttachmentSets[0].ManifestDigest = "invalid"
				bad.InputArtifacts[0].Provenance = "UNKNOWN"
			}
			partial, combined, err := buildInputManifests(bad)
			if err == nil || err.Error() != want || partial != nil || combined.Bytes != nil {
				t.Fatalf("validation ordering or partial result changed: %v", err)
			}
			for _, err := range []error{materializeInputArtifacts(t.Context(), bad, nil), verifyInputArtifacts(bad)} {
				if err == nil || err.Error() != want {
					t.Fatalf("caller validation ordering changed: %v", err)
				}
			}
			entries, err := os.ReadDir(bad.WorkspaceRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid manifest reached workspace writes: %v", err)
			}
		})
	}
}
