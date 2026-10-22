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

// version carries the restore operator binary's build label, defaulting to dev, and is passed to app.RunRestoreOperator for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря restore operator с исходным значением dev и передаётся в app.RunRestoreOperator для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main runs the restore-operator application with signal cancellation and a separate uncancelled root, suppressing error logging and nonzero exit only for context cancellation. It does not itself issue recovery authority or execute a restore.
// main запускает приложение restore-operator с отменой по сигналу и отдельным неотменённым корнем, подавляя журналирование ошибки и ненулевой выход только для отмены контекста. Само полномочие восстановления не выдаётся и восстановление не выполняется.
func main() {
	root := context.Background()
	ctx, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunRestoreOperator(ctx, root, version); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("restore operator failed", "error", err)
		os.Exit(1)
	}
}
