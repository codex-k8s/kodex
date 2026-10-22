package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
)

// version carries the readback attestor binary's build label, defaulting to dev, and is passed to app.RunReadbackAttestor for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря readback attestor с исходным значением dev и передаётся в app.RunReadbackAttestor для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main passes a signal-cancelled lifecycle, uncancelled root and build version to the readback-attestor runtime. Errors produce a readback-specific diagnostic and nonzero exit; attestation ownership and shutdown ordering stay in the application.
// main передаёт отменяемый сигналом жизненный цикл, неотменённый корень и версию сборки runtime readback-attestor. Ошибки дают отдельную диагностику readback и ненулевой выход; владение аттестацией и порядок завершения остаются в приложении.
func main() {
	root := context.Background()
	lifecycle, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunReadbackAttestor(
		lifecycle,
		root,
		version,
	); err != nil {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"internal-rpc-authority readback attestor failed: %v\n",
			err,
		)
		os.Exit(1)
	}
}
