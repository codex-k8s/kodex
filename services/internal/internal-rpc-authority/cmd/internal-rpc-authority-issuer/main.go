package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
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
	if err := app.Run(lifecycle, root, app.ModeIssuer, version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, app.SafeRuntimeFailure(app.ModeIssuer, err))
		os.Exit(1)
	}
}
