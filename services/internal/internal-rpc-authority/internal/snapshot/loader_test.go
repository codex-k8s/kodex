package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

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
