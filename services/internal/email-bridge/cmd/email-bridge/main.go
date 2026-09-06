package main

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/app"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := app.Run(ctx, base, version); err != nil {
		app.LogFailure(slog.Default(), err)
		os.Exit(1)
	}
}
