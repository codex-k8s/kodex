package main

import (
	"context"
	"errors"
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
// Successful or context-cancelled execution exits normally; other errors produce exit status one without leaking their details.
// main владеет отменяемым сигналами корневым контекстом шлюза и независимой базой остановки и выдаёт только фиксированную критическую диагностику.
// Успех и отмена контекста завершаются штатно; иные ошибки дают код выхода один без раскрытия подробностей.
func main() {
	lifecycle, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err := app.Run(lifecycle, context.Background(), version)
	if err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(os.Stderr, "integration-gateway stopped with an error")
		os.Exit(1)
	}
}
