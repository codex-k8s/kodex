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
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunDatabaseCredentialReconciler(
		lifecycle,
		root,
		version,
	); err != nil {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"internal-rpc-authority database credential reconciler failed: %v\n",
			err,
		)
		os.Exit(1)
	}
}
