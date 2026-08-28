package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/app"
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
	command := "serve"
	if len(os.Args) > 2 {
		return fmt.Errorf("backup-controller accepts at most one command")
	}
	if len(os.Args) == 2 {
		command = os.Args[1]
	}
	lifecycleCtx, stop := signal.NotifyContext(backgroundCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, command, version)
}
