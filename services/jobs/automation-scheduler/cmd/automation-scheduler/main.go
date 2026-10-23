package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/automation-scheduler/internal/app"
)

// version is the build-injected release identifier, with dev reserved for local binaries.
// RU: version задаёт идентификатор сборки, а dev используется для локального бинаря.
var version = "dev"

// main owns the only background context and reports a terminal process error to stderr.
// RU: main владеет единственным фоновым контекстом и сообщает конечную ошибку процесса в stderr.
func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run derives the signal-cancelled lifecycle context while retaining the uncancelled base
// for the app's independently bounded shutdown operations.
// RU: run создаёт отменяемый сигналом контекст работы и сохраняет неотменённую основу
// для отдельных ограниченных операций остановки приложения.
func run(backgroundCtx context.Context) error {
	lifecycleCtx, stop := signal.NotifyContext(
		backgroundCtx,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, version)
}
