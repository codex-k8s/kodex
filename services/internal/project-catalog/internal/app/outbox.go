package app

import (
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
)

type serviceOutboxStore = outboxlib.EntityStore[entity.OutboxEvent]

func outboxEvent(event entity.OutboxEvent) outboxlib.Event {
	return outboxlib.Event{
		ID:            event.ID,
		EventType:     event.EventType,
		SchemaVersion: event.SchemaVersion,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		Payload:       event.Payload,
		OccurredAt:    event.OccurredAt,
		AttemptCount:  event.AttemptCount,
	}
}
