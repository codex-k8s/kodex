package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
)

// version carries the issuer binary's build label, defaulting to dev, and is passed to app.Run with ModeIssuer for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря issuer с исходным значением dev и передаётся в app.Run с ModeIssuer для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main creates a signal-cancelled lifecycle and a separate uncancelled root for issuer cleanup, then runs the issuer profile. Failure is formatted through SafeRuntimeFailure before nonzero exit; server startup and cleanup remain owned by app.Run.
// main создаёт отменяемый сигналом жизненный цикл и отдельный неотменённый корень для очистки issuer, затем запускает профиль issuer. Ошибка форматируется через SafeRuntimeFailure перед ненулевым выходом; запуск сервера и очистка принадлежат app.Run.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.Run(lifecycle, root, app.ModeIssuer, version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, app.SafeRuntimeFailure(app.ModeIssuer, err))
		os.Exit(1)
	}
}
