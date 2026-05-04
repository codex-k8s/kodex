package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
)

type eventLogAppender interface {
	Append(ctx context.Context, params eventlog.AppendParams) error
}

type postgresEventLogPublisher struct {
	sourceService string
	eventLog      eventLogAppender
}

func (p postgresEventLogPublisher) Publish(ctx context.Context, event entity.OutboxEvent) error {
	if p.eventLog == nil {
		return fmt.Errorf("%w: event log store is not configured", errOutboxPermanentPublish)
	}
	if strings.TrimSpace(p.sourceService) == "" {
		return fmt.Errorf("%w: event log source is not configured", errOutboxPermanentPublish)
	}
	err := p.eventLog.Append(ctx, eventlog.AppendParams{
		Event: eventlog.Event{
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
	if errors.Is(err, eventlog.ErrInvalidEvent) || errors.Is(err, eventlog.ErrEventConflict) {
		return fmt.Errorf("%w: %w", errOutboxPermanentPublish, err)
	}
	return err
}
