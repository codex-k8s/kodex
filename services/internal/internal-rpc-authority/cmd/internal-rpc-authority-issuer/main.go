package main

import (
	"context"
	"fmt"
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
	if err := app.Run(lifecycle, context.Background(), app.ModeIssuer, version); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "internal-rpc-authority issuer failed: %v\n", err)
		os.Exit(1)
	}
}
