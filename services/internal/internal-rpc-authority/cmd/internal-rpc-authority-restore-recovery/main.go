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

var version = "dev"

func main() {
	root := context.Background()
	ctx, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunRestoreRecovery(ctx, root, version); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("restore recovery failed", "error", err)
		os.Exit(1)
	}
}
