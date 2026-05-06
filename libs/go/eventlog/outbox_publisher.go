package eventlog

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/outbox"
)

// NewOutboxPublisher selects the standard publisher for service outbox delivery.
func NewOutboxPublisher(kind string, allowLossy bool, sourceService string, appender Appender, logger *slog.Logger, serviceName string) (outbox.Publisher, error) {
	switch strings.TrimSpace(kind) {
	case outbox.PublisherKindPostgresEventLog:
		return NewPostgresPublisher(strings.TrimSpace(sourceService), appender), nil
	case outbox.PublisherKindDiagnosticLogLossy:
		if !allowLossy {
			return nil, fmt.Errorf("lossy diagnostic outbox publisher is not explicitly allowed")
		}
		return outbox.NewDiagnosticPublisher(logger, serviceName), nil
	case outbox.PublisherKindDisabled:
		return nil, fmt.Errorf("outbox publisher is disabled")
	default:
		return nil, fmt.Errorf("unsupported outbox publisher kind %q", kind)
	}
}
