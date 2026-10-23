package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
)

// version carries the restore controller binary's build label, defaulting to dev, and is passed to app.RunRestoreController for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря restore controller с исходным значением dev и передаётся в app.RunRestoreController для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main creates the restore controller's signal-cancelled lifecycle and derives a cancellation-independent shutdown context with WithoutCancel. It logs and exits nonzero for returned errors other than context cancellation, leaving controller work and shutdown to the application.
// main создаёт отменяемый сигналом жизненный цикл restore controller и выводит независимый от отмены контекст завершения через WithoutCancel. Возвращённые ошибки, кроме отмены контекста, журналируются и дают ненулевой выход; работа контроллера и завершение переданы приложению.
func main() {
	lifecycle, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	shutdownBase := context.WithoutCancel(lifecycle)
	if err := app.RunRestoreController(lifecycle, shutdownBase, version); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("restore controller stopped with failure", "error", err)
		os.Exit(1)
	}
}
