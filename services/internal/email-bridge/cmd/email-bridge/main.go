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
	if app.Run(ctx, base, version) != nil {
		slog.Error("Email bridge stopped unexpectedly")
		os.Exit(1)
	}
}
