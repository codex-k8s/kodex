package eventconsumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	"github.com/google/uuid"
)

func TestRunOnceAdvancesAfterAck(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		claims: []eventlog.ClaimedBatch{batchOf(eventOf(1, "provider.repository.bootstrap_merged", 1))},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(context.Context, Event) Result {
		return Ack()
	}))
	runner := newTestRunner(t, store, registry)

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(): %v", err)
	}
	if len(store.advances) != 1 {
		t.Fatalf("advance count = %d, want 1", len(store.advances))
	}
	if store.advances[0].LastSequenceID != 1 {
		t.Fatalf("advance sequence = %d, want 1", store.advances[0].LastSequenceID)
	}
	if len(store.releases) != 0 {
		t.Fatalf("release count = %d, want 0", len(store.releases))
	}
}

func TestRunOnceKeepsCheckpointBeforeRetry(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		claims: []eventlog.ClaimedBatch{batchOf(
			eventOf(10, "provider.repository.bootstrap_merged", 1),
			eventOf(11, "provider.repository.bootstrap_merged", 1),
		)},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(_ context.Context, event Event) Result {
		if event.StoredEvent.SequenceID == 10 {
			return Retry(errors.New("temporary downstream failure with diagnostic details"))
		}
		return Ack()
	}))
	runner := newTestRunner(t, store, registry)

	err := runner.RunOnce(context.Background())
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("RunOnce() err = %v, want retryable", err)
	}
	if len(store.advances) != 0 {
		t.Fatalf("advance count = %d, want 0", len(store.advances))
	}
	if len(store.defers) != 1 {
		t.Fatalf("defer count = %d, want 1", len(store.defers))
	}
	if len(store.releases) != 0 {
		t.Fatalf("release count = %d, want 0", len(store.releases))
	}
}

func TestRunOnceDefersWholeBatchBeforeRetry(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		claims: []eventlog.ClaimedBatch{batchOf(
			eventOf(20, "provider.repository.bootstrap_merged", 1),
			eventOf(21, "provider.repository.bootstrap_merged", 1),
		)},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(_ context.Context, event Event) Result {
		if event.StoredEvent.SequenceID == 21 {
			return Retry(errors.New("temporary failure"))
		}
		return Ack()
	}))
	runner := newTestRunner(t, store, registry)

	err := runner.RunOnce(context.Background())
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("RunOnce() err = %v, want retryable", err)
	}
	if len(store.advances) != 0 {
		t.Fatalf("advance count = %d, want 0", len(store.advances))
	}
	if len(store.defers) != 1 {
		t.Fatalf("defer count = %d, want 1", len(store.defers))
	}
	if len(store.releases) != 0 {
		t.Fatalf("release count = %d, want 0", len(store.releases))
	}
}

func TestRunOncePoisonsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	event := eventOf(30, "provider.repository.bootstrap_merged", 1)
	store := &fakeStore{
		claims: []eventlog.ClaimedBatch{batchOf(event), batchOf(event)},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(context.Context, Event) Result {
		return Retry(errors.New("still failing"))
	}))
	runner := newTestRunner(t, store, registry)
	runner.cfg.MaxAttempts = 2

	if err := runner.RunOnce(context.Background()); !errors.Is(err, ErrRetryable) {
		t.Fatalf("first RunOnce() err = %v, want retryable", err)
	}
	if len(store.defers) != 1 {
		t.Fatalf("defer count after first run = %d, want 1", len(store.defers))
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce(): %v", err)
	}
	if len(store.advances) != 1 {
		t.Fatalf("advance count = %d, want 1", len(store.advances))
	}
	if store.advances[0].LastSequenceID != 30 {
		t.Fatalf("advance sequence = %d, want 30", store.advances[0].LastSequenceID)
	}
}

