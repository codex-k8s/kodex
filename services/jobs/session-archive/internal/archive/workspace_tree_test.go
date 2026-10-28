package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// workspaceTreeFixture opens an isolated unprivileged root without bypassing production UID or mount proof.
// The scanner takes these trusted fixture IDs only to test descriptor and codec behavior.
// workspaceTreeFixture открывает isolated unprivileged root без обхода production UID/mount proof.
// Trusted fixture IDs используются только для проверки descriptor и codec behavior.
func workspaceTreeFixture(t *testing.T) (string, int) {
	t.Helper()
	root := t.TempDir()
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return root, fd
}

// TestWorkspaceTreeScannerPreservesPrivateExecutableAndWireOrder exercises real openat2 and canonical TAR bytes.
// Read-only content includes mode0600 and executable files, with SGID directories normalized to data permissions.
// TestWorkspaceTreeScannerPreservesPrivateExecutableAndWireOrder проверяет реальные openat2 и canonical TAR bytes.
// Сохраняются mode0600/executable files; SGID directories нормализуются до data permissions.
func TestWorkspaceTreeScannerPreservesPrivateExecutableAndWireOrder(t *testing.T) {
	root, fd := workspaceTreeFixture(t)
	if os.Mkdir(filepath.Join(root, "a"), 0o750) != nil || unix.Chmod(filepath.Join(root, "a"), 0o2750) != nil ||
		os.WriteFile(filepath.Join(root, "a", "start.sh"), []byte("#!/bin/sh\n"), 0o755) != nil ||
		os.Chmod(filepath.Join(root, "a", "start.sh"), 0o755) != nil ||
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("private"), 0o600) != nil {
		t.Fatal("create isolated tree")
	}
	var first, second bytes.Buffer
	digest, size, entries, streamBytes, err := scanWorkspaceTree(t.Context(), fd, &first, runtimecontract.RuntimeWorkspacePolicyV1(), uint32(os.Getuid()), uint32(os.Getgid()))
	if err != nil || size != 17 || entries != 3 || streamBytes != int64(first.Len()) || digest == "" {
		t.Fatal("valid private/executable tree rejected")
	}
	again, _, _, _, err := scanWorkspaceTree(t.Context(), fd, &second, runtimecontract.RuntimeWorkspacePolicyV1(), uint32(os.Getuid()), uint32(os.Getgid()))
	if err != nil || digest != again || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("reading source changed the canonical commitment")
	}
	reader := tar.NewReader(bytes.NewReader(first.Bytes()))
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if header.Name == "a.txt" && header.Mode != 0o600 || header.Name == "a/start.sh" && header.Mode != 0o755 || header.Name == "a/" && header.Mode != 0o750 {
			t.Fatal("source permissions changed on the canonical wire")
		}
	}
	if !reflect.DeepEqual(names, []string{"a.txt", "a/", "a/start.sh"}) {
		t.Fatal("filesystem name ordering disagrees with the canonical stream order")
	}
}

// TestWorkspaceTreeScannerRejectsLinksSpecialOwnershipAndCancellation preserves rejected source bytes.
// Quota and writer ownership are enforced before a manifest can describe the tree as complete.
// TestWorkspaceTreeScannerRejectsLinksSpecialOwnershipAndCancellation сохраняет rejected source bytes.
// Quota/writer ownership проверяются до объявления дерева полным в manifest.
func TestWorkspaceTreeScannerRejectsLinksSpecialOwnershipAndCancellation(t *testing.T) {
	for _, name := range []string{"symlink", "hardlink", "fifo", "owner", "cancel"} {
		t.Run(name, func(t *testing.T) {
			root, fd := workspaceTreeFixture(t)
			file := filepath.Join(root, "source")
			if err := os.WriteFile(file, []byte("preserved"), 0o600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch name {
			case "symlink":
				err = os.Symlink("source", filepath.Join(root, "link"))
			case "hardlink":
				err = os.Link(file, filepath.Join(root, "link"))
			case "fifo":
				err = unix.Mkfifo(filepath.Join(root, "pipe"), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			uid := uint32(os.Getuid())
			if name == "owner" {
				uid++
			}
			if name == "cancel" {
				cancel()
			}
			if _, _, _, _, err := scanWorkspaceTree(ctx, fd, io.Discard, runtimecontract.RuntimeWorkspacePolicyV1(), uid, uint32(os.Getgid())); err == nil {
				t.Fatal("unsupported tree became a complete checkpoint")
			}
			preserved, err := os.ReadFile(file)
			if err != nil || string(preserved) != "preserved" {
				t.Fatal("failed scan removed or rewrote source data")
			}
		})
	}
}
