package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integrationfixture"
)

func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		slog.Error("integration synthetic fixture stopped", "error", err)
		os.Exit(1)
	}
}

func run(backgroundCtx context.Context) error {
	lifecycleCtx, stop := signal.NotifyContext(backgroundCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return integrationfixture.Run(lifecycleCtx, backgroundCtx, os.Stdout)
}
