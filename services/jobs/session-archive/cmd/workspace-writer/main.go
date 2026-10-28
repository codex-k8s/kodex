package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/worker"
)

// main owns the writable helper's signal lifecycle and closed modes without importing the Kubernetes controller or reading credentials.
// main владеет signal lifecycle writable helper и closed modes без Kubernetes controller или чтения credentials.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(root, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) == 2 && os.Args[1] == "write" {
		err = worker.WriteWorkspace(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "prepare" {
		err = worker.PrepareWorkspaceWriter(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "generation" {
		err = worker.PrepareWorkspaceGeneration(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "volume" {
		err = worker.PrepareWorkspaceVolume(lifecycle, false)
	} else if len(os.Args) == 2 && os.Args[1] == "volume-readback" {
		err = worker.PrepareWorkspaceVolume(lifecycle, true)
	} else {
		err = fmt.Errorf("workspace writer mode is required")
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "session workspace writer failed")
		os.Exit(1)
	}
}
