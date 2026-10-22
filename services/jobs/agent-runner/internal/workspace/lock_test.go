package workspace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestWorkspaceLockProcess is an inert helper unless the explicit test-root variable is set; then it holds a directory lock and signals readiness until stdin releases it or the process exits.
// TestWorkspaceLockProcess — неактивный helper без явной test-root переменной; с ней держит directory lock и сигнализирует готовность до освобождения через stdin или выхода процесса.
func TestWorkspaceLockProcess(t *testing.T) {
	root := os.Getenv("KODEX_WORKSPACE_LOCK_TEST_ROOT")
	if root == "" {
		return
	}
	lock, err := Lock(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Fprintln(os.Stdout, "locked")
	var input [1]byte
	if _, err := io.ReadFull(os.Stdin, input[:]); err != nil && err != io.EOF {
		t.Fatal("workspace lock process control failed")
	}
}

// TestWorkspaceLockAcrossProcessesCancellationAndCrashRecovery starts a bounded copy of the test binary to hold the lock, verifies caller and local contention deadlines, kills that child and proves lock recovery without persistent files.
// TestWorkspaceLockAcrossProcessesCancellationAndCrashRecovery запускает ограниченную копию тестового бинаря с lock, проверяет сроки вызывающего и самой блокировки, завершает child и доказывает восстановление без постоянных файлов.
func TestWorkspaceLockAcrossProcessesCancellationAndCrashRecovery(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceLockProcess$")
	command.Env = []string{"KODEX_WORKSPACE_LOCK_TEST_ROOT=" + root}
	command.WaitDelay = time.Second
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatal("separate process did not acquire workspace lock")
	}
	short, cancelShort := context.WithTimeout(t.Context(), 30*time.Millisecond)
	started := time.Now()
	lock, err := Lock(short, root)
	cancelShort()
	if lock != nil {
		lock.Close()
	}
	if err == nil || DenialReason(err) != runtimecontract.RuntimeWorkspaceIOError || time.Since(started) > time.Second {
		t.Fatal("contended workspace lock ignored cancellation")
	}
	outer, cancelOuter := context.WithTimeout(t.Context(), 2*time.Second)
	lock, err = Lock(outer, root)
	if lock != nil {
		lock.Close()
	}
	if err == nil || outer.Err() != nil {
		cancelOuter()
		t.Fatal("workspace lock exceeded its own contention budget")
	}
	cancelOuter()
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	lock, err = Lock(t.Context(), root)
	if err != nil {
		t.Fatal("crashed process left a workspace lock")
	}
	lock.Close()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("workspace lock created a persistent file")
	}
}

// TestWorkspaceLockRejectsSymlinkRoot requires rejection when the supplied final root component is a symlink, without asserting ancestor-path validation.
// TestWorkspaceLockRejectsSymlinkRoot требует отказ, когда конечный компонент root — symlink, не утверждая проверки пути предков.
func TestWorkspaceLockRejectsSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if lock, err := Lock(t.Context(), link); err == nil {
		lock.Close()
		t.Fatal("workspace lock followed a symlink")
	}
}
