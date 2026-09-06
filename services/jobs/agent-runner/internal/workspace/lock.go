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
	workspaceLockBudget = time.Second
	workspaceLockPoll   = 5 * time.Millisecond
)

// Lock согласует служебные scan/write/cleanup между процессами одного
// execution. Блокируется inode корневого directory, без lock-файла в outbox.
// Это не authorization: provider writes по-прежнему проверяет workspace policy.
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
