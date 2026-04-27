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
	if err := app.Run(ctx, app.LoadConfigFromEnv(), logger); err != nil {
		logger.Error("access-manager stopped", "error", err)
		os.Exit(1)
	}
}
