package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/app"
)

var version = "dev"

func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.Run(lifecycle, root, version); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "control-plane failed: %v\n", err)
		os.Exit(1)
	}
}
