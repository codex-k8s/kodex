package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

const (
	// workspaceLockBudget caps cooperative lock contention at one second even if the caller grants a longer deadline.
	// workspaceLockBudget ограничивает ожидание согласующей блокировки одной секундой даже при более позднем сроке вызывающего.
	workspaceLockBudget = time.Second
	// workspaceLockPoll is the cancellable retry interval after nonblocking flock contention or interruption, avoiding a busy loop.
	// workspaceLockPoll задаёт отменяемый интервал повторов после nonblocking flock contention или прерывания без активного кручения.
	workspaceLockPoll = 5 * time.Millisecond
)

// Lock acquires an exclusive advisory flock on the root directory inode without a persistent lock file, retrying under caller cancellation and a one-second budget. Closing the returned handle releases it; it coordinates service writes but is not authorization or a provider filesystem sandbox.
// Lock получает исключительный advisory flock inode корневого каталога без постоянного lock-файла, повторяя при отмене вызывающего и бюджете одна секунда. Закрытие handle освобождает lock; это согласование служебных записей, не авторизация и не файловая песочница provider.
func Lock(ctx context.Context, root string) (*os.File, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceLockBudget)
	defer cancel()
	descriptor, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, classify(err)
	}
	file := os.NewFile(uintptr(descriptor), "runtime-workspace-lock")
	for {
		if ctx.Err() != nil {
			_ = file.Close()
			return nil, &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
		}
		err = unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return nil, classify(err)
		}
		timer := time.NewTimer(workspaceLockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
