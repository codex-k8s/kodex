package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/app"
	"github.com/google/uuid"
)

var buildVersion = "dev"

func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(base, ctx, os.Args, buildVersion); err != nil {
		executionID := os.Getenv("MATTERCODEX_EXECUTION_ID")
		if uuid.Validate(executionID) != nil {
			executionID = "unbound"
		}
		fmt.Fprintf(os.Stderr, "agent-runner execution failed: execution_id=%s\n", executionID)
		os.Exit(1)
	}
}
