package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/worker"
)

// main owns the bounded reader lifecycle without importing the Kubernetes controller or loading credentials.
// Both explicit modes retain fixed paths and UIDs; no caller-selected source or command is accepted.
// main владеет bounded reader lifecycle без Kubernetes controller и загрузки credentials.
// Оба явных режима сохраняют fixed paths/UID; caller-selected source или command не принимаются.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(root, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) == 2 && os.Args[1] == "read" {
		err = worker.ReadWorkspace(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "prepare" {
		err = worker.PrepareWorkspaceReader(lifecycle)
	} else {
		err = fmt.Errorf("workspace reader mode is required")
	}
	if err != nil {
		// Ошибка boundary закрытая: task bytes, filesystem paths и private metadata не печатаются.
		_, _ = fmt.Fprintln(os.Stderr, "session workspace reader failed")
		os.Exit(1)
	}
}
