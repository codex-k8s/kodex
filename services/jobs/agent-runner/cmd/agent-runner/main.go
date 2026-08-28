package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/app"
)

var buildVersion = "dev"

func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(base, ctx, os.Args, buildVersion); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner execution failed: %v\n", err)
		os.Exit(1)
	}
}
