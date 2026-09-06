package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/app"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
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
	if len(os.Args) > 1 {
		if len(os.Args) == 3 && os.Args[1] == "--prepare-artifact-spool" {
			return callback.PrepareArtifactSpool(os.Args[2])
		}
		return fmt.Errorf("runtime controller arguments are invalid")
	}
	lifecycleCtx, stop := signal.NotifyContext(
		backgroundCtx,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, version)
}
