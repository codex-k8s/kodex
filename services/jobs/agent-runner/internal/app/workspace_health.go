package app

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

// runWorkspaceCanary probes the declared workspace first and only on success probes environment volumes using the same caller context; a failure prevents the next probe.
// runWorkspaceCanary сначала проверяет объявленный workspace и лишь при успехе тома окружения в том же контексте; отказ предотвращает следующую пробу.
func runWorkspaceCanary(ctx context.Context, input model.Input) error {
	if err := workspacepolicy.RunCanary(ctx, input.WorkspaceRoot, input.WorkspacePolicy); err != nil {
		return err
	}
	return workspacepolicy.RunEnvironmentCanary(ctx, input.WorkspaceRoot, input.EnvironmentPolicy)
}

const (
	// workspaceCanaryMode is the fixed runner subcommand used solely for isolated filesystem readiness probing rather than model or network work.
	// workspaceCanaryMode — фиксированная подкоманда runner для изолированной файловой пробы готовности, не model- или network-работы.
	workspaceCanaryMode = "runtime-workspace-canary"
	// workspaceCanaryBudget limits each production canary process context to two seconds before the configured graceful/forced cleanup path.
	// workspaceCanaryBudget ограничивает контекст каждой production canary двумя секундами до заданной graceful/forced очистки процесса.
	workspaceCanaryBudget = 2 * time.Second
	// workspaceCanaryCleanupBudget bounds exec.WaitDelay after cancellation or child exit, allowing SIGTERM before forced child/pipe cleanup without promising process-group termination.
	// workspaceCanaryCleanupBudget ограничивает exec.WaitDelay после отмены или выхода child, оставляя SIGTERM до принудительной очистки child/pipes без обещания завершения всей process group.
	workspaceCanaryCleanupBudget = time.Second
	// workspaceCanaryInterval delays the next probe by five seconds after the preceding check finishes, avoiding concurrent filesystem probes in one monitor.
	// workspaceCanaryInterval откладывает следующую пробу на пять секунд после предыдущей, исключая одновременные файловые пробы одного monitor.
	workspaceCanaryInterval = 5 * time.Second
	// workspaceCanaryFreshness is the maximum accepted snapshot age; missing or future timestamps also deny readiness without filesystem access in the health handler.
	// workspaceCanaryFreshness — максимальный допустимый возраст снимка; отсутствие и будущие timestamps также запрещают готовность без обращения health handler к ФС.
	workspaceCanaryFreshness = 10 * time.Second
)

// workspaceHealth stores the completion time and error of one monitor probe for atomic publication to readiness readers, not a durable filesystem guarantee.
// workspaceHealth хранит время завершения и ошибку одной пробы для атомарной публикации readiness readers, а не устойчивую гарантию состояния ФС.
type workspaceHealth struct {
	checked time.Time
	err     error
}

// workspaceStatus returns only a present, nonfuture snapshot no older than the freshness budget; otherwise it returns a generic workspace denial and never performs filesystem I/O itself.
// workspaceStatus возвращает лишь имеющийся не-будущий снимок в пределах freshness budget; иначе даёт общий workspace denial и сам не выполняет файловый I/O.
func (state *health) workspaceStatus(now time.Time) error {
	result := state.workspace.Load()
	if result == nil || now.Before(result.checked) || now.Sub(result.checked) > workspaceCanaryFreshness {
		return &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return result.err
}

// startWorkspaceMonitor owns one sequential probe goroutine, publishes each result atomically and clears it on exit. Its stop callback cancels and joins the goroutine, so the supplied check must honor cancellation or independently bound its work.
// startWorkspaceMonitor владеет одной последовательной goroutine проб, атомарно публикует результаты и очищает их при выходе. Stop callback отменяет и дожидается goroutine, поэтому check обязан учитывать отмену или самостоятельно ограничивать работу.
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

// checkWorkspaceProcess starts the fixed runner canary under a two-second context with only LANG=C in its environment, delegating bounded stdout and cleanup to runCanaryCommand; no provider credential environment is forwarded.
// checkWorkspaceProcess запускает фиксированную runner canary с двухсекундным контекстом и только LANG=C в окружении, передавая ограничение stdout и очистку runCanaryCommand; окружение provider-реквизитов не передаётся.
func checkWorkspaceProcess(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, workspaceCanaryBudget)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/local/bin/kodex-agent-runner", workspaceCanaryMode)
	// Helper не получает credentials из окружения и не открывает сетевые клиенты.
	command.Env = []string{"LANG=C"}
	return runCanaryCommand(ctx, command)
}

// runCanaryCommand runs a caller-context-bound command with bounded stdout, SIGTERM cancellation and a one-second WaitDelay. Only exact OK succeeds; known failure tokens survive and all process, cancellation, overflow or unknown-output failures map to generic I/O without exposing stdout.
// runCanaryCommand запускает привязанную к контексту вызывающего команду с ограниченным stdout, отменой SIGTERM и WaitDelay одна секунда. Успешен лишь точный OK; известные отказы сохраняются, а ошибки процесса, отмена, overflow и неизвестный вывод становятся общей I/O без раскрытия stdout.
func runCanaryCommand(ctx context.Context, command *exec.Cmd) error {
	var output canaryOutput
	command.Stdout = &output
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = workspaceCanaryCleanupBudget
	if err := command.Run(); err != nil || ctx.Err() != nil || output.overflow {
		return &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if string(output.value) == "OK" {
		return nil
	}
	return &workspacepolicy.Denial{Reason: closedWorkspaceReason(string(output.value))}
}

// canaryOutput retains at most sixty-four stdout bytes and a sticky overflow flag so an oversized response cannot later be accepted as a valid canary token.
// canaryOutput сохраняет не более шестидесяти четырёх байтов stdout и sticky overflow flag, не позволяя позднее принять чрезмерный ответ за корректный canary token.
type canaryOutput struct {
	value    []byte
	overflow bool
}

// Write appends only a fully fitting chunk; an overflowing chunk is rejected with zero progress and a fixed error while the overflow flag remains set for the final process check.
// Write добавляет лишь целиком помещающийся фрагмент; чрезмерный отклоняет с нулевым прогрессом и фиксированной ошибкой, сохраняя overflow flag для итоговой проверки процесса.
func (output *canaryOutput) Write(value []byte) (int, error) {
	if len(value) > 64-len(output.value) {
		output.overflow = true
		return 0, errors.New("workspace canary response exceeds limit")
	}
	output.value = append(output.value, value...)
	return len(value), nil
}
