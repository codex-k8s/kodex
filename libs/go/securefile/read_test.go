package securefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAcceptsExactReadOnlyModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o400, 0o440, 0o444} {
		mode := mode
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte("value"), mode); err != nil {
				t.Fatal(err)
			}
			value, err := Read(path, 16)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if string(value) != "value" {
				t.Fatalf("Read() returned unexpected value length %d", len(value))
			}
		})
	}
}

func TestReadRejectsWritableExecutableAndOwnerUnreadableModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o000, 0o004, 0o040, 0o404, 0o500, 0o600, 0o640, 0o644, 0o660} {
		mode := mode
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(path, 16); err == nil {
				t.Fatalf("Read() accepted mode %04o", mode)
			}
		})
	}
}

func TestReadWithinRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(outside, []byte("value"), 0o400); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "credential")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWithin(root, link, 16); err == nil {
		t.Fatal("ReadWithin() accepted a symlink outside root")
	}
}

func TestReadAcceptsProjectedSymlinkInsideMount(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	versioned := filepath.Join(root, "..data")
	if err := os.Mkdir(versioned, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(versioned, "credential")
	if err := os.WriteFile(target, []byte("value"), 0o444); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "credential")
	if err := os.Symlink(filepath.Join("..data", "credential"), link); err != nil {
		t.Fatal(err)
	}
	value, err := Read(link, 16)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(value) != "value" {
		t.Fatalf("Read() returned unexpected value length %d", len(value))
	}
}

func TestReadRejectsUnboundedAndOversizedFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("value"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 0); err == nil {
		t.Fatal("Read() accepted a zero size boundary")
	}
	if _, err := Read(path, 4); err == nil {
		t.Fatal("Read() accepted an oversized file")
	}
}
