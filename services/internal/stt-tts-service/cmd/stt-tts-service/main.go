package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/app"
)

var version = "dev"

func main() {
	background := context.Background()
	if err := run(background); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(background context.Context) error {
	lifecycle, stop := signal.NotifyContext(background, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(lifecycle, background, version)
}
