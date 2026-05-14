package app

import (
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
)

func outboxEvent(event entity.OutboxEvent) outboxlib.Event {
	return event.Event
}
