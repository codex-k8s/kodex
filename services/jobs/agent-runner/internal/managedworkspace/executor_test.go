package managedworkspace

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecutorNeverCleansHost checks that namespace-wide cleanup cannot run in this test process.
// TestExecutorNeverCleansHost проверяет невозможность namespace-wide cleanup в тестовом процессе.
func TestExecutorNeverCleansHost(t *testing.T) {
	if executorIdentity() {
		t.Skip("test process is the production executor")
	}
	if err := reapExecutorChildren(); !errors.Is(err, errExecutor) {
		t.Fatal("host cleanup not refused")
	}
}

// TestExecutorPinsInitializationAndQuiescence checks immutable command membership and lifecycle
// without starting a command or using real container process cleanup.
// TestExecutorPinsInitializationAndQuiescence проверяет immutable command membership и lifecycle
// без запуска команд и настоящего container process cleanup.
func TestExecutorPinsInitializationAndQuiescence(t *testing.T) {
	cleaned := 0
	executor := commandExecutor{cleanup: func() error { cleaned++; return nil }, probe: func(context.Context) error { return nil }}
	binding := strings.Repeat("a", 64)
	initialize := ExecutorRequest{Action: "INITIALIZE", Binding: binding, Commands: []string{"bash"}}
	if _, err := executor.handle(context.Background(), initialize); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.handle(context.Background(), initialize); err != nil {
		t.Fatal("same initialize is not idempotent")
	}
	changed := initialize
	changed.Commands = []string{"git"}
	if _, err := executor.handle(context.Background(), changed); err == nil {
		t.Fatal("rebind accepted")
	}
	if _, err := executor.handle(context.Background(), ExecutorRequest{Action: "RUN", Binding: binding, Command: "git", Directory: "work", TimeoutSeconds: 1}); err == nil {
		t.Fatal("unpublished command accepted")
	}
	if _, err := executor.handle(context.Background(), ExecutorRequest{Action: "QUIESCE", Binding: binding}); err != nil {
		t.Fatal(err)
	}
	if cleaned != 1 {
		t.Fatal("quiescence did not verify cleanup")
	}
	if _, err := executor.handle(context.Background(), initialize); err == nil {
		t.Fatal("closed instance reopened")
	}
}

// TestCommandCWDAndEnvironmentAreExplicit runs an actual inert shell in a disposable CWD with
// a synthetic parent secret, requiring no inheritance and exact process-exit evidence.
// TestCommandCWDAndEnvironmentAreExplicit запускает инертный shell в disposable CWD с synthetic
// parent secret, требуя отсутствия наследования и точного process-exit evidence.
func TestCommandCWDAndEnvironmentAreExplicit(t *testing.T) {
	t.Setenv("SYNTHETIC_DO_NOT_INHERIT", "test-secret")
	work := t.TempDir()
	request := ExecutorRequest{Action: "RUN", Command: "bash", Directory: "work", TimeoutSeconds: 2,
		Arguments: []string{"-c", "test -z \"$SYNTHETIC_DO_NOT_INHERIT\" && test \"$PWD\" = \"$1\" && printf safe", "probe", work}}
	cleaned := 0
	result, err := runCommandInWork(context.Background(), request, work, func() error { cleaned++; return nil })
	if err != nil || result.Status != "SUCCEEDED" || result.ExitCode != 0 || result.Output != "safe" || cleaned != 1 {
		t.Fatal(result, err, cleaned)
	}
	request.Directory = "work/../outside"
	if validCommandArguments(request) {
		t.Fatal("escaping cwd admitted")
	}
	request.Directory = "work"
	request.TimeoutSeconds = maximumCommandSeconds + 1
	if validCommandArguments(request) {
		t.Fatal("excessive timeout admitted")
	}
	if _, err := commandPath(filepath.Join(work, "bash")); err == nil {
		t.Fatal("arbitrary executable selected")
	}
}

// TestCommandOutputAndExitRemainTruthful verifies bounded stdout/stderr, nonzero exit and a
// failed cleanup's UNKNOWN outcome, without executing a malicious or detached child.
// TestCommandOutputAndExitRemainTruthful проверяет bounded stdout/stderr, nonzero exit и UNKNOWN
// при failed cleanup без исполнения вредоносного или detached child.
func TestCommandOutputAndExitRemainTruthful(t *testing.T) {
	request := ExecutorRequest{Command: "bash", Directory: "work", TimeoutSeconds: 2, Arguments: []string{"-c", "printf bad >&2; exit 7"}}
	result, err := runCommandInWork(context.Background(), request, t.TempDir(), func() error { return nil })
	if err != nil || result.Status != "FAILED" || result.ExitCode != 7 || result.Output != "bad" {
		t.Fatal(result, err)
	}
	result, err = runCommandInWork(context.Background(), request, t.TempDir(), func() error { return errExecutor })
	if err == nil || result.Status != "UNKNOWN" {
		t.Fatal("unknown cleanup became success", result, err)
	}
	output := &cappedOutput{}
	_, _ = output.Write([]byte(strings.Repeat("x", maximumCommandOutput+10)))
	if len(output.data) != maximumCommandOutput || !output.truncated {
		t.Fatal("output limit lost")
	}
}
