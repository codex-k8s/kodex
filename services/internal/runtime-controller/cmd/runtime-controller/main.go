package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/app"
)

var buildVersion = "dev"

func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownBase, cancel := context.WithTimeout(context.WithoutCancel(lifecycle), 30*time.Second)
	defer cancel()
	if err := app.Run(lifecycle, shutdownBase, buildVersion); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("runtime-controller stopped: %v", err)
		os.Exit(1)
	}
}
