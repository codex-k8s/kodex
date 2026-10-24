package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/app"
)

// version holds the build-supplied control-plane version, defaulting to dev for an unversioned
// local binary.
// version хранит переданную при сборке версию control-plane, используя dev для локального бинаря
// без версии.
var version = "dev"

// main creates the process root and signal-cancelled lifecycle, passes both to app.Run and exits
// nonzero if the application returns an error.
// main создаёт корневой контекст процесса и отменяемый сигналами lifecycle, передаёт оба в app.Run
// и завершает процесс с ненулевым кодом при ошибке приложения.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.Run(lifecycle, root, version); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "control-plane failed: %v\n", err)
		os.Exit(1)
	}
}
