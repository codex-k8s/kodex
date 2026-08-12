package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

func TestBootstrapRootMetadataIsCanonical(t *testing.T) {
	for _, name := range []string{"manifest-root", "readback-root"} {
		path := filepath.Join("..", "bootstrap", name, "bootstrap-metadata.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s metadata: %v", name, err)
		}
		var value map[string]json.RawMessage
		if err := internalrpcauth.DecodeCanonicalJSON(raw, &value); err != nil {
			t.Fatalf("%s metadata is not canonical: %v", name, err)
		}
	}
}

func TestReadRegularFileAcceptsExactGroupReadableSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"EC"}`), 0o440); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := readRegularFile(path, 1024, 0o007); err != nil {
		t.Fatalf("group-readable projected secret rejected: %v", err)
	}
}

func TestReadRegularFileRejectsWorldReadablePrivateMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"EC"}`), 0o444); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := readRegularFile(path, 1024, 0o007); err == nil {
		t.Fatal("world-readable private material accepted")
	}
}

func TestReadRegularFileRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "snapshot.jws")
	if err := os.WriteFile(target, []byte("header.payload.signature"), 0o440); err != nil {
		t.Fatalf("write target: %v", err)
	}
	path := filepath.Join(root, "snapshot.jws")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := readRegularFile(path, 1024, 0o004); err == nil {
		t.Fatal("escaping projected-file symlink accepted")
	}
}

func TestValidateHistoryAllowsBoundedCatchUp(t *testing.T) {
	history := []revisionDigest{
		{Revision: 8, DigestSHA256: repeatedDigest("8")},
		{Revision: 9, DigestSHA256: repeatedDigest("9")},
	}
	if err := validateHistory(
		10,
		revisionDigest{Revision: 9, DigestSHA256: repeatedDigest("9")},
		history,
	); err != nil {
		t.Fatalf("valid catch-up history rejected: %v", err)
	}
}

func TestValidateHistoryRejectsGapMutationAndMissingPredecessor(t *testing.T) {
	for name, history := range map[string][]revisionDigest{
		"gap": {
			{Revision: 7, DigestSHA256: repeatedDigest("7")},
			{Revision: 9, DigestSHA256: repeatedDigest("9")},
		},
		"duplicate": {
			{Revision: 8, DigestSHA256: repeatedDigest("8")},
			{Revision: 8, DigestSHA256: repeatedDigest("8")},
		},
		"wrong-predecessor-digest": {
			{Revision: 8, DigestSHA256: repeatedDigest("8")},
			{Revision: 9, DigestSHA256: repeatedDigest("a")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateHistory(
				10,
				revisionDigest{Revision: 9, DigestSHA256: repeatedDigest("9")},
				history,
			); err == nil {
				t.Fatal("invalid signed history accepted")
			}
		})
	}
}

func repeatedDigest(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
