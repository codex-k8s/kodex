package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

func TestServicePingDelegatesToRepository(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("storage unavailable")
	repository := &fakeRepository{err: expectedErr}
	service := New(repository)

	if err := service.Ping(context.Background()); !errors.Is(err, expectedErr) {
		t.Fatalf("Ping() err = %v, want %v", err, expectedErr)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls = %d, want 1", repository.calls)
	}
}

func TestNewPanicsWithoutRepository(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New() did not panic")
		}
	}()
	_ = New(nil)
}

func TestRecordProviderLimitSnapshotStoresLimitAndRuntimeState(t *testing.T) {
	t.Parallel()

	snapshotID := uuid.New()
	runtimeStateID := uuid.New()
	accountID := uuid.New()
	now := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	capturedAt := now.Add(-time.Minute)
	remaining := int64(0)
	limitValue := int64(5000)
	repository := &fakeRepository{}
	service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: []uuid.UUID{snapshotID, runtimeStateID}})

	snapshot, err := service.RecordProviderLimitSnapshot(context.Background(), RecordProviderLimitSnapshotInput{
		ExternalAccountID: accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		LimitClass:        " core ",
		Remaining:         &remaining,
		LimitValue:        &limitValue,
		CapturedAt:        capturedAt,
		Source:            enum.ProviderLimitSourceProviderHub,
		Meta:              value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("RecordProviderLimitSnapshot(): %v", err)
	}
	if snapshot.ID != snapshotID || snapshot.LimitClass != "core" {
		t.Fatalf("snapshot = %+v, want id %s and trimmed limit class", snapshot, snapshotID)
	}
	if repository.recordedRuntimeState.ID != runtimeStateID {
		t.Fatalf("runtime state id = %s, want %s", repository.recordedRuntimeState.ID, runtimeStateID)
	}
	if repository.recordedRuntimeState.Status != enum.ProviderAccountRuntimeStatusLimited {
		t.Fatalf("runtime state status = %s, want limited", repository.recordedRuntimeState.Status)
	}
	if repository.recordedRuntimeState.LastCheckedAt == nil || !repository.recordedRuntimeState.LastCheckedAt.Equal(capturedAt) {
		t.Fatalf("last checked at = %v, want %s", repository.recordedRuntimeState.LastCheckedAt, capturedAt)
	}
}

func TestRecordProviderLimitSnapshotRejectsMissingCommandIdentity(t *testing.T) {
	t.Parallel()

	remaining := int64(1)
	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}})

	_, err := service.RecordProviderLimitSnapshot(context.Background(), RecordProviderLimitSnapshotInput{
		ExternalAccountID: uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		LimitClass:        "core",
		Remaining:         &remaining,
		CapturedAt:        time.Now(),
		Source:            enum.ProviderLimitSourceProviderHub,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordProviderLimitSnapshot() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestIngestWebhookEventStoresProcessedGitHubIssueWebhook(t *testing.T) {
	t.Parallel()

	webhookID := uuid.New()
	receivedEventID := uuid.New()
	providerEventID := uuid.New()
	normalizedEventID := uuid.New()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{webhookID, receivedEventID, providerEventID, normalizedEventID}},
		fakeWebhookNormalizer{facts: value.ProviderWebhookFacts{
			FactKind:             value.ProviderWebhookFactKindWorkItem,
			ProviderWorkItemID:   "55",
			Kind:                 "issue",
			Number:               7,
			RepositoryFullName:   "codex-k8s/kodex",
			RepositoryProviderID: "101",
			OccurredAt:           now.Add(-time.Minute),
		}, ok: true},
	)

	webhook, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
		ProviderSlug:         enum.ProviderSlugGitHub,
		DeliveryID:           "delivery-1",
		EventName:            "issues",
		RepositoryProviderID: "101",
		ReceivedAt:           now,
		PayloadJSON:          []byte(`{"action":"opened","repository":{"id":101,"full_name":"codex-k8s/kodex"},"issue":{"id":55,"number":7,"updated_at":"2026-05-07T11:59:00Z"}}`),
		Meta:                 value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("IngestWebhookEvent(): %v", err)
	}
	if webhook.ID != webhookID || webhook.ProcessingStatus != enum.WebhookProcessingStatusProcessed {
		t.Fatalf("webhook = %+v, want processed id %s", webhook, webhookID)
	}
	if len(repository.recordedProviderEvents) != 1 {
		t.Fatalf("provider events = %d, want 1", len(repository.recordedProviderEvents))
	}
	if repository.recordedProviderEvents[0].EventType != providerEventWorkItemSynced || repository.recordedProviderEvents[0].AggregateID != "55" {
		t.Fatalf("provider event = %+v, want work item 55", repository.recordedProviderEvents[0])
	}
	if len(repository.recordedOutboxEvents) != 2 {
		t.Fatalf("outbox events = %d, want received and normalized", len(repository.recordedOutboxEvents))
	}
}