func TestRunOnceRecoversHandlerPanic(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		claims: []eventlog.ClaimedBatch{batchOf(eventOf(35, "provider.repository.bootstrap_merged", 1))},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(context.Context, Event) Result {
		panic("raw panic detail must not leak")
	}))
	hook := &captureHook{}
	runner := newTestRunner(t, store, registry)
	runner.hook = hook

	err := runner.RunOnce(context.Background())
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("RunOnce() err = %v, want retryable", err)
	}
	if len(store.advances) != 0 {
		t.Fatalf("advance count = %d, want 0", len(store.advances))
	}
	if len(store.defers) != 1 {
		t.Fatalf("defer count = %d, want 1", len(store.defers))
	}
	if got := hook.statuses(); fmt.Sprint(got) != "[retry]" {
		t.Fatalf("statuses = %v, want [retry]", got)
	}
	if len(hook.handled) != 1 || hook.handled[0].Code != "handler_panic" || hook.handled[0].Summary != "event handler panicked" {
		t.Fatalf("handled = %+v, want safe handler_panic diagnostic", hook.handled)
	}
}

func TestRunOnceRetryDeferBlocksOtherLeaseOwner(t *testing.T) {
	t.Parallel()

	store := &leaseAwareStore{
		events: []eventlog.StoredEvent{eventOf(37, "provider.repository.bootstrap_merged", 1)},
	}
	var calls int
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(context.Context, Event) Result {
		calls++
		return Retry(errors.New("temporary failure"))
	}))
	first := newTestRunnerWithOwner(t, store, registry, "owner-1")
	first.cfg.RetryInitialDelay = time.Hour
	second := newTestRunnerWithOwner(t, store, registry, "owner-2")

	if err := first.RunOnce(context.Background()); !errors.Is(err, ErrRetryable) {
		t.Fatalf("first RunOnce() err = %v, want retryable", err)
	}
	if err := second.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce(): %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	if store.deferCount != 1 {
		t.Fatalf("defer count = %d, want 1", store.deferCount)
	}
}

func TestRunOnceMaxAttemptsSurvivesNewRunnerLeaseOwner(t *testing.T) {
	t.Parallel()

	event := eventOf(39, "provider.repository.bootstrap_merged", 1)
	store := &leaseAwareStore{
		events: []eventlog.StoredEvent{event},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(context.Context, Event) Result {
		return Retry(errors.New("permanent failure"))
	}))
	first := newTestRunnerWithOwner(t, store, registry, "owner-1")
	first.cfg.MaxAttempts = 2
	second := newTestRunnerWithOwner(t, store, registry, "owner-2")
	second.cfg.MaxAttempts = 2

	if err := first.RunOnce(context.Background()); !errors.Is(err, ErrRetryable) {
		t.Fatalf("first RunOnce() err = %v, want retryable", err)
	}
	store.lockedUntil = time.Now().Add(-time.Second)
	if err := second.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce(): %v", err)
	}
	if store.lastSequenceID != event.SequenceID {
		t.Fatalf("last sequence id = %d, want poison advance to %d", store.lastSequenceID, event.SequenceID)
	}
	if store.retrySequenceID != 0 || store.retryAttempt != 0 || store.lastError != "" {
		t.Fatalf("retry state = %d/%d/%q, want reset after poison advance", store.retrySequenceID, store.retryAttempt, store.lastError)
	}
}

func TestRunOnceSkipsUnknownAndPoisonsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		claims: []eventlog.ClaimedBatch{batchOf(
			eventOf(40, "other.event", 1),
			eventOf(41, "provider.repository.bootstrap_merged", 2),
		)},
	}
	registry := registryFor(t, "provider.repository.bootstrap_merged", 1, HandlerFunc(func(context.Context, Event) Result {
		t.Fatal("handler should not be called")
		return Ack()
	}))
	hook := &captureHook{}
	runner := newTestRunner(t, store, registry)
	runner.hook = hook
	runner.cfg.ConcurrencyLimit = 1

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(): %v", err)
	}
	if len(store.advances) != 1 {
		t.Fatalf("advance count = %d, want 1", len(store.advances))
	}
	if store.advances[0].LastSequenceID != 41 {
		t.Fatalf("advance sequence = %d, want 41", store.advances[0].LastSequenceID)
	}
	if got := hook.statuses(); fmt.Sprint(got) != "[ack poison]" {
		t.Fatalf("statuses = %v, want [ack poison]", got)
	}
}

