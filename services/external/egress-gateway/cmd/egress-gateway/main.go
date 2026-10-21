package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/app"
)

// version identifies the built gateway release in startup diagnostics.
// version обозначает версию собранного шлюза в диагностике запуска.
var version = "dev"

// main owns root contexts, termination signals, and the gateway lifecycle.
// main владеет корневыми контекстами, сигналами завершения и жизненным циклом шлюза.
func main() {
	log.SetPrefix("egress-gateway " + version + ": ")
	base := context.Background()
	lifecycle, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(lifecycle, base, version); err != nil {
		log.Print("egress gateway runtime stopped with failure")
		os.Exit(1)
	}
}
