package helper

import (
	"context"
	"os"
	"path"
	"strconv"

	"golang.org/x/sys/unix"
)

// installedHelperExecutable is the only program the managed account shell may execute, independently of SSH_ORIGINAL_COMMAND or another sshd listener's ForceCommand configuration.
// installedHelperExecutable задаёт единственную программу managed account shell независимо от SSH_ORIGINAL_COMMAND либо ForceCommand другого sshd listener.
const installedHelperExecutable = "/usr/local/libexec/kodex-ssh-target"

// ExecManagedAccountShell replaces this standalone wrapper process only for OpenSSH's exact fixed-helper argv; interactive shells, subsystems, arguments and shell syntax are rejected before touching stdin.
// ExecManagedAccountShell заменяет standalone wrapper process только при exact fixed-helper argv OpenSSH; interactive shells, subsystems, arguments и shell syntax отклоняются до чтения stdin.
func ExecManagedAccountShell(ctx context.Context, arguments []string) error {
	if len(arguments) != 2 || arguments[0] != "-c" || arguments[1] != installedHelperExecutable {
		return ErrRejected
	}
	profile, err := installedProfile(ctx, true)
	if err != nil {
		return ErrRejected
	}
	parent, err := operatorDirectory(path.Dir(installedHelperExecutable))
	if err != nil {
		return ErrRejected
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), path.Base(installedHelperExecutable), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return ErrRejected
	}
	file := os.NewFile(uintptr(fd), "managed-helper")
	defer file.Close()
	if !operatorFile(file, true) || fileDigest(file, 64<<20) != profile.ExecutableDigest || ctx.Err() != nil {
		return ErrRejected
	}
	// Executing the verified inode through procfs prevents a pathname replacement from changing the selected executable between digest validation and exec.
	// Выполнение проверенного inode через procfs запрещает pathname replacement подменить выбранный executable между digest validation и exec.
	_ = unix.Exec("/proc/self/fd/"+strconv.Itoa(fd), []string{installedHelperExecutable}, []string{"PATH=/usr/bin:/bin", "LANG=C", "HOME=/nonexistent"})
	return ErrRejected
}
