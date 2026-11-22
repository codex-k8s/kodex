// Package app owns the bounded process lifetime of one operator-installed managed SSH helper; it starts no daemon, retry loop or shell.
// Package app владеет ограниченным process lifetime одного устанавливаемого оператором managed SSH helper; daemon, retry loop либо shell не запускаются.
package app

import (
	"context"
	"os"
	"time"

	"github.com/codex-k8s/kodex/tools/managed-ssh-helper/internal/helper"
	"golang.org/x/sys/unix"
)

// Run admits only the installed helper profile and closes the process-owned input/output at its deadline, including peers that never finish sending or receiving a frame.
// Run допускает только installed helper profile и закрывает process-owned input/output при deadline, включая peers, которые не завершают передачу либо чтение frame.
func Run(parent context.Context, arguments []string, input, output *os.File) error {
	if parent == nil || len(arguments) != 1 {
		return helper.ErrRejected
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	input, err := pollableFile(input)
	if err != nil {
		return helper.ErrRejected
	}
	defer input.Close()
	output, err = pollableFile(output)
	if err != nil {
		return helper.ErrRejected
	}
	defer output.Close()
	deadline, _ := ctx.Deadline()
	if input.SetReadDeadline(deadline) != nil || output.SetWriteDeadline(deadline) != nil {
		return helper.ErrRejected
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		input.Close()
		output.Close()
		close(finished)
	})
	defer func() {
		if !stop() {
			<-finished
		}
	}()
	runtime, err := helper.OpenInstalled(ctx)
	if err != nil {
		return helper.ErrRejected
	}
	defer runtime.Close()
	if runtime.Serve(ctx, input, output) != nil || ctx.Err() != nil {
		return helper.ErrRejected
	}
	return nil
}

// pollableFile creates a process-owned nonblocking descriptor before os.NewFile registers it with the Go poller; ordinary inherited stdin may otherwise remain an uninterruptible blocking read.
// pollableFile до регистрации os.NewFile в Go poller создаёт process-owned nonblocking descriptor; обычный inherited stdin иначе может остаться непрерываемым blocking read.
func pollableFile(file *os.File) (*os.File, error) {
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, helper.ErrRejected
	}
	if unix.SetNonblock(fd, true) != nil {
		unix.Close(fd)
		return nil, helper.ErrRejected
	}
	return os.NewFile(uintptr(fd), "managed-ssh-channel"), nil
}