func TestRegistryRejectsDuplicateHandler(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(
		Registration{EventType: "provider.repository.bootstrap_merged", SchemaVersion: 1, Handler: HandlerFunc(func(context.Context, Event) Result { return Ack() })},
		Registration{EventType: "provider.repository.bootstrap_merged", SchemaVersion: 1, Handler: HandlerFunc(func(context.Context, Event) Result { return Ack() })},
	)
	if !errors.Is(err, ErrDuplicateHandler) {
		t.Fatalf("NewRegistry() err = %v, want duplicate handler", err)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	t.Parallel()

	runner := newTestRunner(t, &fakeStore{}, registryFor(t, "event", 1, HandlerFunc(func(context.Context, Event) Result { return Ack() })))
	runner.cfg.RetryInitialDelay = time.Second
	runner.cfg.RetryMaxDelay = 5 * time.Second

	if got := runner.retryDelay(1); got != time.Second {
		t.Fatalf("retryDelay(1) = %s, want 1s", got)
	}
	if got := runner.retryDelay(4); got != 5*time.Second {
		t.Fatalf("retryDelay(4) = %s, want 5s", got)
	}
}

func newTestRunner(t *testing.T, store Store, registry Registry) *Runner {
	t.Helper()
	return newTestRunnerWithOwner(t, store, registry, "test-owner")
}

func newTestRunnerWithOwner(t *testing.T, store Store, registry Registry, leaseOwner string) *Runner {
	t.Helper()
	runner, err := NewRunner(store, registry, ConfigFromRuntimeValues(
		"test-consumer",
		leaseOwner,
		10,
		time.Millisecond,
		time.Minute,
		time.Minute,
		time.Millisecond,
		time.Second,
		128,
		2,
		3,
	), slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatalf("NewRunner(): %v", err)
	}
	return runner
}

func registryFor(t *testing.T, eventType string, schemaVersion int, handler Handler) Registry {
	t.Helper()
	registry, err := NewRegistry(Registration{EventType: eventType, SchemaVersion: schemaVersion, Handler: handler})
	if err != nil {
		t.Fatalf("NewRegistry(): %v", err)
	}
	return registry
}

func batchOf(events ...eventlog.StoredEvent) eventlog.ClaimedBatch {
	return eventlog.ClaimedBatch{
		ConsumerName: "test-consumer",
		LeaseOwner:   "test-owner",
		LockedUntil:  time.Now().Add(time.Minute),
		Events:       events,
	}
}

func eventOf(sequence int64, eventType string, schemaVersion int) eventlog.StoredEvent {
	return eventlog.StoredEvent{
		SequenceID: sequence,
		Event: eventlog.Event{
			ID:            uuid.New(),
			SourceService: "provider-hub",
			EventType:     eventType,
			SchemaVersion: schemaVersion,
			AggregateType: "repository_merge_signal",
			AggregateID:   uuid.New(),
			Payload:       []byte(`{"safe":true}`),
			OccurredAt:    time.Now().UTC(),
		},
		RecordedAt: time.Now().UTC(),
	}
}

type fakeStore struct {
	claims          []eventlog.ClaimedBatch
	advances        []eventlog.AdvanceParams
	defers          []eventlog.DeferParams
	releases        []eventlog.ReleaseParams
	retrySequenceID int64
	retryAttempt    int
	lastError       string
}

func (s *fakeStore) Claim(context.Context, eventlog.ClaimParams) (eventlog.ClaimedBatch, error) {
	if len(s.claims) == 0 {
		return eventlog.ClaimedBatch{ConsumerName: "test-consumer", LeaseOwner: "test-owner"}, nil
	}
	batch := s.claims[0]
	s.claims = s.claims[1:]
	if batch.RetrySequenceID == 0 {
		batch.RetrySequenceID = s.retrySequenceID
		batch.RetryAttempt = s.retryAttempt
		batch.LastError = s.lastError
	}
	return batch, nil
}

func (s *fakeStore) Advance(_ context.Context, params eventlog.AdvanceParams) error {
	s.advances = append(s.advances, params)
	s.retrySequenceID = 0
	s.retryAttempt = 0
	s.lastError = ""
	return nil
}

func (s *fakeStore) Defer(_ context.Context, params eventlog.DeferParams) error {
	s.defers = append(s.defers, params)
	s.retrySequenceID = params.RetrySequenceID
	s.retryAttempt = params.RetryAttempt
	s.lastError = params.LastError
	return nil
}

func (s *fakeStore) Release(_ context.Context, params eventlog.ReleaseParams) error {
	s.releases = append(s.releases, params)
	return nil
}

type leaseAwareStore struct {
	events          []eventlog.StoredEvent
	lastSequenceID  int64
	leaseOwner      string
	lockedUntil     time.Time
	deferCount      int
	retrySequenceID int64
	retryAttempt    int
	lastError       string
}

func (s *leaseAwareStore) Claim(_ context.Context, params eventlog.ClaimParams) (eventlog.ClaimedBatch, error) {
	if !s.lockedUntil.IsZero() && s.lockedUntil.After(params.Now) {
		return eventlog.ClaimedBatch{ConsumerName: params.ConsumerName, LeaseOwner: params.LeaseOwner}, nil
	}
	s.leaseOwner = params.LeaseOwner
	s.lockedUntil = params.LockedUntil
	events := make([]eventlog.StoredEvent, 0, len(s.events))
	for _, event := range s.events {
		if event.SequenceID > s.lastSequenceID {
			events = append(events, event)
		}
	}
	return eventlog.ClaimedBatch{
		ConsumerName:    params.ConsumerName,
		LeaseOwner:      params.LeaseOwner,
		LockedUntil:     params.LockedUntil,
		RetrySequenceID: s.retrySequenceID,
		RetryAttempt:    s.retryAttempt,
		LastError:       s.lastError,
		Events:          events,
	}, nil
}

func (s *leaseAwareStore) Advance(_ context.Context, params eventlog.AdvanceParams) error {
	s.lastSequenceID = params.LastSequenceID
	s.leaseOwner = ""
	s.lockedUntil = time.Time{}
	s.retrySequenceID = 0
	s.retryAttempt = 0
	s.lastError = ""
	return nil
}

func (s *leaseAwareStore) Defer(_ context.Context, params eventlog.DeferParams) error {
	if s.leaseOwner != params.LeaseOwner || s.lockedUntil.IsZero() || !s.lockedUntil.After(params.Now) {
		return eventlog.ErrCheckpointNotOwned
	}
	s.lockedUntil = params.LockedUntil
	s.retrySequenceID = params.RetrySequenceID
	s.retryAttempt = params.RetryAttempt
	s.lastError = params.LastError
	s.deferCount++
	return nil
}

func (s *leaseAwareStore) Release(_ context.Context, params eventlog.ReleaseParams) error {
	if s.leaseOwner != params.LeaseOwner || s.lockedUntil.IsZero() || !s.lockedUntil.After(params.Now) {
		return eventlog.ErrCheckpointNotOwned
	}
	s.leaseOwner = ""
	s.lockedUntil = time.Time{}
	return nil
}

type captureHook struct {
	handled []HandleInfo
}

func (h *captureHook) Claimed(context.Context, ClaimInfo) {}

func (h *captureHook) Handled(_ context.Context, info HandleInfo) {
	h.handled = append(h.handled, info)
}

func (h *captureHook) statuses() []ResultStatus {
	statuses := make([]ResultStatus, 0, len(h.handled))
	for _, info := range h.handled {
		statuses = append(statuses, info.Status)
	}
	return statuses
}
