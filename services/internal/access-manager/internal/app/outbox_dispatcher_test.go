package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
