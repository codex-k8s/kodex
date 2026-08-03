package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDeterministicArchiveAndSafeRestore(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "data.txt"), []byte("stable"), 0o640); err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	if _, err := writeDeterministicArchive(&first, root); err != nil {
		t.Fatal(err)
	}
	if _, err := writeDeterministicArchive(&second, root); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("archive bytes are not deterministic")
	}
	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(archivePath, first.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	if err := extractArchive(archivePath, restored); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(restored, "dir", "data.txt"))
	if err != nil || string(raw) != "stable" {
		t.Fatalf("restored content mismatch: %v", err)
	}
}

func TestArchiveRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeDeterministicArchive(&bytes.Buffer{}, root); err == nil {
		t.Fatal("symlink was accepted")
	}
}

func TestVersionedReferenceRoundTrip(t *testing.T) {
	reference := buildReference("runtime", "tenant/session/archive.tar.gz", "version+1/2")
	bucket, key, version, err := parseReference(reference)
	if err != nil || bucket != "runtime" || key != "tenant/session/archive.tar.gz" || version != "version+1/2" {
		t.Fatalf("reference round trip failed: %q %q %q %v", bucket, key, version, err)
	}
	if _, _, _, err := parseReference("s3://runtime/tenant/session/archive.tar.gz?versionId=null"); err == nil {
		t.Fatal("unversioned S3 reference was accepted")
	}
}
