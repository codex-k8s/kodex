package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/app"
)

// version identifies the built artifact in process telemetry and defaults to development.
// RU: version обозначает собранный артефакт в telemetry процесса и по умолчанию равна development.
var version = "dev"

// main owns the root context and reports a bounded startup or lifecycle error to stderr.
// RU: main владеет корневым контекстом и выводит ограниченную ошибку запуска либо жизненного цикла в stderr.
func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run derives signal cancellation while preserving the uncanceled root for bounded shutdown.
// RU: run создаёт отмену по сигналу, сохраняя неотменённый корень для ограниченной остановки.
func run(backgroundCtx context.Context) error {
	lifecycleCtx, stop := signal.NotifyContext(backgroundCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, version)
}
