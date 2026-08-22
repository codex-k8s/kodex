package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/platformworkergrant"
)

func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(root, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := platformworkergrant.Run(lifecycle, root); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "platform worker grant agent failed: %v\n", err)
		os.Exit(1)
	}
}
