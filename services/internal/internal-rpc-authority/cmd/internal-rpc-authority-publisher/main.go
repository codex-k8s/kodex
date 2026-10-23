package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
)

// version carries the publisher binary's build label, defaulting to dev, and is passed to app.RunPublisher for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря publisher с исходным значением dev и передаётся в app.RunPublisher для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main supplies a signal-cancelled lifecycle, uncancelled root and build version to the publisher runtime, printing a publisher-specific error before nonzero exit on failure. Publication and bounded cleanup are delegated to the application, not performed by this entry point.
// main передаёт отменяемый сигналом жизненный цикл, неотменённый корень и версию сборки runtime издателя, выводя отдельную ошибку publisher перед ненулевым выходом при отказе. Публикация и ограниченная очистка переданы приложению, а не выполняются точкой входа.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunPublisher(lifecycle, root, version); err != nil {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"internal-rpc-authority publisher failed: %v\n",
			err,
		)
		os.Exit(1)
	}
}
