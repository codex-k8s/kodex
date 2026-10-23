// Пакет main запускает исполняемый владелец фактического PostgreSQL PITR.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/app"
)

// version carries the PITR executor binary's build label, defaulting to dev, and is passed to app.RunRestorePITR for application composition; it is not an authority snapshot revision.
// version хранит метку сборки бинаря PITR executor с исходным значением dev и передаётся в app.RunRestorePITR для сборки приложения; это не ревизия снимка полномочий.
var version = "dev"

// main starts the PITR application with signal cancellation, an independent root and the build version; noncancellation failures are logged before nonzero exit. Actual PostgreSQL recovery belongs to app.RunRestorePITR, not to this wrapper.
// main запускает приложение PITR с отменой по сигналу, независимым корнем и версией сборки; ошибки, кроме отмены, журналируются перед ненулевым выходом. Фактическое восстановление PostgreSQL принадлежит app.RunRestorePITR, а не обёртке.
func main() {
	root := context.Background()
	ctx, stop := signal.NotifyContext(
		root,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := app.RunRestorePITR(ctx, root, version); err != nil &&
		!errors.Is(err, context.Canceled) {
		slog.Error("PITR executor failed", "error", err)
		os.Exit(1)
	}
}
