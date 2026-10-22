package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/app"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
)

// version carries the build label passed into service telemetry and defaults to dev unless supplied at link time.
// version хранит метку сборки для телеметрии сервиса; без задания при компоновке используется dev.
var version = "dev"

// main creates the process root context, delegates execution and writes a returned failure to stderr before exiting with status one.
// Service composition and cleanup remain in the delegated application path.
// main создаёт корневой контекст процесса, передаёт выполнение и при ошибке выводит её в stderr перед выходом с кодом один.
// Сборка сервиса и очистка остаются в вызываемом прикладном пути.
func main() {
	backgroundCtx := context.Background()
	if err := run(backgroundCtx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run accepts either no arguments for signal-controlled service startup or the exact artifact-spool preparation command.
// Invalid argument shapes fail before starting the service; SIGINT/SIGTERM cancel lifecycle without canceling the separate shutdown base.
// run принимает отсутствие аргументов для запуска сервиса с сигналами либо точную команду подготовки хранилища вложений.
// Неверный набор аргументов отклоняется до запуска сервиса; SIGINT/SIGTERM отменяют жизненный цикл, не отменяя отдельную базу очистки.
func run(backgroundCtx context.Context) error {
	if len(os.Args) > 1 {
		if len(os.Args) == 3 && os.Args[1] == "--prepare-artifact-spool" {
			return callback.PrepareArtifactSpool(os.Args[2])
		}
		return fmt.Errorf("runtime controller arguments are invalid")
	}
	lifecycleCtx, stop := signal.NotifyContext(
		backgroundCtx,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	return app.Run(lifecycleCtx, backgroundCtx, version)
}
