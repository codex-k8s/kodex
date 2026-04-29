package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("access-manager config failed", "error", err)
		os.Exit(1)
	}
	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("access-manager stopped", "error", err)
		os.Exit(1)
	}
}
