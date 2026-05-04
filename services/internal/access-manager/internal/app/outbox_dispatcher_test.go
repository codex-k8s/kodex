package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
)

func TestOutboxDispatcherPublishesClaimedEvents(t *testing.T) {
	t.Parallel()

	event := testOutboxEvent(1)
	store := &fakeOutboxStore{claimed: []entity.OutboxEvent{event}}
	publisher := &fakeOutboxPublisher{}
	dispatcher := newOutboxDispatcher(store, publisher, testOutboxDispatcherConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := dispatcher.dispatchOnce(context.Background()); err != nil {
		t.Fatalf("dispatchOnce(): %v", err)
	}
	if len(publisher.events) != 1 || publisher.events[0].ID != event.ID {
		t.Fatalf("published events = %#v, want event %s", publisher.events, event.ID)
	}
	if store.publishedID != event.ID || store.publishedAttempt != event.AttemptCount {
		t.Fatalf("published mark = %s/%d, want %s/%d", store.publishedID, store.publishedAttempt, event.ID, event.AttemptCount)
	}
	if store.failedID != uuid.Nil {
		t.Fatalf("failed mark = %s, want empty", store.failedID)
	}
}

func TestOutboxDispatcherSchedulesRetryAfterPublishFailure(t *testing.T) {
	t.Parallel()

	event := testOutboxEvent(2)
	store := &fakeOutboxStore{claimed: []entity.OutboxEvent{event}}
	publisher := &fakeOutboxPublisher{err: errors.New(strings.Repeat("x", 32))}
	cfg := testOutboxDispatcherConfig()
	cfg.FailureMessageLimit = 8
	dispatcher := newOutboxDispatcher(store, publisher, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	started := time.Now().UTC()
	if err := dispatcher.dispatchOnce(context.Background()); err != nil {
		t.Fatalf("dispatchOnce(): %v", err)
	}
	if store.publishedID != uuid.Nil {
		t.Fatalf("published mark = %s, want empty", store.publishedID)
	}
	if store.failedID != event.ID || store.failedAttempt != event.AttemptCount {
		t.Fatalf("failed mark = %s/%d, want %s/%d", store.failedID, store.failedAttempt, event.ID, event.AttemptCount)
	}
	if got := store.lastError; got != "xxxxxxxx" {
		t.Fatalf("lastError = %q, want truncated value", got)
	}
	minNextAttempt := started.Add(2 * cfg.RetryInitialDelay)
	if store.nextAttemptAt.Before(minNextAttempt) {
		t.Fatalf("nextAttemptAt = %s, want after %s", store.nextAttemptAt, minNextAttempt)
	}
}

func TestOutboxDispatcherStopsRetryAfterPermanentPublishFailure(t *testing.T) {
	t.Parallel()

	event := testOutboxEvent(1)
	store := &fakeOutboxStore{claimed: []entity.OutboxEvent{event}}
	publisher := &fakeOutboxPublisher{err: fmt.Errorf("%w: invalid schema", errOutboxPermanentPublish)}
	dispatcher := newOutboxDispatcher(store, publisher, testOutboxDispatcherConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := dispatcher.dispatchOnce(context.Background()); err != nil {
		t.Fatalf("dispatchOnce(): %v", err)
	}
	if store.failedPermanentID != event.ID || store.failedPermanentAttempt != event.AttemptCount {
		t.Fatalf("permanent failed mark = %s/%d, want %s/%d", store.failedPermanentID, store.failedPermanentAttempt, event.ID, event.AttemptCount)
	}
	if store.failedID != uuid.Nil {
		t.Fatalf("transient failed mark = %s, want empty", store.failedID)
	}
}

func TestOutboxDispatcherDoesNotSwallowStoreErrors(t *testing.T) {
	t.Parallel()

	store := &fakeOutboxStore{claimErr: errors.New("database unavailable")}
	dispatcher := newOutboxDispatcher(
		store,
		&fakeOutboxPublisher{},
		testOutboxDispatcherConfig(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	if err := dispatcher.dispatchOnce(context.Background()); err == nil {
		t.Fatal("dispatchOnce() err = nil, want store error")
	}
}

func TestPostgresEventLogPublisherAppendsOutboxEvent(t *testing.T) {
	t.Parallel()

	event := testOutboxEvent(1)
	appender := &fakeEventLogAppender{}
	publisher := postgresEventLogPublisher{sourceService: "access-manager", eventLog: appender}

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

func TestPostgresEventLogPublisherMapsInvalidEventToPermanentFailure(t *testing.T) {
	t.Parallel()

	appender := &fakeEventLogAppender{err: eventlog.ErrInvalidEvent}
	publisher := postgresEventLogPublisher{sourceService: "access-manager", eventLog: appender}

	err := publisher.Publish(context.Background(), testOutboxEvent(1))
	if !errors.Is(err, errOutboxPermanentPublish) {
		t.Fatalf("Publish() err = %v, want permanent failure", err)
	}
	if !errors.Is(err, eventlog.ErrInvalidEvent) {
		t.Fatalf("Publish() err = %v, want invalid event cause", err)
	}
}

func TestPostgresEventLogPublisherMapsEventConflictToPermanentFailure(t *testing.T) {
	t.Parallel()

	appender := &fakeEventLogAppender{err: eventlog.ErrEventConflict}
	publisher := postgresEventLogPublisher{sourceService: "access-manager", eventLog: appender}

	err := publisher.Publish(context.Background(), testOutboxEvent(1))
	if !errors.Is(err, errOutboxPermanentPublish) {
		t.Fatalf("Publish() err = %v, want permanent failure", err)
	}
	if !errors.Is(err, eventlog.ErrEventConflict) {
		t.Fatalf("Publish() err = %v, want event conflict cause", err)
	}
}

func testOutboxDispatcherConfig() outboxDispatcherConfig {
	return outboxDispatcherConfig{
		BatchSize:           10,
		PollInterval:        time.Millisecond,
		LockTTL:             time.Minute,
		PublishTimeout:      time.Second,
		RetryInitialDelay:   time.Second,
		RetryMaxDelay:       time.Minute,
		FailureMessageLimit: 256,
	}
}

func testOutboxEvent(attemptCount int) entity.OutboxEvent {
	return entity.OutboxEvent{
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

type fakeOutboxStore struct {
	claimed  []entity.OutboxEvent
	claimErr error

	publishedID      uuid.UUID
	publishedAttempt int

	failedID      uuid.UUID
	failedAttempt int
	nextAttemptAt time.Time
	lastError     string

	failedPermanentID      uuid.UUID
	failedPermanentAttempt int
	failedPermanentAt      time.Time
}

func (s *fakeOutboxStore) ClaimOutboxEvents(_ context.Context, _ int, _ time.Time, _ time.Time) ([]entity.OutboxEvent, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.claimed, nil
}

func (s *fakeOutboxStore) MarkOutboxEventPublished(_ context.Context, id uuid.UUID, attemptCount int, _ time.Time) error {
	s.publishedID = id
	s.publishedAttempt = attemptCount
	return nil
}

func (s *fakeOutboxStore) MarkOutboxEventFailed(_ context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	s.failedID = id
	s.failedAttempt = attemptCount
	s.nextAttemptAt = nextAttemptAt
	s.lastError = lastError
	return nil
}

func (s *fakeOutboxStore) MarkOutboxEventPermanentlyFailed(_ context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	s.failedPermanentID = id
	s.failedPermanentAttempt = attemptCount
	s.failedPermanentAt = failedAt
	s.lastError = lastError
	return nil
}

type fakeOutboxPublisher struct {
	events []entity.OutboxEvent
	err    error
}

func (p *fakeOutboxPublisher) Publish(_ context.Context, event entity.OutboxEvent) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}

type fakeEventLogAppender struct {
	event eventlog.Event
	err   error
}

func (a *fakeEventLogAppender) Append(_ context.Context, params eventlog.AppendParams) error {
	if a.err != nil {
		return a.err
	}
	a.event = params.Event
	return nil
}
