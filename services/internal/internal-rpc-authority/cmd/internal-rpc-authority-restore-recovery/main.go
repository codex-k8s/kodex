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

// version carries the restore recovery binary's build label, defaulting to dev, and is passed to app.RunRestoreRecovery for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря restore recovery с исходным значением dev и передаётся в app.RunRestoreRecovery для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main passes signal-cancelled and root contexts plus the build version to restore recovery, logging and exiting nonzero only for failures other than context cancellation. Recovery-state transitions remain in the application runtime.
// main передаёт отменяемый сигналом и корневой контексты вместе с версией сборки runtime восстановления, журналируя и завершаясь ненулевым кодом только при ошибках, отличных от отмены контекста. Переходы состояния восстановления остаются в приложении.
func main() {
	root := context.Background()
	ctx, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunRestoreRecovery(ctx, root, version); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("restore recovery failed", "error", err)
		os.Exit(1)
	}
}
