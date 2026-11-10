//go:build linux

package security

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCommandSandboxKernelChild is the disposable subprocess entry. Irreversible restrictions
// never run in the parent test worker, and only synthetic temporary files are inspected.
// TestCommandSandboxKernelChild — disposable subprocess entry. Необратимые ограничения никогда
// не применяются к parent test worker; проверяются лишь synthetic temporary files.
func TestCommandSandboxKernelChild(t *testing.T) {
	if os.Getenv("KODEX_SYNTHETIC_SANDBOX_CHILD") != "1" {
		return
	}
	args := os.Args[len(os.Args)-3:]
	work, tmp, control := args[0], args[1], args[2]
	if !CommandFilesystemAvailable() {
		os.Exit(125)
	}
	if err := RestrictCommandFilesystem(work, tmp); err != nil {
		os.Exit(126)
	}
	if os.WriteFile(filepath.Join(work, "allowed"), []byte("safe"), 0o600) != nil || os.WriteFile(filepath.Join(tmp, "allowed"), []byte("safe"), 0o600) != nil {
		os.Exit(127)
	}
	if err := os.Remove(filepath.Join(control, "socket-marker")); err == nil {
		os.Exit(10)
	}
	if err := os.WriteFile(filepath.Join(control, "replacement"), []byte("unsafe"), 0o600); err == nil {
		os.Exit(11)
	}
	if err := os.Symlink(control, filepath.Join(work, "alias")); err != nil {
		os.Exit(12)
	}
	if err := os.WriteFile(filepath.Join(work, "alias/replacement"), []byte("unsafe"), 0o600); err == nil {
		os.Exit(13)
	}
	// A descendant must retain the same rule, including access through an in-work alias.
	// Потомок обязан сохранить правило, включая доступ через alias внутри work.
	child := exec.Command("/bin/sh", "-c", `printf unsafe > "$1/replacement"`, "probe", control)
	child.Env = []string{"PATH=/usr/bin:/bin"}
	if child.Run() == nil {
		os.Exit(14)
	}
	os.Exit(0)
}

// TestCommandSandboxPreventsControlSocketReplacement executes real Landlock in a separate
// process and proves both direct and descendant mutation denials. Unsupported kernels are
// explicitly skipped, never mistaken for a successful sandbox qualification.
// TestCommandSandboxPreventsControlSocketReplacement запускает настоящий Landlock в отдельном
// процессе и доказывает direct/descendant mutation denial. Unsupported kernel явно пропускается,
// а не считается успешной квалификацией sandbox.
func TestCommandSandboxPreventsControlSocketReplacement(t *testing.T) {
	base := t.TempDir()
	work, tmp, control := filepath.Join(base, "work"), filepath.Join(base, "tmp"), filepath.Join(base, "control")
	for _, name := range []string{work, tmp, control} {
		if err := os.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(control, "socket-marker")
	if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestCommandSandboxKernelChild$", "--", work, tmp, control)
	child.Env = []string{"PATH=/usr/bin:/bin", "KODEX_SYNTHETIC_SANDBOX_CHILD=1"}
	if err := child.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 125 {
			t.Skip("Landlock ABI 3 unavailable")
		}
		t.Fatal("real kernel sandbox failed", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("control marker modified")
	}
	if _, err := os.Stat(filepath.Join(control, "replacement")); !os.IsNotExist(err) {
		t.Fatal("control replacement created")
	}
}
