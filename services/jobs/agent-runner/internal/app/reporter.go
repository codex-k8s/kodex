package app

import (
	"context"
	"time"
)

type ReportInput struct {
	Config     Config
	Context    AgentRunContext
	StartedAt  time.Time
	FinishedAt time.Time
}

type Reporter interface {
	ReportStarted(context.Context, ReportInput) error
	ReportFailed(context.Context, ReportInput, Diagnostic) error
}

type NoopReporter struct{}

func (NoopReporter) ReportStarted(context.Context, ReportInput) error {
	return nil
}

func (NoopReporter) ReportFailed(context.Context, ReportInput, Diagnostic) error {
	return nil
}
