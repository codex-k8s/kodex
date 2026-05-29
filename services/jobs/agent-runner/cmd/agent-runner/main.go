package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/app"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/clients/agentmanager"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != app.CommandRun {
		_, _ = fmt.Fprintln(os.Stderr, "unsupported agent-runner command")
		os.Exit(app.ExitUsage)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := app.LoadConfig()
	if err != nil {
		diagnostic := app.NewDiagnostic(app.CodeInvalidConfiguration, "agent-runner configuration is invalid", app.ExitFailure)
		logger.Error("agent-runner failed", diagnostic.LogAttrs()...)
		os.Exit(diagnostic.ExitCode)
	}
	reporter, closeReporter, err := agentmanager.NewReporterFromConfig(cfg.AgentManager)
	if err != nil {
		diagnostic := app.NewDiagnostic(app.CodeInvalidConfiguration, "agent-manager reporting configuration is invalid", app.ExitFailure)
		logger.Error("agent-runner failed", diagnostic.LogAttrs()...)
		os.Exit(diagnostic.ExitCode)
	}
	defer closeReporter()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner := app.NewRunner(reporter, logger)
	diagnostic := runner.Run(ctx, cfg)
	if diagnostic.OK() {
		logger.Info("agent-runner completed", diagnostic.LogAttrs()...)
		os.Exit(app.ExitOK)
	}
	logger.Error("agent-runner failed", diagnostic.LogAttrs()...)
	os.Exit(diagnostic.ExitCode)
}
