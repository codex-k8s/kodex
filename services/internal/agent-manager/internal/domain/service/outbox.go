package service

import (
	"context"
	"errors"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
)

// ErrOutboxStorageNotConfigured means agent-manager has no service-local outbox store yet.
var ErrOutboxStorageNotConfigured = errors.New("agent-manager outbox storage is not configured")

// EventPublisher publishes one agent-manager domain event through the configured outbox boundary.
type EventPublisher interface {
	Publish(ctx context.Context, event outboxlib.Event) error
}

// DisabledEventPublisher rejects event publication until persistent outbox storage is added.
type DisabledEventPublisher struct{}

// Publish returns a failed precondition sentinel instead of pretending that event delivery succeeded.
func (DisabledEventPublisher) Publish(context.Context, outboxlib.Event) error {
	return ErrOutboxStorageNotConfigured
}
