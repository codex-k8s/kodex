package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/app"
)

// version is the build-injected service version, defaulting to dev; it is telemetry metadata, not a
// release-admission decision.
// version — внедряемая при сборке версия сервиса с dev по умолчанию; это telemetry metadata, не решение о
// допуске выпуска.
var version = "dev"

// main creates the process base context, reports the returned startup/runtime error unchanged to stderr
// and exits 1 on failure; it does not sanitize arbitrary diagnostics.
// main создаёт базовый context процесса, без изменений выводит возвращённую startup/runtime error в stderr
// и завершает процесс с кодом 1; произвольные diagnostics не очищает.
func main() {
	background := context.Background()
	if err := run(background); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run derives a SIGINT/SIGTERM lifecycle context while retaining the supplied base for shutdown, then
// delegates composition and execution to app.Run.
// run создаёт lifecycle context для SIGINT/SIGTERM, сохраняя переданную базу для shutdown, затем
// делегирует сборку и исполнение app.Run.
func run(background context.Context) error {
	lifecycle, stop := signal.NotifyContext(background, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(lifecycle, background, version)
}
