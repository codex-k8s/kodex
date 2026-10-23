package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
)

// version carries the verifier binary's build label, defaulting to dev, and is passed to app.Run with ModeVerifier for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря verifier с исходным значением dev и передаётся в app.Run с ModeVerifier для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main creates a signal-cancelled lifecycle and separate uncancelled cleanup root for the verifier profile, formats failure through SafeRuntimeFailure and exits nonzero. It does not implement replay storage or authorization logic itself.
// main создаёт отменяемый сигналом жизненный цикл и отдельный неотменённый корень очистки для профиля verifier, форматирует ошибку через SafeRuntimeFailure и завершается ненулевым кодом. Хранилище replay и логика авторизации здесь не реализуются.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.Run(lifecycle, root, app.ModeVerifier, version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, app.SafeRuntimeFailure(app.ModeVerifier, err))
		os.Exit(1)
	}
}
