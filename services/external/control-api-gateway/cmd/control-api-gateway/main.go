package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/app"
)

// version defaults to the development build label and may be replaced by the build linker; it is passed to app telemetry rather than used as an authorization or compatibility decision.
// version по умолчанию задаёт development build label и может заменяться build linker; передаётся app telemetry, а не используется как authorization и compatibility decision.
var version = "dev"

// main creates the process root and signal-cancelled lifecycle, passes both to app.Run and writes an English failure diagnostic before exiting with status one on error. os.Exit skips the deferred signal cleanup; successful return runs it normally.
// main создаёт process root и отменяемый сигналами lifecycle, передаёт оба app.Run и при ошибке пишет English failure diagnostic до выхода со status one. os.Exit пропускает отложенный signal cleanup; успешный возврат выполняет его штатно.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(root, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(lifecycle, root, version); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "control-api-gateway failed: %v\n", err)
		os.Exit(1)
	}
}
