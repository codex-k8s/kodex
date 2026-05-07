package outbox

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
)

func TestDispatcherPublishesClaimedEvents(t *testing.T) {
	t.Parallel()

	event := testEvent(1)
	store := &fakeStore{claimed: []Event{event}}
	publisher := &fakePublisher{}
	dispatcher := NewDispatcher(store, publisher, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service")

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

func TestDispatcherSchedulesRetryAfterPublishFailure(t *testing.T) {
	t.Parallel()

	event := testEvent(2)
	store := &fakeStore{claimed: []Event{event}}
	publisher := &fakePublisher{err: errors.New(strings.Repeat("x", 32))}
	cfg := testConfig()
	cfg.FailureMessageLimit = 8
	dispatcher := NewDispatcher(store, publisher, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service")

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

func TestDispatcherStopsRetryAfterPermanentPublishFailure(t *testing.T) {
	t.Parallel()

	event := testEvent(1)
	store := &fakeStore{claimed: []Event{event}}
	publisher := &fakePublisher{err: fmt.Errorf("%w: invalid schema", ErrPermanentPublish)}
	dispatcher := NewDispatcher(store, publisher, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service")

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

func TestDispatcherDoesNotSwallowStoreErrors(t *testing.T) {
	t.Parallel()

	store := &fakeStore{claimErr: errors.New("database unavailable")}
	dispatcher := NewDispatcher(store, &fakePublisher{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service")

	if err := dispatcher.dispatchOnce(context.Background()); err == nil {
		t.Fatal("dispatchOnce() err = nil, want store error")
	}
}

func TestConfigFromRuntimeValuesMapsAllFields(t *testing.T) {
	t.Parallel()

	cfg := ConfigFromRuntimeValues(11, 2*time.Second, 3*time.Second, 4*time.Second, 5*time.Second, 6*time.Second, 777)
	if cfg.BatchSize != 11 ||
		cfg.PollInterval != 2*time.Second ||
		cfg.LockTTL != 3*time.Second ||
		cfg.PublishTimeout != 4*time.Second ||
		cfg.RetryInitialDelay != 5*time.Second ||
		cfg.RetryMaxDelay != 6*time.Second ||
		cfg.FailureMessageLimit != 777 {
		t.Fatalf("ConfigFromRuntimeValues() = %+v", cfg)
	}
}

func testConfig() Config {
	return Config{
		BatchSize:           10,
		PollInterval:        time.Millisecond,
		LockTTL:             time.Minute,
		PublishTimeout:      time.Second,
		RetryInitialDelay:   time.Second,
		RetryMaxDelay:       time.Minute,
		FailureMessageLimit: 256,
	}
}

func testEvent(attemptCount int) Event {
	return Event{
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

type fakeStore struct {
	claimed  []Event
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

func (s *fakeStore) ClaimOutboxEvents(_ context.Context, _ int, _ time.Time, _ time.Time) ([]Event, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.claimed, nil
}

func (s *fakeStore) MarkOutboxEventPublished(_ context.Context, id uuid.UUID, attemptCount int, _ time.Time) error {
	s.publishedID = id
	s.publishedAttempt = attemptCount
	return nil
}

func (s *fakeStore) MarkOutboxEventFailed(_ context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	s.failedID = id
	s.failedAttempt = attemptCount
	s.nextAttemptAt = nextAttemptAt
	s.lastError = lastError
	return nil
}

func (s *fakeStore) MarkOutboxEventPermanentlyFailed(_ context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	s.failedPermanentID = id
	s.failedPermanentAttempt = attemptCount
	s.failedPermanentAt = failedAt
	s.lastError = lastError
	return nil
}

type fakePublisher struct {
	events []Event
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, event Event) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}
