package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/app"
)

// buildVersion is the build-time version injected by the release linker; local builds retain the inert dev label.
// buildVersion — версия сборки, задаваемая release linker; локальные сборки сохраняют неактивную метку dev.
var buildVersion = "dev"

// main owns the process root context and signal-derived lifecycle, passes both to composition and reports failures on stderr with exit status one. os.Exit bypasses deferred signal cleanup on that failure path.
// main владеет корневым контекстом процесса и signal-derived lifecycle, передаёт оба в composition и сообщает ошибки в stderr с кодом один. На пути ошибки os.Exit пропускает отложенную очистку сигналов.
func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(base, ctx, os.Args, buildVersion); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner execution failed: %v\n", err)
		os.Exit(1)
	}
}
