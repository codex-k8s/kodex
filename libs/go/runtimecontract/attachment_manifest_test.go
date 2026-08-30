package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func manifestArtifact(ref, name, mediaType, scope string, position int64, source string) RunnerInputArtifact {
	return RunnerInputArtifact{
		Ref: ref, FileName: name, MediaType: mediaType,
		Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 42,
		Revision: 2, Version: 3, Scope: scope, Position: position, Source: source,
	}
}

func TestBuildAttachmentManifestIsCanonicalAndCoversAllScopes(t *testing.T) {
	artifacts := []RunnerInputArtifact{
		manifestArtifact("artifact_qrstuvwx", "Правила 2026.md", "text/markdown", AttachmentScopeKnowledge, 1, "KNOWLEDGE_SOURCE"),
		manifestArtifact("artifact_ijklmnop", "prior.txt", "text/plain", AttachmentScopeSession, 1, "INTERACTION_ATTACHMENT"),
		manifestArtifact("artifact_abcdefgh", "customer brief.txt", "text/plain", AttachmentScopeInput, 1, "CONTROL_CENTER"),
	}
	canonical, err := BuildAttachmentManifest("aset_abcdefgh", "SESSION_TURN", artifacts)
	if err != nil {
		t.Fatalf("BuildAttachmentManifest() error = %v", err)
	}
	reordered, err := BuildAttachmentManifest("aset_abcdefgh", "SESSION_TURN", []RunnerInputArtifact{artifacts[2], artifacts[0], artifacts[1]})
	if err != nil {
		t.Fatalf("BuildAttachmentManifest(reordered) error = %v", err)
	}
	if !bytes.Equal(canonical.Bytes, reordered.Bytes) || canonical.Digest != reordered.Digest {
		t.Fatal("canonical manifest depends on caller ordering")
	}
	if canonical.Manifest.Schema != AttachmentManifestSchema || canonical.Manifest.Version != AttachmentManifestVersion ||
		canonical.Manifest.AttachmentSetRef != "aset_abcdefgh" || canonical.Manifest.AttachmentContext != "SESSION_TURN" ||
		canonical.Manifest.Digest != canonical.Digest || len(canonical.Manifest.Files) != 3 {
		t.Fatalf("unexpected manifest header: %#v", canonical.Manifest)
	}
	want := []struct {
		scope, purpose, path string
	}{
		{AttachmentScopeInput, "SESSION_TURN", "/workspace/input/aset_abcdefgh/files/0001-customer_brief.txt"},
		{AttachmentScopeSession, "SESSION_INPUT", "/workspace/session/0001-prior.txt"},
		{AttachmentScopeKnowledge, "PROJECT_KNOWLEDGE", "/workspace/knowledge/0001-________2026.md"},
	}
	for index, expected := range want {
		file := canonical.Manifest.Files[index]
		if file.Scope != expected.scope || file.Purpose != expected.purpose || file.Path != expected.path ||
			file.ArtifactRef == "" || file.Revision != 2 || file.Version != 3 || file.FileName == "" ||
			file.MediaType == "" || file.SizeBytes != 42 || file.SHA256 == "" || file.Position != 1 || file.Source == "" {
			t.Fatalf("file[%d] = %#v", index, file)
		}
	}
	payload := attachmentManifestPayload{
		Schema: canonical.Manifest.Schema, Version: canonical.Manifest.Version,
		AttachmentSetRef: canonical.Manifest.AttachmentSetRef, AttachmentContext: canonical.Manifest.AttachmentContext,
		Files: canonical.Manifest.Files,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v", err)
	}
	digest := sha256.Sum256(rawPayload)
	if canonical.Digest != hex.EncodeToString(digest[:]) {
		t.Fatalf("digest = %s", canonical.Digest)
	}
	rawManifest, err := json.Marshal(canonical.Manifest)
	if err != nil || !bytes.Equal(canonical.Bytes, rawManifest) {
		t.Fatalf("manifest bytes are not canonical: err=%v", err)
	}
}

func TestBuildAttachmentManifestRejectsTraversalAndDuplicates(t *testing.T) {
	valid := manifestArtifact("artifact_abcdefgh", "brief.txt", "text/plain", AttachmentScopeInput, 1, "CONTROL_CENTER")
	cases := map[string][]RunnerInputArtifact{
		"traversal":       {func() RunnerInputArtifact { item := valid; item.FileName = "../secret"; return item }()},
		"duplicate ref":   {valid, valid},
		"duplicate place": {valid, manifestArtifact("artifact_ijklmnop", "other.txt", "text/plain", AttachmentScopeInput, 1, "CONTROL_CENTER")},
	}
	for name, artifacts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildAttachmentManifest("aset_abcdefgh", "RUN_INPUT", artifacts); err == nil {
				t.Fatal("invalid artifact catalog was accepted")
			}
		})
	}
}

func TestBuildAttachmentManifestWithoutDirectSetSupportsSessionAndKnowledge(t *testing.T) {
	manifest, err := BuildAttachmentManifest("", "", []RunnerInputArtifact{
		manifestArtifact("artifact_abcdefgh", "prior.txt", "text/plain", AttachmentScopeSession, 1, "INTERACTION_ATTACHMENT"),
		manifestArtifact("artifact_ijklmnop", "policy.md", "text/markdown", AttachmentScopeKnowledge, 1, "KNOWLEDGE_SOURCE"),
	})
	if err != nil {
		t.Fatalf("BuildAttachmentManifest() error = %v", err)
	}
	if manifest.Manifest.AttachmentSetRef != "" || manifest.Manifest.AttachmentContext != "" || len(manifest.Manifest.Files) != 2 {
		t.Fatalf("manifest = %#v", manifest.Manifest)
	}
}
