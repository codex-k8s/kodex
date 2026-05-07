package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/app"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("provider-hub stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer stop()

	cfg, err := app.LoadConfig()
	if err != nil {
		return fmt.Errorf("provider-hub config failed: %w", err)
	}
	if err := app.Run(ctx, cfg, logger); err != nil {
		return err
	}
	return nil
}
