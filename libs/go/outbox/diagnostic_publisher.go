package outbox

import (
	"context"
	"log/slog"
	"strings"
)

// DiagnosticPublisher logs events and intentionally loses durable delivery semantics.
type DiagnosticPublisher struct {
	logger      *slog.Logger
	serviceName string
}

// NewDiagnosticPublisher creates a lossy publisher for local one-off diagnostics.
func NewDiagnosticPublisher(logger *slog.Logger, serviceName string) DiagnosticPublisher {
	return DiagnosticPublisher{logger: logger, serviceName: strings.TrimSpace(serviceName)}
}

// Publish writes the event identity to logs and returns success.
func (p DiagnosticPublisher) Publish(_ context.Context, event Event) error {
	p.logger.Info(
		p.logMessage("outbox event delivered to diagnostic log sink"),
		"event_id", event.ID.String(),
		"event_type", event.EventType,
		"aggregate_type", event.AggregateType,
		"aggregate_id", event.AggregateID.String(),
	)
	return nil
}

func (p DiagnosticPublisher) logMessage(text string) string {
	if p.serviceName == "" {
		return text
	}
	return p.serviceName + " " + text
}
