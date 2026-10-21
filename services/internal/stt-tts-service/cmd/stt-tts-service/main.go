package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/app"
)

// version supplies the linker-assigned build version to service telemetry, defaulting to dev for local builds.
// RU: version передаёт telemetry сервиса версию сборки из компоновщика, используя dev по умолчанию для локальной сборки.
var version = "dev"

// main creates the process root context and reports the returned startup or lifecycle failure before a nonzero exit.
// RU: main создаёт корневой контекст процесса и сообщает возвращённый отказ startup или жизненного цикла перед ненулевым выходом.
func main() {
	background := context.Background()
	if err := run(background); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run derives signal cancellation from the supplied root while preserving that root as the independent shutdown base.
// RU: run выводит отмену по сигналу из переданного корня, сохраняя тот же корень независимой базой shutdown.
func run(background context.Context) error {
	lifecycle, stop := signal.NotifyContext(background, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(lifecycle, background, version)
}
