package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/app"
)

// version identifies the controller build passed to application initialization;
// development builds retain this fallback unless the linker supplies a value.
// version передаёт идентификатор сборки контроллера при инициализации приложения;
// локальная сборка сохраняет это значение, если линкер не задаёт другое.
var version = "dev"

// main passes a signal-cancelled lifecycle and a separate noncancelled base context
// to the admission controller, preserving its shutdown budget after SIGINT/SIGTERM.
// Application errors are printed to stderr and terminate the process unsuccessfully.
// main передаёт контроллеру допуска жизненный контекст с отменой по сигналам
// и отдельный неотменённый базовый контекст для завершения после SIGINT/SIGTERM.
// Ошибки приложения выводятся в stderr и завершают процесс с ненулевым кодом.
func main() {
	base := context.Background()
	lifecycle, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.RunAdmissionController(lifecycle, base, version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
