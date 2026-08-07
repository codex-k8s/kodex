package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/app"
)

var version = "dev"

func main() {
	base := context.Background()
	lifecycle, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(lifecycle, base, version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
