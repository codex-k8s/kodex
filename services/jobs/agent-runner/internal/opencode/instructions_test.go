package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCurrentInstructionsArePrivateAndCannotBeReplaced checks immutable per-turn materialization.
// TestCurrentInstructionsArePrivateAndCannotBeReplaced проверяет immutable материализацию одного turn.
func TestCurrentInstructionsArePrivateAndCannotBeReplaced(t *testing.T) {
	home := t.TempDir()
	if os.Chmod(home, 0o700) != nil {
		t.Fatal("prepare private HOME")
	}
	const current = "Synthetic current owner instructions."
	path, err := InstallCurrentInstructions(home, current)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	data, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 || string(data) != current || filepath.Dir(path) != home {
		t.Fatal("current instructions are not exact private read-only bytes")
	}
	if _, err := InstallCurrentInstructions(home, "replacement"); err == nil {
		t.Fatal("same-turn instructions replaced")
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != current || RemoveManagedHome(home) != nil {
		t.Fatal("immutable materialization changed or escaped joined HOME cleanup")
	}
}

// TestCurrentInstructionsRejectUnsafeInputs checks confinement and bounded text without disclosure.
// TestCurrentInstructionsRejectUnsafeInputs проверяет confinement и ограниченный текст без раскрытия.
func TestCurrentInstructionsRejectUnsafeInputs(t *testing.T) {
	for _, kind := range []string{"shared_home", "linked_home", "linked_file", "oversize", "invalid_utf8", "nul"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			if os.Chmod(home, 0o700) != nil {
				t.Fatal("prepare private HOME")
			}
			text := "synthetic"
			switch kind {
			case "shared_home":
				if os.Chmod(home, 0o750) != nil {
					t.Fatal("prepare shared HOME")
				}
			case "linked_home":
				link := filepath.Join(t.TempDir(), "home")
				if os.Symlink(home, link) != nil {
					t.Fatal("prepare linked HOME")
				}
				home = link
			case "linked_file":
				if os.Symlink(filepath.Join(t.TempDir(), "foreign"), filepath.Join(home, "current-instructions.md")) != nil {
					t.Fatal("prepare linked current file")
				}
			case "oversize":
				text = strings.Repeat("x", (1<<20)+1)
			case "invalid_utf8":
				text = string([]byte{0xff})
			case "nul":
				text = "synthetic\x00text"
			}
			if _, err := InstallCurrentInstructions(home, text); err != ErrProtocol {
				t.Fatal("unsafe current instructions accepted or disclosed")
			}
		})
	}
}

// TestCurrentInstructionsAllowEmptyRevocation creates an empty current set without restoring old text.
// TestCurrentInstructionsAllowEmptyRevocation создаёт пустой current set без возврата прежнего текста.
func TestCurrentInstructionsAllowEmptyRevocation(t *testing.T) {
	home := t.TempDir()
	if os.Chmod(home, 0o700) != nil {
		t.Fatal("prepare private HOME")
	}
	path, err := InstallCurrentInstructions(home, "")
	data, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || len(data) != 0 {
		t.Fatal("empty current instruction set was not materialized")
	}
}
