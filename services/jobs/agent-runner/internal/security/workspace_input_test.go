package security

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProtectWorkspaceInputTree(t *testing.T) {
	for _, invalid := range []string{"../input", "input/../knowledge", "/input", "input/child", ".kodex/state", "context"} {
		if ProtectWorkspaceInputTree(t.TempDir(), invalid) == nil {
			t.Fatal("invalid tree accepted")
		}
	}
	for _, kind := range []string{"ordinary", "symlink", "hardlink", "root-file", "root-symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			tree := filepath.Join(root, "input")
			if kind == "root-symlink" {
				if err := os.Symlink(root, tree); err != nil {
					t.Fatal(err)
				}
				if ProtectWorkspaceInputTree(root, "input") == nil {
					t.Fatal("root symlink accepted")
				}
				return
			}
			if kind == "root-file" {
				if err := os.WriteFile(tree, []byte("synthetic"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(tree, "nested"), 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(tree, "nested", "proof")
				if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink(root, filepath.Join(tree, "link")); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "hardlink" {
					if err := os.Link(path, filepath.Join(root, "alias")); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "fifo" {
					if err := unix.Mkfifo(filepath.Join(tree, "pipe"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := ProtectWorkspaceInputTree(root, "input")
			if kind != "ordinary" {
				if err == nil {
					t.Fatal("unsafe tree accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := ProtectWorkspaceInputTree(root, "input"); err != nil {
				t.Fatal(err)
			}
			for relative, mode := range map[string]os.FileMode{"nested": 0750 | os.ModeSetgid, "nested/proof": 0440} {
				info, err := os.Lstat(filepath.Join(tree, relative))
				if err != nil || info.Mode()&(os.ModePerm|os.ModeSetgid) != mode {
					t.Fatal("protected mode mismatch")
				}
			}
		})
	}
}
