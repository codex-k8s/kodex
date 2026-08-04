package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRemoveSafeStagingRejectsLinks(t *testing.T) {
	directory := t.TempDir()
	descriptor, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	staging := ".artifact.mattercodex-digest"
	if err := os.WriteFile(filepath.Join(directory, staging), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeSafeStaging(descriptor, staging, 64); err != nil {
		t.Fatalf("removeSafeStaging() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, staging)); !os.IsNotExist(err) {
		t.Fatalf("staging file still exists: %v", err)
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, staging)); err != nil {
		t.Fatal(err)
	}
	if err := removeSafeStaging(descriptor, staging, 64); err == nil {
		t.Fatal("removeSafeStaging() accepted a symlink")
	}
	if err := os.Remove(filepath.Join(directory, staging)); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(directory, staging)); err != nil {
		t.Fatal(err)
	}
	if err := removeSafeStaging(descriptor, staging, 64); err == nil {
		t.Fatal("removeSafeStaging() accepted a hard link")
	}
}
