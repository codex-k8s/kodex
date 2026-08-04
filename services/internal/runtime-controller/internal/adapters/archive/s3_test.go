package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
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

func TestArchiveRejectsNestedSymlinkAndHardlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "parent"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", filepath.Join(root, "parent", "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeDeterministicArchive(&bytes.Buffer{}, root); err == nil {
		t.Fatal("nested symlink was accepted")
	}
	if err := os.Remove(filepath.Join(root, "parent", "escape")); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "parent", "original")
	if err := os.WriteFile(original, []byte("immutable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, filepath.Join(root, "parent", "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeDeterministicArchive(&bytes.Buffer{}, root); err == nil {
		t.Fatal("hard-linked file was accepted")
	}
}

func TestRestoredTreeDigestExcludesProofAndDetectsMutation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := treeSHA256(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rehydrateMarkerName), []byte(`{"schema":"proof"}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rehydrateOwnerName), []byte(`{"schema":"owner"}`), 0o400); err != nil {
		t.Fatal(err)
	}
	actual, err := restoredTreeSHA256(root)
	if err != nil || actual != expected {
		t.Fatalf("proof marker changed restored digest: %s %s %v", actual, expected, err)
	}
	if err := os.Chmod(filepath.Join(root, "state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	mutated, err := restoredTreeSHA256(root)
	if err != nil || mutated == expected {
		t.Fatalf("tree mutation was not detected: %s %v", mutated, err)
	}
}

func TestSyncTreeRejectsSymlinkBeforeDurablePublish(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state"), []byte("durable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncTree(t.Context(), root); err != nil {
		t.Fatalf("sync regular tree: %v", err)
	}
	if err := os.Symlink("state", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := syncTree(t.Context(), root); err == nil {
		t.Fatal("symlink reached durable publication")
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

func TestPinnedRetentionUsesOwnerPinInsteadOfSlidingWindow(t *testing.T) {
	pinnedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	execution := entity.Execution{
		PVCRetentionSeconds:     uint64((7 * 24 * time.Hour) / time.Second),
		ArchiveRetentionSeconds: uint64((90 * 24 * time.Hour) / time.Second),
		PVCCleanupEligibleAt:    pinnedAt.Add(7 * 24 * time.Hour),
		ArchiveRetainUntil:      pinnedAt.Add(90*24*time.Hour + time.Hour),
	}
	if !validPinnedRetention(execution, pinnedAt.Add(30*24*time.Hour)) {
		t.Fatal("valid owner-pinned retention became invalid as wall clock advanced")
	}
	execution.ArchiveRetainUntil = pinnedAt.Add(90*24*time.Hour - time.Second)
	if validPinnedRetention(execution, pinnedAt) {
		t.Fatal("retention shorter than the owner-pinned policy was accepted")
	}
}
