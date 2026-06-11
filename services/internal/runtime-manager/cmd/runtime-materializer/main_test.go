package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestManifestBundlesReturnsPerServiceYAMLDigest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundleDir := filepath.Join(root, "deploy", "base", "agent-manager")
	if err := os.MkdirAll(bundleDir, 0o750); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "b.yaml"), []byte("kind: Service\nmetadata:\n  name: agent-manager\n"), 0o640); err != nil {
		t.Fatalf("write b.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "a.yaml"), []byte("kind: Deployment\nmetadata:\n  name: agent-manager\n"), 0o640); err != nil {
		t.Fatalf("write a.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "ignored.txt"), []byte("not part of deploy digest\n"), 0o640); err != nil {
		t.Fatalf("write ignored.txt: %v", err)
	}

	digests, err := digestManifestBundles(root, []string{"agent-manager"})
	if err != nil {
		t.Fatalf("digestManifestBundles(): %v", err)
	}
	expected := digestManifestFiles([]manifestFile{
		{Relative: "a.yaml", Raw: []byte("kind: Deployment\nmetadata:\n  name: agent-manager\n")},
		{Relative: "b.yaml", Raw: []byte("kind: Service\nmetadata:\n  name: agent-manager\n")},
	})
	if digests["agent-manager"] != expected {
		t.Fatalf("digest = %q, want %q", digests["agent-manager"], expected)
	}
	if _, ok := digests["ignored"]; ok {
		t.Fatalf("unexpected digest keys: %+v", digests)
	}
}
