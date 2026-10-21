package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/app"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/worker"
)

// version is replaced at build time with the archive binary's release identifier.
// RU: version заменяется при сборке идентификатором выпуска бинарного файла архива.
var version = "dev"

// main owns root and signal contexts and dispatches the explicit controller, worker or restore-preparation mode.
// RU: main владеет корневым и сигнальным контекстами и выбирает явный режим контроллера, worker либо подготовки восстановления.
func main() {
	background := context.Background()
	lifecycle, stop := signal.NotifyContext(background, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) == 2 && os.Args[1] == "worker" {
		err = worker.Run(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "prepare-restore" {
		err = worker.PrepareRestore(lifecycle)
	} else if len(os.Args) == 2 && os.Args[1] == "controller" {
		err = app.Run(lifecycle, background, version)
	} else {
		err = fmt.Errorf("session-archive mode is required")
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
