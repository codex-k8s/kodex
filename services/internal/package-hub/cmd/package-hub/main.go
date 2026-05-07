package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/app"
)

const serviceName = "package-hub"

func main() {
	os.Exit(run(newLogger(os.Stdout)))
}

func run(logger *slog.Logger) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer stop()

	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error(serviceName+" config failed", "error", err)
		return 1
	}
	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error(serviceName+" stopped", "error", err)
		return 1
	}
	return 0
}

func newLogger(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, nil))
}
