package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/app"
)

var version = "dev"

func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err := app.Run(lifecycle, context.Background(), version)
	if err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(os.Stderr, "integration-gateway stopped with an error")
		os.Exit(1)
	}
}
