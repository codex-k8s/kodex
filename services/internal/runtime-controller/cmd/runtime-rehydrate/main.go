package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/worker"
)

func main() {
	if err := worker.RunRehydrate(context.Background()); err != nil {
		slog.Error("runtime rehydrate failed", "error", err)
		os.Exit(1)
	}
}
