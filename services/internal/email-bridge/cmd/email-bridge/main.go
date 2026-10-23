package main

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/app"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// version is the build-injected service version, falling back to a local development label when unset.
// version содержит версию сервиса, подставляемую при сборке; без подстановки используется локальная метка разработки.
var version = "dev"

// main owns the root and signal-aware lifecycle contexts, starts the application, and reports startup or run failure at the process boundary.
// main создаёт корневой и зависящий от сигналов контексты жизненного цикла, запускает приложение и сообщает о сбое запуска или работы на границе процесса.
func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := app.Run(ctx, base, version); err != nil {
		app.LogFailure(slog.Default(), err)
		os.Exit(1)
	}
}
