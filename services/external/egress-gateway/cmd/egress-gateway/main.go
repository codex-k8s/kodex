package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/app"
)

var version = "dev"

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
