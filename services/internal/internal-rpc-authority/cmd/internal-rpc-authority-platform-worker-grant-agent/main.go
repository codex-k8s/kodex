package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/platformworkergrant"
)

// main gives the platform-worker-grant agent a signal-cancelled lifecycle and separate root context for cleanup, then prints a fixed-prefix diagnostic and exits nonzero if Run fails. Grant issuance and lifecycle remain in platformworkergrant.Run.
// main передаёт агенту полномочий platform worker отменяемый сигналом жизненный цикл и отдельный корневой контекст очистки, затем при ошибке Run выводит диагностику с фиксированным префиксом и завершается ненулевым кодом. Выдача полномочий и жизненный цикл остаются в platformworkergrant.Run.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(root, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := platformworkergrant.Run(lifecycle, root); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "platform worker grant agent failed: %v\n", err)
		os.Exit(1)
	}
}
