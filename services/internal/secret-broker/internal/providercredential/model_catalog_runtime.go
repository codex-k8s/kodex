package providercredential

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// Проверяется исполняемый pin, а не только имя Docker ARG или наличие файла.
func (process *AppServerProcess) checkCatalogRuntime(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, process.binary, "--version")
	command.Dir = process.root
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + process.root, "CODEX_HOME=" + process.root}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	command.WaitDelay = time.Second
	output := &catalogVersionOutput{}
	command.Stdout = output
	if command.Run() != nil || !bytes.Equal(bytes.TrimSpace(output.buffer.Bytes()), []byte("codex-cli "+catalogCodexVersion)) {
		return errModelCatalogUnverified
	}
	return nil
}

type catalogVersionOutput struct{ buffer bytes.Buffer }

func (output *catalogVersionOutput) Write(value []byte) (int, error) {
	if output.buffer.Len()+len(value) > 128 {
		return 0, errModelCatalogUnverified
	}
	return output.buffer.Write(value)
}
