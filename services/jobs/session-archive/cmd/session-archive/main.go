package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/app"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/worker"
)

var version = "dev"

func main() {
	background := context.Background()
	lifecycle, stop := signal.NotifyContext(background, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) == 2 && os.Args[1] == "worker" {
		err = worker.Run(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "controller" {
		err = app.Run(lifecycle, background, version)
	} else {
		err = fmt.Errorf("session-archive mode is required")
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