func TestIngestWebhookEventStoresFailedKnownWebhookWithBadPayloadShape(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		fakeWebhookNormalizer{ok: true, err: errors.New("provider payload misses required id")},
	)

	webhook, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
		ProviderSlug: enum.ProviderSlugGitHub,
		DeliveryID:   "delivery-1",
		EventName:    "issues",
		ReceivedAt:   now,
		PayloadJSON:  []byte(`{"repository":{"id":101}}`),
		Meta:         value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("IngestWebhookEvent(): %v", err)
	}
	if webhook.ProcessingStatus != enum.WebhookProcessingStatusFailed || webhook.LastError == "" {
		t.Fatalf("webhook = %+v, want failed with error", webhook)
	}
	if len(repository.recordedProviderEvents) != 0 || len(repository.recordedOutboxEvents) != 1 {
		t.Fatalf("provider events = %d outbox = %d, want only received outbox", len(repository.recordedProviderEvents), len(repository.recordedOutboxEvents))
	}
}

func TestRetryWebhookEventProcessingReturnsCurrentStateAfterConcurrentProcessing(t *testing.T) {
	t.Parallel()

	webhookID := uuid.New()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	processedWebhook := entity.WebhookEvent{
		ID:               webhookID,
		ProviderSlug:     enum.ProviderSlugGitHub,
		DeliveryID:       "delivery-1",
		EventName:        "issues",
		ReceivedAt:       now,
		ProcessingStatus: enum.WebhookProcessingStatusProcessed,
		PayloadJSON:      []byte(`{"repository":{"id":101},"issue":{"id":55}}`),
	}
	repository := &fakeRepository{
		recordedWebhook: entity.WebhookEvent{
			ID:               webhookID,
			ProviderSlug:     enum.ProviderSlugGitHub,
			DeliveryID:       "delivery-1",
			EventName:        "issues",
			ReceivedAt:       now,
			ProcessingStatus: enum.WebhookProcessingStatusPending,
			PayloadJSON:      []byte(`{"repository":{"id":101},"issue":{"id":55}}`),
		},
		processWebhookErr:   errs.ErrNotFound,
		webhookAfterProcess: &processedWebhook,
	}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}},
		fakeWebhookNormalizer{facts: value.ProviderWebhookFacts{
			FactKind:             value.ProviderWebhookFactKindWorkItem,
			ProviderWorkItemID:   "55",
			Kind:                 "issue",
			RepositoryProviderID: "101",
			OccurredAt:           now,
		}, ok: true},
	)

	webhook, err := service.RetryWebhookEventProcessing(context.Background(), RetryWebhookEventProcessingInput{
		WebhookEventID: webhookID,
		Meta:           value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("RetryWebhookEventProcessing(): %v", err)
	}
	if webhook.ProcessingStatus != enum.WebhookProcessingStatusProcessed {
		t.Fatalf("status = %s, want processed", webhook.ProcessingStatus)
	}
}

