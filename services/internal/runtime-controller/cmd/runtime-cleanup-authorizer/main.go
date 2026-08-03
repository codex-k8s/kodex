package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := worker.RunCleanupAuthorizer(ctx); err != nil {
		slog.Error("runtime cleanup authorization failed", "error", err)
		os.Exit(1)
	}
}
