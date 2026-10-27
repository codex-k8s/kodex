package archive

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestWorkspaceRestoreFilesystemFencesEntries checks real no-overwrite, path, hard-link and partial-write boundaries.
// Its fixture uses the current unprivileged UID; distinct production identities are checked separately.
// TestWorkspaceRestoreFilesystemFencesEntries проверяет настоящие no-overwrite, path, hard-link и partial-write boundaries.
// Fixture использует текущий непривилегированный UID; разные production identities проверяются отдельно.
func TestWorkspaceRestoreFilesystemFencesEntries(t *testing.T) {
	for _, boundary := range []string{"traversal", "symlink", "overwrite", "hardlink", "short", "overrun", "mode", "directory-mode", "identity"} {
		t.Run(boundary, func(t *testing.T) {
			root, files, fd := workspaceRestoreFilesystemFixture(t, t.Context())
			switch boundary {
			case "traversal":
				if _, err := files.File(t.Context(), "../escape", 0o600, 0); err == nil {
					t.Fatal("parent path escaped restore root")
				}
			case "symlink":
				outside := t.TempDir()
				if os.Symlink(outside, filepath.Join(root, "link")) != nil {
					t.Fatal("prepare symlink fixture")
				}
				if _, err := files.File(t.Context(), "link/escape", 0o600, 0); err == nil {
					t.Fatal("symlink crossed restore root")
				}
				if _, err := os.Lstat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
					t.Fatal("restore changed an outside directory")
				}
			case "overwrite":
				if os.WriteFile(filepath.Join(root, "file"), []byte("retained"), 0o600) != nil {
					t.Fatal("prepare previous file")
				}
				if _, err := files.File(t.Context(), "file", 0o600, 2); err == nil {
					t.Fatal("restore overwrote an existing entry")
				}
				raw, err := os.ReadFile(filepath.Join(root, "file"))
				if err != nil || string(raw) != "retained" {
					t.Fatal("failed restore damaged previous bytes")
				}
			case "hardlink", "short", "overrun":
				file, err := files.File(t.Context(), "file", 0o700, 2)
				if err != nil {
					t.Fatal("open bounded destination")
				}
				defer file.Close()
				switch boundary {
				case "hardlink":
					if _, err := file.Write([]byte("ok")); err != nil || os.Link(filepath.Join(root, "file"), filepath.Join(root, "alias")) != nil {
						t.Fatal("prepare hard-link fixture")
					}
				case "short":
					if _, err := file.Write([]byte("o")); err != nil {
						t.Fatal("prepare partial file")
					}
				case "overrun":
					if n, err := file.Write([]byte("too")); err == nil || n != 0 {
						t.Fatal("over-limit bytes were acknowledged")
					}
				}
				if file.Close() == nil {
					t.Fatal("unconfirmed file was accepted")
				}
				info, err := os.Lstat(filepath.Join(root, "file"))
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal("partial evidence disappeared or received final permissions")
				}
			case "mode":
				if _, err := files.File(t.Context(), "file", 0o4755, 0); err == nil {
					t.Fatal("privileged mode entered a restore")
				}
			case "directory-mode":
				if files.Directory(t.Context(), "directory") != nil || unix.Chmod(filepath.Join(root, "directory"), 0o755) != nil {
					t.Fatal("prepare changed directory fixture")
				}
				if files.SealDirectory(t.Context(), "directory", 0o700) == nil {
					t.Fatal("changed directory was silently repaired")
				}
			case "identity":
				identity := files.identity
				identity.actor++
				if unexpected, err := newWorkspaceRestoreFilesystem(t.Context(), fd, files.policy, identity); err == nil {
					_ = unexpected.Close()
					t.Fatal("caller-selected UID acquired a writer")
				}
			}
		})
	}
}