func TestRetryWebhookEventProcessingDoesNotSwallowStorageConflict(t *testing.T) {
	t.Parallel()

	webhookID := uuid.New()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		recordedWebhook: entity.WebhookEvent{
			ID:               webhookID,
			ProviderSlug:     enum.ProviderSlugGitHub,
			DeliveryID:       "delivery-1",
			EventName:        "issues",
			ReceivedAt:       now,
			ProcessingStatus: enum.WebhookProcessingStatusPending,
			PayloadJSON:      []byte(`{"repository":{"id":101},"issue":{"id":55}}`),
		},
		processWebhookErr: errs.ErrConflict,
	}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}},
		fakeWebhookNormalizer{facts: value.ProviderWebhookFacts{
			FactKind:             value.ProviderWebhookFactKindWorkItem,
			ProviderWorkItemID:   "55",
			Kind:                 "issue",
			RepositoryProviderID: "101",
			OccurredAt:           now,
		}, ok: true},
	)

	_, err := service.RetryWebhookEventProcessing(context.Background(), RetryWebhookEventProcessingInput{
		WebhookEventID: webhookID,
		Meta:           value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RetryWebhookEventProcessing() err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRetryWebhookEventProcessingReturnsConflictWhenRereadStaysPending(t *testing.T) {
	t.Parallel()

	webhookID := uuid.New()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	pendingWebhook := entity.WebhookEvent{
		ID:               webhookID,
		ProviderSlug:     enum.ProviderSlugGitHub,
		DeliveryID:       "delivery-1",
		EventName:        "issues",
		ReceivedAt:       now,
		ProcessingStatus: enum.WebhookProcessingStatusPending,
		PayloadJSON:      []byte(`{"repository":{"id":101},"issue":{"id":55}}`),
	}
	repository := &fakeRepository{
		recordedWebhook:     pendingWebhook,
		processWebhookErr:   errs.ErrNotFound,
		webhookAfterProcess: &pendingWebhook,
	}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}},
		fakeWebhookNormalizer{facts: value.ProviderWebhookFacts{
			FactKind:             value.ProviderWebhookFactKindWorkItem,
			ProviderWorkItemID:   "55",
			Kind:                 "issue",
			RepositoryProviderID: "101",
			OccurredAt:           now,
		}, ok: true},
	)

	_, err := service.RetryWebhookEventProcessing(context.Background(), RetryWebhookEventProcessingInput{
		WebhookEventID: webhookID,
		Meta:           value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RetryWebhookEventProcessing() err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestListProviderAccountRuntimeStatesRejectsScopeFiltersUntilResolverExists(t *testing.T) {
	t.Parallel()

	service := New(&fakeRepository{})
	projectID := uuid.New()

	_, err := service.ListProviderAccountRuntimeStates(context.Background(), ListProviderAccountRuntimeStatesInput{
		ProjectID: &projectID,
		Meta:      value.QueryMeta{Actor: value.Actor{Type: "user", ID: uuid.NewString()}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListProviderAccountRuntimeStates() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

type fakeRepository struct {
	err                    error
	calls                  int
	recordedSnapshot       entity.ProviderLimitSnapshot
	recordedRuntimeState   entity.ProviderAccountRuntimeState
	recordedWebhook        entity.WebhookEvent
	processWebhookErr      error
	webhookAfterProcess    *entity.WebhookEvent
	recordedProviderEvents []entity.ProviderEvent
	recordedOutboxEvents   []entity.OutboxEvent
}

func (r *fakeRepository) Ping(context.Context) error {
	r.calls++
	return r.err
}

func (r *fakeRepository) UpsertAccountRuntimeState(context.Context, entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{}, r.err
}

func (r *fakeRepository) StoreWebhookEvent(_ context.Context, webhook entity.WebhookEvent, providerEvents []entity.ProviderEvent, outboxEvents []entity.OutboxEvent) (entity.WebhookEvent, []entity.ProviderEvent, error) {
	r.recordedWebhook = webhook
	r.recordedProviderEvents = providerEvents
	r.recordedOutboxEvents = outboxEvents
	return webhook, providerEvents, r.err
}

func (r *fakeRepository) ProcessWebhookEvent(_ context.Context, webhook entity.WebhookEvent, providerEvents []entity.ProviderEvent, outboxEvents []entity.OutboxEvent) (entity.WebhookEvent, error) {
	r.recordedWebhook = webhook
	r.recordedProviderEvents = providerEvents
	r.recordedOutboxEvents = outboxEvents
	if r.webhookAfterProcess != nil {
		r.recordedWebhook = *r.webhookAfterProcess
	}
	if r.processWebhookErr != nil {
		return webhook, r.processWebhookErr
	}
	return webhook, r.err
}

func (r *fakeRepository) GetWebhookEvent(context.Context, uuid.UUID) (entity.WebhookEvent, error) {
	return r.recordedWebhook, r.err
}

func (r *fakeRepository) ListWebhookEvents(context.Context, query.WebhookEventFilter) ([]entity.WebhookEvent, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) ListProviderEvents(context.Context, query.ProviderEventFilter) ([]entity.ProviderEvent, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) GetAccountRuntimeState(context.Context, query.AccountRuntimeStateLookup) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{}, r.err
}

func (r *fakeRepository) ListAccountRuntimeStates(context.Context, query.AccountRuntimeStateFilter) ([]entity.ProviderAccountRuntimeState, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) RecordLimitSnapshot(_ context.Context, snapshot entity.ProviderLimitSnapshot, state entity.ProviderAccountRuntimeState) (entity.ProviderLimitSnapshot, error) {
	r.recordedSnapshot = snapshot
	r.recordedRuntimeState = state
	return snapshot, r.err
}

func (r *fakeRepository) ListLimitSnapshots(context.Context, query.LimitSnapshotFilter) ([]entity.ProviderLimitSnapshot, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) RecordProviderOperation(context.Context, entity.ProviderOperation) (entity.ProviderOperation, error) {
	return entity.ProviderOperation{}, r.err
}

func (r *fakeRepository) ListProviderOperations(context.Context, query.ProviderOperationFilter) ([]entity.ProviderOperation, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, r.err
}

func (r *fakeRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return r.err
}

func (r *fakeRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return r.err
}

func (r *fakeRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return r.err
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDs struct {
	ids []uuid.UUID
}

func (g *sequenceIDs) New() uuid.UUID {
	if len(g.ids) == 0 {
		panic("test id sequence is empty")
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id
}

type fakeWebhookNormalizer struct {
	providerSlug enum.ProviderSlug
	facts        value.ProviderWebhookFacts
	ok           bool
	err          error
}

func (n fakeWebhookNormalizer) ProviderSlug() enum.ProviderSlug {
	if n.providerSlug == "" {
		return enum.ProviderSlugGitHub
	}
	return n.providerSlug
}

func (n fakeWebhookNormalizer) NormalizeWebhook(entity.WebhookEvent) (value.ProviderWebhookFacts, bool, error) {
	return n.facts, n.ok, n.err
}
