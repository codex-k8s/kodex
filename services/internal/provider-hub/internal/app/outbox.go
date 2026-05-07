package app

import (
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
)

type serviceOutboxStore = outboxlib.EntityStore[entity.OutboxEvent]

func outboxEvent(event entity.OutboxEvent) outboxlib.Event {
	return outboxlib.EventFromRecord(event)
}
