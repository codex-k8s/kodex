package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integrationfixture"
)

// main owns the disposable fixture root context and logs a serving failure before exiting nonzero; it launches no platform agents.
// main владеет корневым контекстом одноразовой оснастки и пишет сбой обслуживания до ненулевого выхода, не запуская агентов платформы.
func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		slog.Error("integration synthetic fixture stopped", "error", err)
		os.Exit(1)
	}
}

// run derives a signal-cancelled lifecycle and retains the supplied independent base for fixture shutdown and stdout logging.
// run создаёт отменяемый сигналами lifecycle и сохраняет переданную независимую базу для остановки оснастки и записи в stdout.
func run(backgroundCtx context.Context) error {
	lifecycleCtx, stop := signal.NotifyContext(backgroundCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return integrationfixture.Run(lifecycleCtx, backgroundCtx, os.Stdout)
}
