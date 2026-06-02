package app

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type Runner struct {
	reporter Reporter
	logger   *slog.Logger
	clock    Clock
	executor CodexExecutor
}

func NewRunner(reporter Reporter, logger *slog.Logger) Runner {
	if reporter == nil {
		reporter = NoopReporter{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return Runner{reporter: reporter, logger: logger, clock: systemClock{}, executor: NewCodexCLIExecutor()}
}

func NewRunnerWithClock(reporter Reporter, logger *slog.Logger, clock Clock) Runner {
	runner := NewRunner(reporter, logger)
	if clock != nil {
		runner.clock = clock
	}
	return runner
}

func NewRunnerWithExecutor(reporter Reporter, logger *slog.Logger, clock Clock, executor CodexExecutor) Runner {
	runner := NewRunnerWithClock(reporter, logger, clock)
	if executor != nil {
		runner.executor = executor
	}
	return runner
}

func (r Runner) Run(ctx context.Context, cfg Config) Diagnostic {
	normalized, diagnostic := cfg.Normalize()
	if !diagnostic.OK() {
		return diagnostic
	}
	contextFile, diagnostic := LoadContext(normalized)
	if !diagnostic.OK() {
		return r.reportFailure(ctx, normalized, AgentRunContext{}, diagnostic)
	}
	spec, diagnostic := ValidateCodexSessionExecutionSpec(normalized, contextFile)
	if !diagnostic.OK() {
		return r.reportFailure(ctx, normalized, contextFile, diagnostic)
	}
	executionInput, diagnostic := LoadCodexExecutionInput(normalized, *spec)
	if !diagnostic.OK() {
		return r.reportFailure(ctx, normalized, contextFile, diagnostic)
	}
	startedAt := r.clock.Now()
	report := ReportInput{Config: normalized, Context: contextFile, StartedAt: startedAt}
	if err := r.reporter.ReportStarted(ctx, report); err != nil {
		if errors.Is(err, context.Canceled) {
			return NewDiagnostic("agent_runner_cancelled", "agent-runner was cancelled", ExitFailure)
		}
		return NewDiagnostic("agent_manager_report_failed", "agent-runner could not report start to agent-manager", ExitFailure)
	}
	result, diagnostic := r.executor.Execute(ctx, CodexExecutionRequest{
		Config:  normalized,
		Context: contextFile,
		Spec:    *spec,
		Input:   executionInput,
	})
	if !diagnostic.OK() {
		report.FinishedAt = r.clock.Now()
		return r.reportFailure(ctx, normalized, contextFile, diagnostic)
	}
	report.FinishedAt = r.clock.Now()
	if err := r.reporter.ReportCompleted(ctx, report, result); err != nil {
		if errors.Is(err, context.Canceled) {
			return NewDiagnostic("agent_runner_cancelled", "agent-runner was cancelled", ExitFailure)
		}
		return NewDiagnostic("agent_manager_report_failed", "agent-runner could not report completion to agent-manager", ExitFailure)
	}
	return OKDiagnostic()
}

func (r Runner) reportFailure(ctx context.Context, cfg Config, contextFile AgentRunContext, diagnostic Diagnostic) Diagnostic {
	report := ReportInput{Config: cfg, Context: contextFile, FinishedAt: r.clock.Now()}
	if err := r.reporter.ReportFailed(ctx, report, diagnostic); err != nil {
		if errors.Is(err, context.Canceled) {
			return NewDiagnostic("agent_runner_cancelled", "agent-runner was cancelled", ExitFailure)
		}
		r.logger.Warn("agent-runner safe failure report failed", slog.String("error_code", "agent_manager_report_failed"))
		return NewDiagnostic("agent_manager_report_failed", "agent-runner could not report failure to agent-manager", ExitFailure)
	}
	return diagnostic
}
