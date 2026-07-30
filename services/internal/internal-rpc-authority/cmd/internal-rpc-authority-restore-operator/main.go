package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunRestoreOperator(ctx); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("restore operator failed", "error", err)
		os.Exit(1)
	}
}
