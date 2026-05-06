package eventlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/outbox"
)

// Appender is the minimal event-log write contract needed by PostgresPublisher.
type Appender interface {
	Append(ctx context.Context, params AppendParams) error
}

// PostgresPublisher publishes service outbox events into platform-event-log.
type PostgresPublisher struct {
	sourceService string
	eventLog      Appender
}

// NewPostgresPublisher creates a publisher for the shared PostgreSQL event log.
func NewPostgresPublisher(sourceService string, eventLog Appender) PostgresPublisher {
	return PostgresPublisher{sourceService: strings.TrimSpace(sourceService), eventLog: eventLog}
}

// Publish writes one service outbox event into platform-event-log.
func (p PostgresPublisher) Publish(ctx context.Context, event outbox.Event) error {
	if p.eventLog == nil {
		return fmt.Errorf("%w: event log store is not configured", outbox.ErrPermanentPublish)
	}
	if p.sourceService == "" {
		return fmt.Errorf("%w: event log source is not configured", outbox.ErrPermanentPublish)
	}
	err := p.eventLog.Append(ctx, AppendParams{
		Event: Event{
			ID:            event.ID,
			SourceService: p.sourceService,
			EventType:     event.EventType,
			SchemaVersion: event.SchemaVersion,
			AggregateType: event.AggregateType,
			AggregateID:   event.AggregateID,
			Payload:       event.Payload,
			OccurredAt:    event.OccurredAt,
		},
		RecordedAt: time.Now().UTC(),
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidEvent) || errors.Is(err, ErrEventConflict) {
		return fmt.Errorf("%w: %w", outbox.ErrPermanentPublish, err)
	}
	return err
}
