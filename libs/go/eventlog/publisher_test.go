package eventlog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/outbox"
)

func TestPostgresPublisherAppendsOutboxEvent(t *testing.T) {
	t.Parallel()

	event := testOutboxEvent(1)
	appender := &fakeAppender{}
	publisher := NewPostgresPublisher("access-manager", appender)

	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish(): %v", err)
	}
	if appender.event.ID != event.ID || appender.event.EventType != event.EventType {
		t.Fatalf("appended event = %#v, want outbox event %s", appender.event, event.ID)
	}
	if appender.event.SourceService != "access-manager" {
		t.Fatalf("source service = %q, want access-manager", appender.event.SourceService)
	}
}

func TestPostgresPublisherMapsInvalidEventToPermanentFailure(t *testing.T) {
	t.Parallel()

	appender := &fakeAppender{err: ErrInvalidEvent}
	publisher := NewPostgresPublisher("access-manager", appender)

	err := publisher.Publish(context.Background(), testOutboxEvent(1))
	if !errors.Is(err, outbox.ErrPermanentPublish) {
		t.Fatalf("Publish() err = %v, want permanent failure", err)
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("Publish() err = %v, want invalid event cause", err)
	}
}

func TestPostgresPublisherMapsEventConflictToPermanentFailure(t *testing.T) {
	t.Parallel()

	appender := &fakeAppender{err: ErrEventConflict}
	publisher := NewPostgresPublisher("access-manager", appender)

	err := publisher.Publish(context.Background(), testOutboxEvent(1))
	if !errors.Is(err, outbox.ErrPermanentPublish) {
		t.Fatalf("Publish() err = %v, want permanent failure", err)
	}
	if !errors.Is(err, ErrEventConflict) {
		t.Fatalf("Publish() err = %v, want event conflict cause", err)
	}
}

func testOutboxEvent(attemptCount int) outbox.Event {
	return outbox.Event{
		ID:            uuid.New(),
		EventType:     "access.organization.created",
		SchemaVersion: 1,
		AggregateType: "organization",
		AggregateID:   uuid.New(),
		Payload:       []byte(`{}`),
		OccurredAt:    time.Now().UTC(),
		AttemptCount:  attemptCount,
	}
}

type fakeAppender struct {
	event Event
	err   error
}

func (a *fakeAppender) Append(_ context.Context, params AppendParams) error {
	if a.err != nil {
		return a.err
	}
	a.event = params.Event
	return nil
}
