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
	lifecycle, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	shutdownBase := context.WithoutCancel(lifecycle)
	if err := app.RunRestoreController(lifecycle, shutdownBase, version); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("restore controller stopped with failure", "error", err)
		os.Exit(1)
	}
}
