package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/app"
)

var version = "dev"

func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(backgroundCtx context.Context) error {
	lifecycleCtx, stop := signal.NotifyContext(
		backgroundCtx,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, version)
}
