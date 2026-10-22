package runtimecontract

import (
	"bytes"
	"reflect"
	"testing"
)

// TestBuildAttachmentSetManifestPreservesCanonicalBytes verifies exact selection and the original
// canonical bytes for current and historical sets with mixed, reordered and same-ref foreign files;
// neither successful projection nor a mutation of its result may alter the caller's catalog.
// TestBuildAttachmentSetManifestPreservesCanonicalBytes проверяет точный выбор и исходные
// канонические байты текущего и исторического наборов при смешанных файлах, ином порядке и чужом
// файле с той же ссылкой; проекция и изменение её результата не должны менять исходный каталог.
func TestBuildAttachmentSetManifestPreservesCanonicalBytes(t *testing.T) {
	for _, scope := range []string{AttachmentScopeInput, AttachmentScopeSession} {
		t.Run(scope, func(t *testing.T) {
			set := RunnerAttachmentSet{Ref: "aset_abcdefgh", Purpose: "SESSION_TURN", Scope: scope}
			first := manifestArtifact("artifact_abcdefgh", "first.txt", "text/plain", scope, 2, "CONTROL_CENTER")
			second := manifestArtifact("artifact_ijklmnop", "prior.txt", "text/plain", scope, 1, "INTERACTION_ATTACHMENT")
			for _, item := range []*RunnerInputArtifact{&first, &second} {
				item.AttachmentSetRef, item.AttachmentPurpose = set.Ref, set.Purpose
				item.Provenance = "CURRENT_TURN"
				if scope == AttachmentScopeSession {
					item.Provenance = "SESSION_HISTORY"
				}
			}
			foreign := first
			foreign.AttachmentSetRef = "aset_ijklmnop"
			knowledge := manifestArtifact("artifact_qrstuvwx", "policy.md", "text/markdown", AttachmentScopeKnowledge, 1, "KNOWLEDGE_SOURCE")
			knowledge.AttachmentPurpose, knowledge.Provenance = "PROJECT_KNOWLEDGE", "PROJECT_BINDING"
			catalog := []RunnerInputArtifact{knowledge, first, foreign, second}
			original := append([]RunnerInputArtifact(nil), catalog...)
			want, err := BuildAttachmentManifest(set.Ref, set.Purpose, []RunnerInputArtifact{
				manifestArtifact(first.Ref, first.FileName, first.MediaType, AttachmentScopeInput, first.Position, first.Source),
				manifestArtifact(second.Ref, second.FileName, second.MediaType, AttachmentScopeInput, second.Position, second.Source),
			})
			if err != nil {
				t.Fatal(err)
			}
			actual, err := BuildAttachmentSetManifest(set, catalog)
			if err != nil || !reflect.DeepEqual(actual, want) {
				t.Fatalf("set manifest differs from original canonical representation: %v", err)
			}
			reordered, err := BuildAttachmentSetManifest(set, []RunnerInputArtifact{second, foreign, first, knowledge})
			if err != nil || !bytes.Equal(actual.Bytes, reordered.Bytes) || actual.Digest != reordered.Digest {
				t.Fatalf("set manifest depends on catalog order: %v", err)
			}
			actual.Manifest.Files[0].FileName = "changed.txt"
			actual.Bytes[0] = ' '
			if !reflect.DeepEqual(catalog, original) {
				t.Fatal("set projection mutated the runtime catalog")
			}
		})
	}
}

// TestBuildAttachmentSetManifestKeepsValidation checks that selected metadata still passes the
// original canonical validator, missing sets fail and unrelated invalid files remain unselected;
// full-catalog provenance validation belongs to the caller, not this projection helper.
// TestBuildAttachmentSetManifestKeepsValidation проверяет сохранение исходной проверки выбранных
// метаданных, отказ при отсутствии набора и исключение посторонних некорректных файлов;
// проверка происхождения всего каталога остаётся у вызывающего кода, не у этого helper.
func TestBuildAttachmentSetManifestKeepsValidation(t *testing.T) {
	set := RunnerAttachmentSet{Ref: "aset_abcdefgh", Purpose: "RUN_INPUT"}
	valid := manifestArtifact("artifact_abcdefgh", "brief.txt", "text/plain", AttachmentScopeInput, 1, "CONTROL_CENTER")
	valid.AttachmentSetRef, valid.AttachmentPurpose, valid.Provenance = set.Ref, set.Purpose, "CURRENT_TURN"
	for name, mutate := range map[string]func(*RunnerInputArtifact){
		"path":     func(a *RunnerInputArtifact) { a.FileName = "../private" },
		"digest":   func(a *RunnerInputArtifact) { a.Digest = "invalid" },
		"size":     func(a *RunnerInputArtifact) { a.SizeBytes = MaximumInputArtifactBytes + 1 },
		"position": func(a *RunnerInputArtifact) { a.Position = 0 },
		"revision": func(a *RunnerInputArtifact) { a.Revision = 0 },
		"version":  func(a *RunnerInputArtifact) { a.Version = 0 },
		"source":   func(a *RunnerInputArtifact) { a.Source = "UNKNOWN" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			mutate(&bad)
			if _, err := BuildAttachmentSetManifest(set, []RunnerInputArtifact{bad}); err == nil {
				t.Fatal("invalid selected artifact was accepted")
			}
		})
	}
	for _, catalog := range [][]RunnerInputArtifact{nil, {}, {valid, valid}, {{AttachmentSetRef: "aset_ijklmnop"}}} {
		if _, err := BuildAttachmentSetManifest(set, catalog); err == nil {
			t.Fatal("missing or duplicate set files were accepted")
		}
	}
	for _, bad := range []RunnerAttachmentSet{{Ref: "../private", Purpose: set.Purpose}, {Ref: set.Ref, Purpose: "UNKNOWN"}} {
		artifact := valid
		artifact.AttachmentSetRef = bad.Ref
		if _, err := BuildAttachmentSetManifest(bad, []RunnerInputArtifact{artifact}); err == nil {
			t.Fatal("invalid canonical set binding was accepted")
		}
	}
	if _, err := BuildAttachmentSetManifest(set, []RunnerInputArtifact{{AttachmentSetRef: "aset_ijklmnop"}, valid}); err != nil {
		t.Fatalf("unselected metadata changed the per-set projection: %v", err)
	}
	empty, err := BuildAttachmentSetManifest(RunnerAttachmentSet{}, nil)
	baseline, baselineErr := BuildAttachmentManifest("", "", nil)
	if err != nil || baselineErr != nil || !reflect.DeepEqual(empty, baseline) {
		t.Fatal("empty canonical manifest semantics changed")
	}
}
