package service

import (
	"context"
	"errors"
	"testing"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
)

func TestDisabledEventPublisherRejectsPublication(t *testing.T) {
	t.Parallel()

	err := DisabledEventPublisher{}.Publish(context.Background(), outboxlib.Event{})
	if !errors.Is(err, ErrOutboxStorageNotConfigured) {
		t.Fatalf("Publish() err = %v, want ErrOutboxStorageNotConfigured", err)
	}
}

func TestServiceDefaultsToDisabledEventPublisher(t *testing.T) {
	t.Parallel()

	agentService := New(Config{})
	if !agentService.Ready() {
		t.Fatal("Ready() = false, want true")
	}
	err := agentService.EventPublisher().Publish(context.Background(), outboxlib.Event{})
	if !errors.Is(err, ErrOutboxStorageNotConfigured) {
		t.Fatalf("Publish() err = %v, want ErrOutboxStorageNotConfigured", err)
	}
}
