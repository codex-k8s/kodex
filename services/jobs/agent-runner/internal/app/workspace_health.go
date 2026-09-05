package app

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

const (
	workspaceCanaryMode          = "runtime-workspace-canary"
	workspaceCanaryBudget        = 2 * time.Second
	workspaceCanaryCleanupBudget = time.Second
	workspaceCanaryInterval      = 5 * time.Second
	workspaceCanaryFreshness     = 10 * time.Second
)

type workspaceHealth struct {
	checked time.Time
	err     error
}

// Probe читает только ограниченно свежий результат. Файловая система может
// остановиться внутри syscall, поэтому проверка выполняется отдельным процессом.
func (state *health) workspaceStatus(now time.Time) error {
	result := state.workspace.Load()
	if result == nil || now.Before(result.checked) || now.Sub(result.checked) > workspaceCanaryFreshness {
		return &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return result.err
}

func startWorkspaceMonitor(ctx context.Context, state *health, check func(context.Context) error) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer state.workspace.Store(nil)
		for {
			if ctx.Err() != nil {
				return
			}
			err := check(ctx)
			state.workspace.Store(&workspaceHealth{checked: time.Now(), err: err})
			timer := time.NewTimer(workspaceCanaryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}

func checkWorkspaceProcess(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, workspaceCanaryBudget)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/local/bin/kodex-agent-runner", workspaceCanaryMode)
	// Helper не получает credentials из окружения и не открывает сетевые клиенты.
	command.Env = []string{"LANG=C"}
	return runCanaryCommand(ctx, command)
}

func runCanaryCommand(ctx context.Context, command *exec.Cmd) error {
	var output canaryOutput
	command.Stdout = &output
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = workspaceCanaryCleanupBudget
	if err := command.Run(); err != nil || ctx.Err() != nil || output.overflow {
		return &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	switch string(output.value) {
	case "OK":
		return nil
	case runtimecontract.RuntimeWorkspaceReadOnly, runtimecontract.RuntimeWorkspaceQuotaExceeded,
		runtimecontract.RuntimeWorkspacePathOutsideWorkspace, runtimecontract.RuntimeWorkspaceIOError:
		return &workspacepolicy.Denial{Reason: string(output.value)}
	default:
		return &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
}

type canaryOutput struct {
	value    []byte
	overflow bool
}

func (output *canaryOutput) Write(value []byte) (int, error) {
	if len(value) > 64-len(output.value) {
		output.overflow = true
		return 0, errors.New("workspace canary response exceeds limit")
	}
	output.value = append(output.value, value...)
	return len(value), nil
}
