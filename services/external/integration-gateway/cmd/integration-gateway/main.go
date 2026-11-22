package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/app"
)

// version carries the linker-selected gateway build identifier, defaulting to dev for a local binary.
// version хранит выбранный линкером идентификатор сборки шлюза с локальным умолчанием dev.
var version = "dev"

// main owns the gateway's signal-cancelled root context and independent shutdown base, reporting only a fixed fatal diagnostic.
// Only a nil application result exits normally; the app normalizes positively joined cancellation, while any remaining error produces exit one without leaking details.
// main владеет отменяемым сигналами корневым контекстом шлюза и независимой базой остановки и выдаёт только фиксированную критическую диагностику.
// Лишь nil result приложения завершает процесс штатно; app нормализует positively joined cancellation, а оставшаяся ошибка даёт exit one без раскрытия подробностей.
func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err := app.Run(lifecycle, context.Background(), version)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "integration-gateway stopped with an error")
		os.Exit(1)
	}
}
