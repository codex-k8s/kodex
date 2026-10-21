package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/app"
)

// version is replaced by the build with the running controller release identifier.
// RU: version заменяется при сборке идентификатором запущенного выпуска контроллера.
var version = "dev"

// main owns the root context and exits nonzero when the selected command fails.
// RU: main владеет корневым контекстом и завершает процесс с ошибкой при отказе выбранной команды.
func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run accepts at most one command and binds its lifecycle to process termination signals.
// RU: run принимает не более одной команды и связывает её жизненный цикл с сигналами завершения процесса.
func run(backgroundCtx context.Context) error {
	command := "serve"
	if len(os.Args) > 2 {
		return fmt.Errorf("backup-controller accepts at most one command")
	}
	if len(os.Args) == 2 {
		command = os.Args[1]
	}
	lifecycleCtx, stop := signal.NotifyContext(backgroundCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, command, version)
}
