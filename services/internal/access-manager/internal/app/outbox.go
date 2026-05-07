package app

import (
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
)

func outboxEvent(event entity.OutboxEvent) outboxlib.Event {
	return outboxlib.NewEvent(
		event.ID,
		event.EventType,
		event.SchemaVersion,
		event.AggregateType,
		event.AggregateID,
		event.Payload,
		event.OccurredAt,
		event.AttemptCount,
	)
}
