package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/app"
)

// version supplies the link-time build identifier to telemetry; local builds default to dev.
// RU: version передаёт telemetry идентификатор сборки из компоновщика; локальная сборка по умолчанию использует dev.
var version = "dev"

// main owns signal cancellation and an independent shutdown base, reporting only a safe process-level failure before exit.
// RU: main владеет отменой по сигналу и независимой базой shutdown, выводя перед выходом только безопасное сообщение об отказе процесса.
func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(lifecycle, context.Background(), version); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(os.Stderr, "interaction-gateway stopped with an error")
		os.Exit(1)
	}
}
