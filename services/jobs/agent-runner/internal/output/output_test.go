package output

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadOutputRejectsLinks(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "result.txt")
	if err := os.WriteFile(path, []byte("result"), 0o600); err != nil {
		t.Fatal(err)
	}
	descriptor, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	if raw, err := readOutput(descriptor, "result.txt"); err != nil || string(raw) != "result" {
		t.Fatalf("readOutput() = %q, %v", raw, err)
	}
	if err := os.Link(path, filepath.Join(directory, "hardlink.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := readOutput(descriptor, "result.txt"); err == nil {
		t.Fatal("readOutput() accepted a hard-linked artifact")
	}
	if err := os.Symlink(path, filepath.Join(directory, "symlink.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := readOutput(descriptor, "symlink.txt"); err == nil {
		t.Fatal("readOutput() accepted a symlink artifact")
	}
}
