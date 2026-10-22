package providercredential

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// checkCatalogRuntime executes only the configured binary's --version with a minimal private-home
// environment and a three-second budget, requires exact trimmed pinned output and bounds capture to 128
// bytes. Cancellation kills the process group; this is a version pin, not executable signature
// verification.
// checkCatalogRuntime выполняет только --version настроенного binary с минимальным окружением private
// home и бюджетом три секунды, требует точный закреплённый trimmed вывод и ограничивает capture 128
// байтами. Отмена завершает process group; это version-привязка, не проверка подписи executable.
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

// catalogVersionOutput holds only the bounded --version stdout capture; it does not retain stderr, model
// payload or account credentials.
// catalogVersionOutput хранит только ограниченный stdout capture --version без stderr, model payload и
// account credentials.
type catalogVersionOutput struct{ buffer bytes.Buffer }

// Write rejects a chunk that would grow version output beyond 128 bytes instead of truncating an
// oversized response into a valid-looking pin.
// Write отклоняет chunk, увеличивающий version вывод свыше 128 байтов, вместо усечения слишком большого
// ответа до правдоподобной привязки.
func (output *catalogVersionOutput) Write(value []byte) (int, error) {
	if output.buffer.Len()+len(value) > 128 {
		return 0, errModelCatalogUnverified
	}
	return output.buffer.Write(value)
}
