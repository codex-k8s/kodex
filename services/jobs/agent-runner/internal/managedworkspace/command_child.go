package managedworkspace

import (
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	"golang.org/x/sys/unix"
)

// CommandChildMode is a private pre-exec kernel-confinement entrypoint. It accepts no credentials
// or endpoint and cannot replace the trusted container executor or its owner admission.
// CommandChildMode — private pre-exec kernel-confinement entrypoint. Credentials и endpoint не
// принимает и не заменяет trusted container executor либо owner admission.
const CommandChildMode = runtimecontract.WorkspaceCommandChildMode

// RunCommandChild installs irreversible filesystem mutation restrictions before replacing this
// secret-free child with one registered image tool. A selected shell keeps the same restrictions
// across every fork/exec; it cannot unlink/rebind the daemon's authority socket.
// RunCommandChild ставит необратимые filesystem mutation restrictions перед заменой этого child
// без секретов выбранным image tool. Shell сохраняет ограничения через каждый fork/exec и не
// может удалить либо перепривязать authority socket daemon.
func RunCommandChild(args []string) error {
	if os.Getpid() == 1 || os.Geteuid() != 10002 || os.Getegid() != 29000 || len(args) < 1 || len(args) > 65 || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return errExecutor
	}
	probe := len(args) == 1 && args[0] == "--sandbox-probe"
	executable := ""
	var err error
	if !probe {
		executable, err = commandPath(args[0])
		if err != nil {
			return err
		}
	}
	if security.RestrictCommandFilesystem(runtimecontract.RuntimeWorkspaceWorkingRoot, "/tmp") != nil {
		return errExecutor
	}
	if probe {
		fd, err := unix.Open("/run/kodex/workspace/authority/.sandbox-probe", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
		if err == nil {
			unix.Close(fd)
			return errExecutor
		}
		if err != unix.EACCES && err != unix.EPERM {
			return errExecutor
		}
		return nil
	}
	if unix.Exec(executable, append([]string{executable}, args[1:]...), commandEnvironment()) != nil {
		return errExecutor
	}
	return nil
}

// commandEnvironment enumerates only fixed public process settings. Provider, callback,
// integration and user environment variables are never inherited by repository commands.
// commandEnvironment перечисляет лишь fixed public process settings. Provider, callback,
// integration и user environment variables не наследуются командами репозитория.
func commandEnvironment() []string {
	return []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/tmp", "TMPDIR=/tmp", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "CI=true", "NO_COLOR=1", "GOMAXPROCS=2", "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOCACHE=/tmp/go-cache", "GOMODCACHE=/tmp/go-mod", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
}
