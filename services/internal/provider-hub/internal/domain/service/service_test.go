package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
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

func TestIngestWebhookEventStoresProjectionUpdateFromKnownFacts(t *testing.T) {
	t.Parallel()

	webhookID := uuid.New()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{webhookID, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		fakeWebhookNormalizer{facts: value.ProviderWebhookFacts{
			FactKind:             value.ProviderWebhookFactKindWorkItem,
			ProviderWorkItemID:   "55",
			Kind:                 "issue",
			Number:               7,
			RepositoryFullName:   "codex-k8s/kodex",
			RepositoryProviderID: "101",
			OccurredAt:           now.Add(-time.Minute),
			WorkItem: &value.ProviderWorkItemSnapshot{
				ProviderSlug:       string(enum.ProviderSlugGitHub),
				ProviderWorkItemID: "55",
				RepositoryFullName: "codex-k8s/kodex",
				Kind:               string(enum.WorkItemKindIssue),
				Number:             7,
				URL:                "https://github.com/codex-k8s/kodex/issues/7",
				Title:              "Проверить проекции",
				State:              "open",
				Body:               "<!-- kodex:artifact v1\nkind: issue\nmanaged_by: kodex\nwork_type: dev\nnext_ref: https://github.com/codex-k8s/kodex/issues/8\n-->\nОписание задачи",
				Labels:             []string{"area:provider-hub"},
				ProviderUpdatedAt:  now.Add(-time.Minute),
			},
		}, ok: true},
	)

	_, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
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
	if repository.recordedProjection.WorkItem == nil {
		t.Fatal("projection work item is nil, want stored projection")
	}
	if repository.recordedProjection.WorkItem.WorkItemType != "dev" || repository.recordedProjection.WorkItem.WatermarkStatus != enum.WorkItemWatermarkStatusValid {
		t.Fatalf("projection = %+v, want valid dev watermark", repository.recordedProjection.WorkItem)
	}
	if len(repository.recordedProjection.Relationships) != 1 || repository.recordedProjection.Relationships[0].RelationshipType != relationshipNext {
		t.Fatalf("relationships = %+v, want next relationship", repository.recordedProjection.Relationships)
	}
	if len(repository.recordedOutboxEvents) != 4 {
		t.Fatalf("outbox events = %d, want received, normalized, work item and relationship", len(repository.recordedOutboxEvents))
	}
}

func TestWorkItemProjectionValidatesWatermarkContract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		kind       string
		body       string
		wantStatus enum.WorkItemWatermarkStatus
	}{
		{
			name:       "valid issue",
			kind:       string(enum.WorkItemKindIssue),
			body:       "<!-- kodex:artifact v1\nkind: issue\nmanaged_by: kodex\nwork_type: dev\n-->\nbody",
			wantStatus: enum.WorkItemWatermarkStatusValid,
		},
		{
			name:       "missing work type",
			kind:       string(enum.WorkItemKindIssue),
			body:       "<!-- kodex:artifact v1\nkind: issue\nmanaged_by: kodex\n-->\nbody",
			wantStatus: enum.WorkItemWatermarkStatusInvalid,
		},
		{
			name:       "mismatched kind",
			kind:       string(enum.WorkItemKindPullRequest),
			body:       "<!-- kodex:artifact v1\nkind: issue\nmanaged_by: kodex\nwork_type: dev\nsource_ref: https://github.com/codex-k8s/kodex/issues/7\n-->\nbody",
			wantStatus: enum.WorkItemWatermarkStatusInvalid,
		},
		{
			name:       "pull request without source ref",
			kind:       string(enum.WorkItemKindPullRequest),
			body:       "<!-- kodex:artifact v1\nkind: pull_request\nmanaged_by: kodex\nwork_type: dev\n-->\nbody",
			wantStatus: enum.WorkItemWatermarkStatusInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projection, _, err := workItemProjectionFromSnapshot(value.ProviderWorkItemSnapshot{
				ProviderSlug:       string(enum.ProviderSlugGitHub),
				ProviderWorkItemID: "55",
				RepositoryFullName: "codex-k8s/kodex",
				Kind:               tc.kind,
				Number:             7,
				URL:                "https://github.com/codex-k8s/kodex/issues/7",
				Title:              "Проверить контракт watermark",
				State:              "open",
				Body:               tc.body,
				ProviderUpdatedAt:  now,
			}, now)
			if err != nil {
				t.Fatalf("workItemProjectionFromSnapshot(): %v", err)
			}
			if projection.WatermarkStatus != tc.wantStatus {
				t.Fatalf("watermark status = %s, want %s", projection.WatermarkStatus, tc.wantStatus)
			}
		})
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

func TestGetWorkItemProjectionDelegatesToRepository(t *testing.T) {
	t.Parallel()

	projectionID := uuid.New()
	repository := &fakeRepository{
		workItemProjection: entity.ProviderWorkItemProjection{
			Base: entity.Base{ID: projectionID},
			Kind: enum.WorkItemKindIssue,
		},
	}
	service := New(repository)

	projection, err := service.GetWorkItemProjection(context.Background(), GetWorkItemProjectionInput{
		WorkItemProjectionID: projectionID,
		Meta:                 value.QueryMeta{Actor: value.Actor{Type: "user", ID: uuid.NewString()}},
	})
	if err != nil {
		t.Fatalf("GetWorkItemProjection(): %v", err)
	}
	if projection.ID != projectionID {
		t.Fatalf("projection id = %s, want %s", projection.ID, projectionID)
	}
	if repository.lastWorkItemLookup.ID == nil || *repository.lastWorkItemLookup.ID != projectionID {
		t.Fatalf("lookup = %+v, want id %s", repository.lastWorkItemLookup, projectionID)
	}
}

func TestEnqueueReconciliationCreatesCursorsForUniqueArtifacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 13, 0, 0, 0, time.UTC)
	externalAccountID := uuid.New()
	issueCursorID := uuid.New()
	prCursorID := uuid.New()
	repository := &fakeRepository{}
	requestID := uuid.New()
	service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: []uuid.UUID{requestID, issueCursorID, prCursorID}})

	result, err := service.EnqueueReconciliation(context.Background(), EnqueueReconciliationInput{
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: externalAccountID,
		ScopeType:         enum.SyncCursorScopeRepository,
		ScopeRef:          " codex-k8s/kodex ",
		ArtifactKinds:     []enum.SyncArtifactKind{enum.SyncArtifactIssue, enum.SyncArtifactPullRequest, enum.SyncArtifactIssue},
		Priority:          enum.SyncCursorPriorityHot,
		Meta:              value.CommandMeta{IdempotencyKey: "repo-sync-1"},
	})
	if err != nil {
		t.Fatalf("EnqueueReconciliation(): %v", err)
	}
	if len(result.SyncCursors) != 2 {
		t.Fatalf("sync cursors = %d, want 2", len(result.SyncCursors))
	}
	if repository.reconciliationRequest.ID != requestID || repository.reconciliationRequest.IdempotencyKey != "repo-sync-1" {
		t.Fatalf("request = %+v, want idempotent request %s", repository.reconciliationRequest, requestID)
	}
	if repository.reconciliationRequest.ExternalAccountID != externalAccountID {
		t.Fatalf("request account = %s, want %s", repository.reconciliationRequest.ExternalAccountID, externalAccountID)
	}
	if repository.enqueuedSyncCursors[0].ID != issueCursorID || repository.enqueuedSyncCursors[0].ScopeRef != "codex-k8s/kodex" {
		t.Fatalf("first cursor = %+v, want trimmed scope and id %s", repository.enqueuedSyncCursors[0], issueCursorID)
	}
	if repository.enqueuedSyncCursors[0].ExternalAccountID != externalAccountID {
		t.Fatalf("first cursor account = %s, want %s", repository.enqueuedSyncCursors[0].ExternalAccountID, externalAccountID)
	}
	if string(repository.enqueuedSyncCursors[0].RateBudgetStateJSON) != "{}" {
		t.Fatalf("rate budget json = %s, want {}", repository.enqueuedSyncCursors[0].RateBudgetStateJSON)
	}
}

func TestEnqueueReconciliationRejectsMissingCommandIdentity(t *testing.T) {
	t.Parallel()

	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})
	_, err := service.EnqueueReconciliation(context.Background(), EnqueueReconciliationInput{
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: uuid.New(),
		ScopeType:         enum.SyncCursorScopeRepository,
		ScopeRef:          "codex-k8s/kodex",
		ArtifactKinds:     []enum.SyncArtifactKind{enum.SyncArtifactIssue},
		Priority:          enum.SyncCursorPriorityHot,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("EnqueueReconciliation() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestEnqueueReconciliationRejectsMissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})
	_, err := service.EnqueueReconciliation(context.Background(), EnqueueReconciliationInput{
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: uuid.New(),
		ScopeType:         enum.SyncCursorScopeRepository,
		ScopeRef:          "codex-k8s/kodex",
		ArtifactKinds:     []enum.SyncArtifactKind{enum.SyncArtifactIssue},
		Priority:          enum.SyncCursorPriorityHot,
		Meta:              value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("EnqueueReconciliation() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRegisterProviderArtifactSignalEnqueuesHotWorkItemCursors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-2 * time.Minute)
	signalStorageID := uuid.New()
	requestID := uuid.New()
	commentCursorID := uuid.New()
	pullRequestCursorID := uuid.New()
	relationshipCursorID := uuid.New()
	externalAccountID := uuid.New()
	repository := &fakeRepository{}
	service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: []uuid.UUID{signalStorageID, requestID, commentCursorID, pullRequestCursorID, relationshipCursorID}})

	result, err := service.RegisterProviderArtifactSignal(context.Background(), RegisterProviderArtifactSignalInput{
		SignalID:          "slot-agent-signal-1",
		ExternalAccountID: externalAccountID,
		Target: ProviderArtifactTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: " codex-k8s/kodex ",
			WorkItemKind:       enum.WorkItemKindPullRequest,
			Number:             688,
		},
		Source:      " slot_agent_after ",
		ObservedAt:  observedAt,
		PayloadJSON: []byte(`{"run_id":"run-1"}`),
		Meta:        value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("RegisterProviderArtifactSignal(): %v", err)
	}
	if result.SignalID != "slot-agent-signal-1" || result.Status != "accepted" {
		t.Fatalf("result = %+v, want accepted signal", result)
	}
	if repository.reconciliationRequest.ID != requestID ||
		repository.reconciliationRequest.IdempotencyKey != "artifact-signal:id:slot-agent-signal-1" ||
		repository.reconciliationRequest.ExternalAccountID != externalAccountID ||
		repository.reconciliationRequest.ScopeType != enum.SyncCursorScopeWorkItem ||
		repository.reconciliationRequest.ScopeRef != "codex-k8s/kodex#pull_request:688" ||
		repository.reconciliationRequest.Priority != enum.SyncCursorPriorityHot {
		t.Fatalf("request = %+v, want hot work item signal request", repository.reconciliationRequest)
	}
	if repository.providerArtifactSignal.ID != signalStorageID ||
		repository.providerArtifactSignal.IdentityKey != "artifact-signal:id:slot-agent-signal-1" ||
		repository.providerArtifactSignal.ExternalAccountID != externalAccountID ||
		repository.providerArtifactSignal.ScopeRef != "codex-k8s/kodex#pull_request:688" ||
		string(repository.providerArtifactSignal.PayloadJSON) != `{"run_id":"run-1"}` {
		t.Fatalf("stored signal = %+v, want signal-level idempotency record", repository.providerArtifactSignal)
	}
	wantKinds := []enum.SyncArtifactKind{enum.SyncArtifactComment, enum.SyncArtifactPullRequest, enum.SyncArtifactRelationship}
	if len(repository.reconciliationRequest.ArtifactKinds) != len(wantKinds) {
		t.Fatalf("artifact kinds = %+v, want %+v", repository.reconciliationRequest.ArtifactKinds, wantKinds)
	}
	for index, wantKind := range wantKinds {
		if repository.reconciliationRequest.ArtifactKinds[index] != wantKind {
			t.Fatalf("artifact kinds = %+v, want %+v", repository.reconciliationRequest.ArtifactKinds, wantKinds)
		}
	}
	if len(repository.enqueuedSyncCursors) != len(wantKinds) {
		t.Fatalf("cursors = %d, want %d", len(repository.enqueuedSyncCursors), len(wantKinds))
	}
	if repository.enqueuedSyncCursors[1].ID != pullRequestCursorID ||
		repository.enqueuedSyncCursors[1].ExternalAccountID != externalAccountID ||
		repository.enqueuedSyncCursors[1].ArtifactKind != enum.SyncArtifactPullRequest ||
		repository.enqueuedSyncCursors[1].Priority != enum.SyncCursorPriorityHot {
		t.Fatalf("pull request cursor = %+v, want hot PR cursor", repository.enqueuedSyncCursors[1])
	}
}

func TestRegisterProviderArtifactSignalSupportsTargetForms(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 13, 0, 0, 0, time.UTC)
	externalAccountID := uuid.New()
	cases := []struct {
		name         string
		target       ProviderArtifactTarget
		wantScopeRef string
		wantKinds    []enum.SyncArtifactKind
	}{
		{
			name: "web URL without kind",
			target: ProviderArtifactTarget{
				ProviderSlug: enum.ProviderSlugGitHub,
				WebURL:       "https://github.com/codex-k8s/kodex/pull/703",
			},
			wantScopeRef: "web_url:https://github.com/codex-k8s/kodex/pull/703",
			wantKinds: []enum.SyncArtifactKind{
				enum.SyncArtifactComment,
				enum.SyncArtifactIssue,
				enum.SyncArtifactMergeRequest,
				enum.SyncArtifactPullRequest,
				enum.SyncArtifactRelationship,
			},
		},
		{
			name: "provider object id without kind",
			target: ProviderArtifactTarget{
				ProviderSlug:     enum.ProviderSlugGitLab,
				ProviderObjectID: "gid://gitlab/MergeRequest/42",
			},
			wantScopeRef: "provider_object_id:gid://gitlab/MergeRequest/42",
			wantKinds: []enum.SyncArtifactKind{
				enum.SyncArtifactComment,
				enum.SyncArtifactIssue,
				enum.SyncArtifactMergeRequest,
				enum.SyncArtifactPullRequest,
				enum.SyncArtifactRelationship,
			},
		},
		{
			name: "repository number without kind",
			target: ProviderArtifactTarget{
				ProviderSlug:       enum.ProviderSlugGitHub,
				RepositoryFullName: "codex-k8s/kodex",
				Number:             703,
			},
			wantScopeRef: "codex-k8s/kodex#number:703",
			wantKinds: []enum.SyncArtifactKind{
				enum.SyncArtifactComment,
				enum.SyncArtifactIssue,
				enum.SyncArtifactMergeRequest,
				enum.SyncArtifactPullRequest,
				enum.SyncArtifactRelationship,
			},
		},
		{
			name: "repository only",
			target: ProviderArtifactTarget{
				ProviderSlug:       enum.ProviderSlugGitHub,
				RepositoryFullName: "codex-k8s/kodex",
			},
			wantScopeRef: "codex-k8s/kodex",
			wantKinds:    []enum.SyncArtifactKind{enum.SyncArtifactRepository},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
			repository := &fakeRepository{}
			service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: ids})

			_, err := service.RegisterProviderArtifactSignal(context.Background(), RegisterProviderArtifactSignalInput{
				SignalID:          "signal-" + tc.name,
				ExternalAccountID: externalAccountID,
				Target:            tc.target,
				Source:            "agent_manager",
				ObservedAt:        now.Add(-time.Minute),
				Meta:              value.CommandMeta{CommandID: uuid.New()},
			})
			if err != nil {
				t.Fatalf("RegisterProviderArtifactSignal(): %v", err)
			}
			if repository.reconciliationRequest.ScopeRef != tc.wantScopeRef ||
				repository.reconciliationRequest.Priority != enum.SyncCursorPriorityHot {
				t.Fatalf("request = %+v, want scope ref %s hot", repository.reconciliationRequest, tc.wantScopeRef)
			}
			if len(repository.reconciliationRequest.ArtifactKinds) != len(tc.wantKinds) {
				t.Fatalf("artifact kinds = %+v, want %+v", repository.reconciliationRequest.ArtifactKinds, tc.wantKinds)
			}
			for index, wantKind := range tc.wantKinds {
				if repository.reconciliationRequest.ArtifactKinds[index] != wantKind {
					t.Fatalf("artifact kinds = %+v, want %+v", repository.reconciliationRequest.ArtifactKinds, tc.wantKinds)
				}
			}
		})
	}
}

func TestRegisterProviderArtifactSignalRejectsNumberWithoutLocator(t *testing.T) {
	t.Parallel()

	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})
	_, err := service.RegisterProviderArtifactSignal(context.Background(), RegisterProviderArtifactSignalInput{
		ExternalAccountID: uuid.New(),
		Target: ProviderArtifactTarget{
			ProviderSlug: enum.ProviderSlugGitHub,
			Number:       581,
		},
		Source:     "agent_manager",
		ObservedAt: time.Now(),
		Meta:       value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RegisterProviderArtifactSignal() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRegisterProviderArtifactSignalRejectsMissingExternalAccount(t *testing.T) {
	t.Parallel()

	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})
	_, err := service.RegisterProviderArtifactSignal(context.Background(), RegisterProviderArtifactSignalInput{
		Target: ProviderArtifactTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindIssue,
			Number:             581,
		},
		Source:     "agent_manager",
		ObservedAt: time.Now(),
		Meta:       value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RegisterProviderArtifactSignal() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRegisterProviderArtifactSignalRejectsMalformedPayload(t *testing.T) {
	t.Parallel()

	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})
	_, err := service.RegisterProviderArtifactSignal(context.Background(), RegisterProviderArtifactSignalInput{
		ExternalAccountID: uuid.New(),
		Target: ProviderArtifactTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindIssue,
			Number:             581,
		},
		Source:      "agent_manager",
		ObservedAt:  time.Now(),
		PayloadJSON: []byte(`[]`),
		Meta:        value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RegisterProviderArtifactSignal() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRunReconciliationBatchClaimsCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 13, 30, 0, 0, time.UTC)
	cursorID := uuid.New()
	externalAccountID := uuid.New()
	repository := &fakeRepository{
		syncCursor: entity.SyncCursor{
			Base:              entity.Base{ID: cursorID, Version: 2},
			ProviderSlug:      enum.ProviderSlugGitHub,
			ExternalAccountID: externalAccountID,
			ScopeType:         enum.SyncCursorScopeRepository,
			ScopeRef:          "codex-k8s/kodex",
			ArtifactKind:      enum.SyncArtifactIssue,
			Priority:          enum.SyncCursorPriorityHot,
		},
	}
	providerAdapter := &fakeProviderAdapter{}
	service := NewWithDependencies(Dependencies{
		Repository:           repository,
		Clock:                fixedClock{now: now},
		IDGenerator:          &sequenceIDs{ids: []uuid.UUID{uuid.New()}},
		AccountUsageResolver: fakeAccountUsageResolver{providerSlug: enum.ProviderSlugGitHub},
		SecretResolver:       &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("token-value"))},
		ProviderAdapters:     []providerclient.Adapter{providerAdapter},
	})

	result, err := service.RunReconciliationBatch(context.Background(), RunReconciliationBatchInput{
		SyncCursorID:      &cursorID,
		ExternalAccountID: &externalAccountID,
		MaxItems:          50,
		LeaseOwner:        "worker-1",
		Meta:              value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("RunReconciliationBatch(): %v", err)
	}
	if result.SyncCursor.ID != cursorID {
		t.Fatalf("leased cursor id = %s, want %s", result.SyncCursor.ID, cursorID)
	}
	if repository.syncCursorClaim.ID == nil || *repository.syncCursorClaim.ID != cursorID {
		t.Fatalf("claim = %+v, want cursor id %s", repository.syncCursorClaim, cursorID)
	}
	if repository.syncCursorClaim.ExternalAccountID == nil || *repository.syncCursorClaim.ExternalAccountID != externalAccountID {
		t.Fatalf("claim = %+v, want account id %s", repository.syncCursorClaim, externalAccountID)
	}
	if repository.syncCursorClaim.LeaseOwner != "worker-1" || !repository.syncCursorClaim.LeaseUntil.Equal(now.Add(syncCursorLeaseTTL)) {
		t.Fatalf("claim = %+v, want lease owner and ttl", repository.syncCursorClaim)
	}
	tokenBytes := providerAdapter.observedToken.Bytes()
	if len(tokenBytes) == 0 {
		t.Fatal("provider token buffer was released before reconciliation was observed")
	}
	for _, value := range tokenBytes {
		if value != 0 {
			t.Fatal("provider token was not cleared after reconciliation")
		}
	}
}

func TestRunReconciliationBatchMarksSecretResolveFailureWithoutLeakingSecret(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 14, 0, 0, 0, time.UTC)
	cursorID := uuid.New()
	repository := &fakeRepository{syncCursor: entity.SyncCursor{
		Base:              entity.Base{ID: cursorID, Version: 2},
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: uuid.New(),
		ScopeType:         enum.SyncCursorScopeRepository,
		ScopeRef:          "codex-k8s/kodex",
		ArtifactKind:      enum.SyncArtifactIssue,
		Priority:          enum.SyncCursorPriorityHot,
	}}
	service := NewWithDependencies(Dependencies{
		Repository:           repository,
		Clock:                fixedClock{now: now},
		IDGenerator:          &sequenceIDs{ids: []uuid.UUID{uuid.New()}},
		AccountUsageResolver: fakeAccountUsageResolver{providerSlug: enum.ProviderSlugGitHub},
		SecretResolver:       &fakeSecretResolver{err: secretresolver.ErrSecretNotFound},
		ProviderAdapters:     []providerclient.Adapter{&fakeProviderAdapter{}},
	})

	_, err := service.RunReconciliationBatch(context.Background(), RunReconciliationBatchInput{
		MaxItems:   1,
		LeaseOwner: "worker-1",
		Meta:       value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("RunReconciliationBatch() err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
	if repository.reconciliationCompletion.Cursor.LastError != reconciliationErrorSecretUnavailable {
		t.Fatalf("last error = %q, want %q", repository.reconciliationCompletion.Cursor.LastError, reconciliationErrorSecretUnavailable)
	}
	if strings.Contains(repository.reconciliationCompletion.Cursor.LastError, "token-value") {
		t.Fatal("cursor error leaks secret value")
	}
}

func TestRunReconciliationBatchKeepsRateLimitedCursorLeasedForRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 15, 0, 0, 0, time.UTC)
	retryAfter := time.Minute
	repository := &fakeRepository{syncCursor: entity.SyncCursor{
		Base:              entity.Base{ID: uuid.New(), Version: 2},
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: uuid.New(),
		ScopeType:         enum.SyncCursorScopeRepository,
		ScopeRef:          "codex-k8s/kodex",
		ArtifactKind:      enum.SyncArtifactIssue,
		Priority:          enum.SyncCursorPriorityHot,
	}}
	service := NewWithDependencies(Dependencies{
		Repository:           repository,
		Clock:                fixedClock{now: now},
		IDGenerator:          &sequenceIDs{ids: []uuid.UUID{uuid.New()}},
		AccountUsageResolver: fakeAccountUsageResolver{providerSlug: enum.ProviderSlugGitHub},
		SecretResolver:       &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("token-value"))},
		ProviderAdapters: []providerclient.Adapter{&fakeProviderAdapter{
			err: &providerclient.Error{Kind: providerclient.ErrorKindRateLimited, RetryAfter: retryAfter, Cause: errors.New("token-value")},
		}},
	})

	result, err := service.RunReconciliationBatch(context.Background(), RunReconciliationBatchInput{
		MaxItems:   1,
		LeaseOwner: "worker-1",
		Meta:       value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("RunReconciliationBatch(): %v", err)
	}
	if result.RetryAfter != retryAfter.String() {
		t.Fatalf("retry after = %q, want %q", result.RetryAfter, retryAfter.String())
	}
	cursor := repository.reconciliationCompletion.Cursor
	if cursor.LastError != reconciliationErrorProviderRateLimited {
		t.Fatalf("last error = %q, want rate limit", cursor.LastError)
	}
	if cursor.LeaseOwner != "worker-1" || cursor.LeaseUntil == nil || !cursor.LeaseUntil.Equal(now.Add(retryAfter)) {
		t.Fatalf("cursor lease = owner %q until %v, want retry lease", cursor.LeaseOwner, cursor.LeaseUntil)
	}
	if strings.Contains(cursor.LastError, "token-value") || strings.Contains(result.RetryAfter, "token-value") {
		t.Fatal("rate-limit result leaks secret value")
	}
}

func TestRunReconciliationBatchRejectsInvalidMaxItems(t *testing.T) {
	t.Parallel()

	service := NewWithRuntime(&fakeRepository{}, fixedClock{now: time.Now()}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})
	_, err := service.RunReconciliationBatch(context.Background(), RunReconciliationBatchInput{
		MaxItems:   0,
		LeaseOwner: "worker-1",
		Meta:       value.CommandMeta{CommandID: uuid.New()},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RunReconciliationBatch() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestCreateIssueRecordsSucceededProviderOperationWithSecretResolution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	operationID := uuid.New()
	outboxID := uuid.New()
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	workItemType := "dev"
	secretResolver := &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))}
	executor := &fakeWriteExecutor{
		result: providerclient.WriteResult{
			ResultRef:              "provider:issue:77",
			ProviderObjectID:       "issue-77",
			ProviderVersion:        "etag-77",
			ReconciliationEnqueued: true,
		},
	}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, outboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         secretResolver,
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	result, err := service.CreateIssue(context.Background(), CreateIssueInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		Title:             "  Завести issue  ",
		Body:              "  Описание  ",
		Labels:            []string{"kind:bug", "area:provider-hub"},
		WorkItemType:      &workItemType,
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			Actor: value.Actor{
				Type: "agent",
				ID:   uuid.NewString(),
			},
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				ProjectID:         projectID.String(),
				RepositoryID:      repositoryID.String(),
				RoleKey:           "agent-manager",
				RiskLevel:         value.ProviderOperationRiskLevelMedium,
				ChangedFields:     []string{"title", "body", "labels", "assignee_provider_logins", "work_item_type"},
				PolicyVersion:     "2026-05-12",
				PolicySnapshotRef: "policy:provider-write:v1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue(): %v", err)
	}
	if secretResolver.calls != 1 {
		t.Fatalf("secret resolver calls = %d, want 1", secretResolver.calls)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if executor.request.Credential.ExternalAccountID != externalAccountID {
		t.Fatalf("executor credential account = %s, want %s", executor.request.Credential.ExternalAccountID, externalAccountID)
	}
	if executor.observedTokenLen == 0 {
		t.Fatal("executor credential token is empty")
	}
	if executor.request.CreateIssue == nil || executor.request.CreateIssue.Title != "Завести issue" {
		t.Fatalf("executor request = %+v, want trimmed create issue payload", executor.request.CreateIssue)
	}
	if repository.recordedProviderOperation.ID != operationID || repository.recordedProviderOperation.Status != enum.ProviderOperationStatusSucceeded {
		t.Fatalf("operation = %+v, want stored succeeded operation %s", repository.recordedProviderOperation, operationID)
	}
	if repository.recordedProviderOperation.ProviderVersion != "etag-77" {
		t.Fatalf("provider version = %q, want etag-77", repository.recordedProviderOperation.ProviderVersion)
	}
	if len(repository.recordedOutboxEvents) != 1 || repository.recordedOutboxEvents[0].EventType != providerEventOperationCompleted {
		t.Fatalf("outbox = %+v, want completed event", repository.recordedOutboxEvents)
	}
	if result.ProviderOperation == nil || result.ProviderOperation.Status != enum.ProviderOperationStatusSucceeded {
		t.Fatalf("result operation = %+v, want succeeded operation", result.ProviderOperation)
	}
	if result.Result.ResultRef != "provider:issue:77" || !result.Result.ReconciliationEnqueued {
		t.Fatalf("result payload = %+v, want provider result and reconciliation flag", result.Result)
	}
}

func TestCreateIssueAppliesProviderProjectionAfterWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 5, 0, 0, time.UTC)
	operationID := uuid.New()
	operationOutboxID := uuid.New()
	providerEventID := uuid.New()
	workItemOutboxID := uuid.New()
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	executor := &fakeWriteExecutor{
		result: providerclient.WriteResult{
			ResultRef:        "https://github.com/codex-k8s/kodex/issues/77",
			ProviderObjectID: "github:codex-k8s/kodex:issue:77",
			ProviderVersion:  `"etag-77"`,
			WorkItem: &value.ProviderWorkItemSnapshot{
				ProjectID:          projectID.String(),
				RepositoryID:       repositoryID.String(),
				ProviderSlug:       string(enum.ProviderSlugGitHub),
				ProviderWorkItemID: "github:codex-k8s/kodex:issue:77",
				RepositoryFullName: "codex-k8s/kodex",
				Kind:               string(enum.WorkItemKindIssue),
				Number:             77,
				URL:                "https://github.com/codex-k8s/kodex/issues/77",
				Title:              "Задача из GitHub",
				State:              "open",
				Body:               "<!-- kodex:artifact v1\nkind: issue\nmanaged_by: kodex\nwork_type: dev\n-->",
				ProviderUpdatedAt:  now.Add(time.Minute),
			},
		},
	}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, operationOutboxID, providerEventID, workItemOutboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	_, err := service.CreateIssue(context.Background(), CreateIssueInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		Title:             "Задача из GitHub",
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelLow,
				ChangedFields: []string{"title", "body", "labels", "assignee_provider_logins"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue(): %v", err)
	}
	if repository.recordedProjection.WorkItem == nil || repository.recordedProjection.WorkItem.ProviderWorkItemID != "github:codex-k8s/kodex:issue:77" {
		t.Fatalf("projection = %+v, want created provider work item", repository.recordedProjection.WorkItem)
	}
	if len(repository.recordedProviderEvents) != 1 || repository.recordedProviderEvents[0].ID != providerEventID {
		t.Fatalf("provider events = %+v, want work item synced event", repository.recordedProviderEvents)
	}
	if len(repository.recordedProjection.Relationships) != 0 {
		t.Fatalf("relationships = %+v, want no project repository binding for ordinary issue", repository.recordedProjection.Relationships)
	}
	if len(repository.recordedOutboxEvents) != 2 {
		t.Fatalf("outbox events = %d, want operation and work item events", len(repository.recordedOutboxEvents))
	}
}

func TestCreateIssueReplaysStoredCommandWithoutExternalWrite(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	policyContext := value.ProviderOperationPolicyContext{
		RiskLevel:     value.ProviderOperationRiskLevelLow,
		ChangedFields: []string{"title", "body", "labels", "assignee_provider_logins"},
	}
	storedOperation := entity.ProviderOperation{
		Base:              entity.Base{ID: uuid.New(), Version: 1},
		CommandID:         commandID.String(),
		ExternalAccountID: externalAccountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		OperationType:     enum.ProviderOperationCreateIssue,
		TargetRef:         repositoryTargetRef(enum.ProviderSlugGitHub, repositoryID.String()),
		Status:            enum.ProviderOperationStatusSucceeded,
		ResultRef:         "https://github.com/codex-k8s/kodex/issues/77",
		ProviderVersion:   `"etag-77"`,
		OperationPolicyContext: value.ProviderOperationPolicyContext{
			OperationType: string(enum.ProviderOperationCreateIssue),
			TargetRef:     repositoryTargetRef(enum.ProviderSlugGitHub, repositoryID.String()),
			RiskLevel:     value.ProviderOperationRiskLevelLow,
			ChangedFields: []string{"assignee_provider_logins", "body", "labels", "title"},
			RiskTags:      []string{},
		},
	}
	secretResolver := &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))}
	executor := &fakeWriteExecutor{}
	repository := &fakeRepository{recordedProviderOperation: storedOperation}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: time.Date(2026, 5, 12, 10, 7, 0, 0, time.UTC)},
		IDGenerator:            &sequenceIDs{},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         secretResolver,
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	result, err := service.CreateIssue(context.Background(), CreateIssueInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		Title:             "Задача из GitHub",
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID:              commandID,
			OperationPolicyContext: policyContext,
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue(): %v", err)
	}
	if secretResolver.calls != 0 || executor.calls != 0 {
		t.Fatalf("secret calls = %d executor calls = %d, want replay without external write", secretResolver.calls, executor.calls)
	}
	if result.ProviderOperation == nil || result.ProviderOperation.ID != storedOperation.ID {
		t.Fatalf("result operation = %+v, want stored operation", result.ProviderOperation)
	}
	if result.Result.ResultRef != storedOperation.ResultRef {
		t.Fatalf("result ref = %q, want stored result", result.Result.ResultRef)
	}
}

func TestUpdateIssueReplaysStoredCommandBeforeExpectedVersionCheck(t *testing.T) {
	t.Parallel()

	externalAccountID := uuid.New()
	commandID := uuid.New()
	expectedVersion := int64(3)
	storedOperation := entity.ProviderOperation{
		Base:              entity.Base{ID: uuid.New(), Version: 1},
		CommandID:         commandID.String(),
		ExternalAccountID: externalAccountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		OperationType:     enum.ProviderOperationUpdateIssue,
		TargetRef:         "github:repo:codex-k8s/kodex:issue:42",
		Status:            enum.ProviderOperationStatusSucceeded,
		ResultRef:         "https://github.com/codex-k8s/kodex/issues/42",
		ProviderVersion:   `"new-etag"`,
		OperationPolicyContext: value.ProviderOperationPolicyContext{
			OperationType: string(enum.ProviderOperationUpdateIssue),
			TargetRef:     "github:repo:codex-k8s/kodex:issue:42",
			RiskLevel:     value.ProviderOperationRiskLevelLow,
			ChangedFields: []string{"title"},
		},
	}
	repository := &fakeRepository{
		recordedProviderOperation: storedOperation,
		workItemProjection: entity.ProviderWorkItemProjection{
			Base: entity.Base{ID: uuid.New(), Version: 4},
			Kind: enum.WorkItemKindIssue,
		},
	}
	executor := &fakeWriteExecutor{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: time.Date(2026, 5, 12, 10, 8, 0, 0, time.UTC)},
		IDGenerator:            &sequenceIDs{},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	title := "Повтор команды"
	result, err := service.UpdateIssue(context.Background(), UpdateIssueInput{
		Target: ProviderTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindIssue,
			Number:             42,
		},
		Title:             &title,
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID:       commandID,
			ExpectedVersion: &expectedVersion,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelLow,
				ChangedFields: []string{"title"},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateIssue(): %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want replay before expected version check", executor.calls)
	}
	if result.ProviderOperation == nil || result.ProviderOperation.ID != storedOperation.ID {
		t.Fatalf("result operation = %+v, want stored operation", result.ProviderOperation)
	}
}

func TestCreateIssueRecordsInProgressBeforeExternalWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 9, 0, 0, time.UTC)
	operationID := uuid.New()
	outboxID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	repository := &fakeRepository{}
	executor := &fakeWriteExecutor{
		beforeExecute: func() {
			if repository.recordedProviderOperation.ID != operationID ||
				repository.recordedProviderOperation.Status != enum.ProviderOperationStatusInProgress {
				t.Fatalf("operation before execute = %+v, want durable in-progress operation", repository.recordedProviderOperation)
			}
		},
		result: providerclient.WriteResult{ResultRef: "https://github.com/codex-k8s/kodex/issues/77"},
	}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, outboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	_, err := service.CreateIssue(context.Background(), CreateIssueInput{
		ProjectID:         uuid.New(),
		RepositoryID:      uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		Title:             "Новая задача",
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelLow,
				ChangedFields: []string{"title", "body", "labels", "assignee_provider_logins"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue(): %v", err)
	}
	if repository.recordedProviderOperation.Status != enum.ProviderOperationStatusSucceeded {
		t.Fatalf("operation after execute = %+v, want succeeded operation", repository.recordedProviderOperation)
	}
}

func TestCreateIssueConcurrentSameCommandExecutesOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 9, 30, 0, time.UTC)
	operationID := uuid.New()
	outboxID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	enteredExecute := make(chan struct{})
	releaseExecute := make(chan struct{})
	repository := &fakeRepository{missProviderOperationReplay: true}
	executor := &fakeWriteExecutor{
		beforeExecute: func() {
			enteredExecute <- struct{}{}
			<-releaseExecute
		},
		result: providerclient.WriteResult{ResultRef: "https://github.com/codex-k8s/kodex/issues/77"},
	}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, uuid.New(), outboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})
	input := CreateIssueInput{
		ProjectID:         uuid.New(),
		RepositoryID:      uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		Title:             "Новая задача",
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelLow,
				ChangedFields: []string{"title", "body", "labels", "assignee_provider_logins"},
			},
		},
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.CreateIssue(context.Background(), input)
		firstDone <- err
	}()
	<-enteredExecute

	_, secondErr := service.CreateIssue(context.Background(), input)
	if !errors.Is(secondErr, errs.ErrConflict) {
		close(releaseExecute)
		t.Fatalf("second CreateIssue() err = %v, want %v", secondErr, errs.ErrConflict)
	}
	close(releaseExecute)
	if firstErr := <-firstDone; firstErr != nil {
		t.Fatalf("first CreateIssue(): %v", firstErr)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
}

func TestUpdatePullRequestRecordsProviderOperation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 9, 45, 0, time.UTC)
	operationID := uuid.New()
	outboxID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	executor := &fakeWriteExecutor{result: providerclient.WriteResult{ResultRef: "https://github.com/codex-k8s/kodex/pull/77"}}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, outboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})
	title := "Обновлённый PR"
	baseBranch := "release"
	_, err := service.UpdatePullRequest(context.Background(), UpdatePullRequestInput{
		Target: ProviderTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindPullRequest,
			Number:             77,
		},
		Title:             &title,
		BaseBranch:        &baseBranch,
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelMedium,
				ChangedFields: []string{"title", "base_branch"},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdatePullRequest(): %v", err)
	}
	if executor.calls != 1 || executor.request.UpdatePullRequest == nil || executor.request.UpdatePullRequest.BaseBranch == nil {
		t.Fatalf("executor request = %+v, want update pull request command", executor.request)
	}
	if repository.recordedProviderOperation.ID != operationID ||
		repository.recordedProviderOperation.OperationType != enum.ProviderOperationUpdatePullRequest ||
		repository.recordedProviderOperation.Status != enum.ProviderOperationStatusSucceeded {
		t.Fatalf("operation = %+v, want succeeded update_pull_request operation %s", repository.recordedProviderOperation, operationID)
	}
}

func TestCreateRepositoryRecordsOperationAndRepositoryEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	operationID := uuid.New()
	operationOutboxID := uuid.New()
	repositoryCreatedOutboxID := uuid.New()
	owner := "codex-k8s"
	description := "Тестовый сервис"
	executor := &fakeWriteExecutor{
		result: providerclient.WriteResult{
			ResultRef:        "https://github.com/codex-k8s/new-service",
			ProviderObjectID: "100500",
			ProviderVersion:  `"repo-etag"`,
			Target: &providerclient.Target{
				ProviderSlug:         enum.ProviderSlugGitHub,
				RepositoryFullName:   "codex-k8s/new-service",
				ProviderRepositoryID: "100500",
				WebURL:               "https://github.com/codex-k8s/new-service",
			},
			BaseBranch: "main",
		},
	}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, operationOutboxID, repositoryCreatedOutboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	result, err := service.CreateRepository(context.Background(), CreateRepositoryInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     &owner,
		RepositoryName:    "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		Description:       &description,
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel: value.ProviderOperationRiskLevelMedium,
				ChangedFields: []string{
					"auto_init",
					"description",
					"owner_kind",
					"provider_owner",
					"repository_name",
					"visibility",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateRepository(): %v", err)
	}
	if executor.calls != 1 ||
		executor.request.CreateRepository == nil ||
		executor.request.CreateRepository.ProviderOwner != owner ||
		executor.request.CreateRepository.Visibility != enum.RepositoryVisibilityPrivate {
		t.Fatalf("executor request = %+v, want create repository command", executor.request)
	}
	if repository.recordedProviderOperation.ID != operationID ||
		repository.recordedProviderOperation.OperationType != enum.ProviderOperationCreateRepository ||
		repository.recordedProviderOperation.TargetRef != repositoryTargetRef(enum.ProviderSlugGitHub, repositoryID.String())+"#create_repository:new-service:codex-k8s" {
		t.Fatalf("operation = %+v, want create_repository operation", repository.recordedProviderOperation)
	}
	if len(repository.recordedOutboxEvents) != 2 ||
		repository.recordedOutboxEvents[1].ID != repositoryCreatedOutboxID ||
		repository.recordedOutboxEvents[1].EventType != providerEventRepositoryCreated {
		t.Fatalf("outbox events = %+v, want operation and repository created events", repository.recordedOutboxEvents)
	}
	if result.Result.Target == nil ||
		result.Result.Target.RepositoryFullName != "codex-k8s/new-service" ||
		result.Result.BaseBranch != "main" ||
		result.Result.ProviderObjectID != "100500" {
		t.Fatalf("result = %+v, want repository target and base branch", result.Result)
	}
}

func TestCreateRepositoryReplayReturnsRepositoryResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 12, 15, 0, 0, time.UTC)
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	operationID := uuid.New()
	operationOutboxID := uuid.New()
	repositoryCreatedOutboxID := uuid.New()
	owner := "codex-k8s"
	executor := &fakeWriteExecutor{
		result: providerclient.WriteResult{
			ResultRef:        "https://github.com/codex-k8s/new-service",
			ProviderObjectID: "100500",
			ProviderVersion:  `"repo-etag"`,
			Target: &providerclient.Target{
				ProviderSlug:         enum.ProviderSlugGitHub,
				RepositoryFullName:   "codex-k8s/new-service",
				ProviderRepositoryID: "100500",
				WebURL:               "https://github.com/codex-k8s/new-service",
			},
			BaseBranch: "main",
		},
	}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, operationOutboxID, repositoryCreatedOutboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})
	input := CreateRepositoryInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     &owner,
		RepositoryName:    "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel: value.ProviderOperationRiskLevelMedium,
				ChangedFields: []string{
					"auto_init",
					"owner_kind",
					"provider_owner",
					"repository_name",
					"visibility",
				},
			},
		},
	}
	first, err := service.CreateRepository(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateRepository() first: %v", err)
	}
	second, err := service.CreateRepository(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateRepository() replay: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want one provider write and one replay", executor.calls)
	}
	if first.Result.ProviderObjectID != "100500" ||
		first.Result.BaseBranch != "main" ||
		second.Result.ProviderObjectID != first.Result.ProviderObjectID ||
		second.Result.BaseBranch != first.Result.BaseBranch ||
		second.Result.ResultRef != first.Result.ResultRef ||
		second.Result.Target == nil ||
		second.Result.Target.ProviderRepositoryID != "100500" ||
		second.Result.Target.WebURL != first.Result.ResultRef {
		t.Fatalf("replay result = %+v, want same repository id/base branch/result ref as first %+v", second.Result, first.Result)
	}
}

func TestCreateBootstrapPullRequestRecordsProjectionAndBootstrapEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 9, 50, 0, time.UTC)
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	operationID := uuid.New()
	operationOutboxID := uuid.New()
	providerEventID := uuid.New()
	workItemOutboxID := uuid.New()
	bootstrapOutboxID := uuid.New()
	executor := &fakeWriteExecutor{
		result: providerclient.WriteResult{
			ResultRef:        "https://github.com/codex-k8s/kodex/pull/88",
			ProviderObjectID: "github:codex-k8s/kodex:pull_request:88",
			ProviderVersion:  `"etag-88"`,
			WorkItem: &value.ProviderWorkItemSnapshot{
				ProjectID:          projectID.String(),
				RepositoryID:       repositoryID.String(),
				ProviderSlug:       string(enum.ProviderSlugGitHub),
				ProviderWorkItemID: "github:codex-k8s/kodex:pull_request:88",
				RepositoryFullName: "codex-k8s/kodex",
				Kind:               string(enum.WorkItemKindPullRequest),
				Number:             88,
				URL:                "https://github.com/codex-k8s/kodex/pull/88",
				Title:              "Bootstrap платформы",
				State:              "open",
				ProviderUpdatedAt:  now.Add(time.Minute),
			},
		},
	}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, operationOutboxID, providerEventID, workItemOutboxID, bootstrapOutboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	_, err := service.CreateBootstrapPullRequest(context.Background(), CreateBootstrapPullRequestInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		BaseBranch:        "main",
		BootstrapBranch:   "kodex/bootstrap",
		CommitMessage:     "Bootstrap repository",
		Title:             "Bootstrap платформы",
		Body:              "Подготовленные файлы bootstrap.",
		Files:             []BootstrapFile{{Path: "services.yaml", Content: "version: 1\n"}},
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel: value.ProviderOperationRiskLevelMedium,
				ChangedFields: []string{
					"repository_target",
					"base_branch",
					"bootstrap_branch",
					"commit_message",
					"title",
					"body",
					"files",
					"draft",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateBootstrapPullRequest(): %v", err)
	}
	if executor.calls != 1 || executor.request.CreateBootstrapPullRequest == nil {
		t.Fatalf("executor request = %+v, want bootstrap pull request command", executor.request)
	}
	if repository.recordedProviderOperation.OperationType != enum.ProviderOperationCreateBootstrapPullRequest ||
		repository.recordedProviderOperation.TargetRef != repositoryTargetRef(enum.ProviderSlugGitHub, repositoryID.String())+"#bootstrap_pull_request:kodex/bootstrap" {
		t.Fatalf("operation = %+v, want bootstrap pull request operation", repository.recordedProviderOperation)
	}
	if repository.recordedProjection.WorkItem == nil ||
		repository.recordedProjection.WorkItem.ProjectID == nil ||
		*repository.recordedProjection.WorkItem.ProjectID != projectID ||
		repository.recordedProjection.WorkItem.RepositoryID == nil ||
		*repository.recordedProjection.WorkItem.RepositoryID != repositoryID {
		t.Fatalf("projection = %+v, want project and repository binding", repository.recordedProjection.WorkItem)
	}
	if len(repository.recordedProjection.Relationships) != 1 ||
		repository.recordedProjection.Relationships[0].RelationshipType != relationshipProjectRepositoryBinding {
		t.Fatalf("relationships = %+v, want project repository binding", repository.recordedProjection.Relationships)
	}
	if len(repository.recordedOutboxEvents) != 3 ||
		repository.recordedOutboxEvents[2].ID != bootstrapOutboxID ||
		repository.recordedOutboxEvents[2].EventType != providerEventRepositoryBootstrapCompleted {
		t.Fatalf("outbox events = %+v, want operation, work item and bootstrap events", repository.recordedOutboxEvents)
	}
}

func TestCreateBootstrapPullRequestRejectsSameBaseAndBootstrapBranch(t *testing.T) {
	t.Parallel()

	service := NewWithDependencies(Dependencies{
		Repository:             &fakeRepository{},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		SecretResolver:         &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))},
		ProviderWriteExecutors: []providerclient.WriteExecutor{&fakeWriteExecutor{}},
	})

	_, err := service.CreateBootstrapPullRequest(context.Background(), CreateBootstrapPullRequestInput{
		ProjectID:         uuid.New(),
		RepositoryID:      uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		RepositoryTarget:  ProviderTarget{ProviderSlug: enum.ProviderSlugGitHub, RepositoryFullName: "codex-k8s/kodex"},
		BaseBranch:        "main",
		BootstrapBranch:   " main ",
		CommitMessage:     "Bootstrap repository",
		Title:             "Bootstrap платформы",
		Files:             []BootstrapFile{{Path: "services.yaml", Content: "version: 1\n"}},
		ExternalAccountID: uuid.New(),
		Meta: value.CommandMeta{
			CommandID: uuid.New(),
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelMedium,
				ChangedFields: []string{"repository_target", "base_branch", "bootstrap_branch", "files"},
			},
		},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateBootstrapPullRequest() err = %v, want invalid argument", err)
	}
}

func TestUpdateIssueRequiresApprovalGateReferenceWhenPolicyDemandsIt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 12, 10, 10, 0, 0, time.UTC)
	operationID := uuid.New()
	outboxID := uuid.New()
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	executor := &fakeWriteExecutor{}
	repository := &fakeRepository{}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, outboxID}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	title := "Новый заголовок"
	result, err := service.UpdateIssue(context.Background(), UpdateIssueInput{
		Target: ProviderTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindIssue,
			Number:             581,
		},
		Title:             &title,
		ExternalAccountID: externalAccountID,
		Meta: value.CommandMeta{
			CommandID: commandID,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				ProjectID:        projectID.String(),
				RepositoryID:     repositoryID.String(),
				RiskLevel:        value.ProviderOperationRiskLevelHigh,
				ApprovalRequired: true,
				ChangedFields:    []string{"title"},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateIssue(): %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if repository.recordedProviderOperation.Status != enum.ProviderOperationStatusDenied ||
		repository.recordedProviderOperation.ErrorCode != writeFailureApprovalRequired {
		t.Fatalf("operation = %+v, want denied approval-required operation", repository.recordedProviderOperation)
	}
	if len(repository.recordedOutboxEvents) != 1 || repository.recordedOutboxEvents[0].EventType != providerEventOperationFailed {
		t.Fatalf("outbox = %+v, want failed event", repository.recordedOutboxEvents)
	}
	if result.ProviderOperation == nil || result.ProviderOperation.Status != enum.ProviderOperationStatusDenied {
		t.Fatalf("result operation = %+v, want denied operation", result.ProviderOperation)
	}
}

func TestUpdateIssueChecksExpectedVersionBeforeExecutor(t *testing.T) {
	t.Parallel()

	executor := &fakeWriteExecutor{}
	repository := &fakeRepository{
		workItemProjection: entity.ProviderWorkItemProjection{
			Base: entity.Base{ID: uuid.New(), Version: 4},
			Kind: enum.WorkItemKindIssue,
		},
	}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: time.Date(2026, 5, 12, 10, 20, 0, 0, time.UTC)},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	title := "Поменять заголовок"
	expectedVersion := int64(3)
	_, err := service.UpdateIssue(context.Background(), UpdateIssueInput{
		Target: ProviderTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindIssue,
			Number:             581,
		},
		Title:             &title,
		ExternalAccountID: uuid.New(),
		Meta: value.CommandMeta{
			CommandID:       uuid.New(),
			ExpectedVersion: &expectedVersion,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel:     value.ProviderOperationRiskLevelLow,
				ChangedFields: []string{"title"},
			},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("UpdateIssue() err = %v, want %v", err, errs.ErrConflict)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if len(repository.recordedOutboxEvents) != 0 {
		t.Fatalf("outbox = %+v, want no stored operation", repository.recordedOutboxEvents)
	}
}

func TestUpdateRelationshipChecksExpectedVersionBeforeExecutor(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	targetProviderRef := "https://github.com/codex-k8s/kodex/pull/731"
	executor := &fakeWriteExecutor{}
	repository := &fakeRepository{
		workItemProjection: entity.ProviderWorkItemProjection{
			Base: entity.Base{ID: sourceID, Version: 7},
			Kind: enum.WorkItemKindIssue,
		},
		relationship: entity.ProviderRelationship{
			ID:                uuid.New(),
			Version:           4,
			SourceWorkItemID:  sourceID,
			TargetProviderRef: targetProviderRef,
			RelationshipType:  "linked_pr",
			Source:            enum.RelationshipSourceManual,
			Confidence:        enum.RelationshipConfidenceConfirmed,
		},
	}
	service := NewWithDependencies(Dependencies{
		Repository:             repository,
		Clock:                  fixedClock{now: time.Date(2026, 5, 12, 10, 30, 0, 0, time.UTC)},
		IDGenerator:            &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		AccountUsageResolver:   fakeAccountUsageResolver{},
		ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
	})

	expectedVersion := int64(3)
	_, err := service.UpdateRelationship(context.Background(), UpdateRelationshipInput{
		Source: ProviderTarget{
			ProviderSlug:       enum.ProviderSlugGitHub,
			RepositoryFullName: "codex-k8s/kodex",
			WorkItemKind:       enum.WorkItemKindIssue,
			Number:             581,
		},
		TargetProviderRef: &targetProviderRef,
		RelationshipType:  "linked_pr",
		SourceKind:        enum.RelationshipSourceManual,
		Confidence:        enum.RelationshipConfidenceConfirmed,
		ExternalAccountID: uuid.New(),
		Meta: value.CommandMeta{
			CommandID:       uuid.New(),
			ExpectedVersion: &expectedVersion,
			OperationPolicyContext: value.ProviderOperationPolicyContext{
				RiskLevel: value.ProviderOperationRiskLevelLow,
				ChangedFields: []string{
					"source",
					"target_provider_ref",
					"relationship_type",
					"source_kind",
					"confidence",
				},
			},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("UpdateRelationship() err = %v, want %v", err, errs.ErrConflict)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
	if len(repository.recordedOutboxEvents) != 0 {
		t.Fatalf("outbox = %+v, want no stored operation", repository.recordedOutboxEvents)
	}
}

type fakeRepository struct {
	mu                          sync.Mutex
	err                         error
	calls                       int
	missProviderOperationReplay bool
	recordedSnapshot            entity.ProviderLimitSnapshot
	recordedRuntimeState        entity.ProviderAccountRuntimeState
	recordedWebhook             entity.WebhookEvent
	recordedProjection          providerrepo.ProjectionUpdate
	processWebhookErr           error
	webhookAfterProcess         *entity.WebhookEvent
	recordedProviderEvents      []entity.ProviderEvent
	recordedOutboxEvents        []entity.OutboxEvent
	recordedProviderOperation   entity.ProviderOperation
	workItemProjection          entity.ProviderWorkItemProjection
	relationship                entity.ProviderRelationship
	lastWorkItemLookup          query.ProviderTargetLookup
	reconciliationRequest       entity.ReconciliationRequest
	enqueuedSyncCursors         []entity.SyncCursor
	providerArtifactSignal      entity.ProviderArtifactSignal
	syncCursor                  entity.SyncCursor
	syncCursorClaim             providerrepo.SyncCursorClaim
	reconciliationCompletion    providerrepo.ReconciliationBatchCompletion
}

func (r *fakeRepository) Ping(context.Context) error {
	r.calls++
	return r.err
}

func (r *fakeRepository) UpsertAccountRuntimeState(context.Context, entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{}, r.err
}

func (r *fakeRepository) StoreWebhookEvent(_ context.Context, webhook entity.WebhookEvent, projection providerrepo.ProjectionUpdate, providerEvents []entity.ProviderEvent, outboxEvents []entity.OutboxEvent) (entity.WebhookEvent, []entity.ProviderEvent, error) {
	r.recordedWebhook = webhook
	r.recordedProjection = projection
	r.recordedProviderEvents = providerEvents
	r.recordedOutboxEvents = outboxEvents
	return webhook, providerEvents, r.err
}

func (r *fakeRepository) ProcessWebhookEvent(_ context.Context, webhook entity.WebhookEvent, projection providerrepo.ProjectionUpdate, providerEvents []entity.ProviderEvent, outboxEvents []entity.OutboxEvent) (entity.WebhookEvent, error) {
	r.recordedWebhook = webhook
	r.recordedProjection = projection
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

func (r *fakeRepository) GetWorkItemProjection(_ context.Context, lookup query.ProviderTargetLookup) (entity.ProviderWorkItemProjection, error) {
	r.lastWorkItemLookup = lookup
	return r.workItemProjection, r.err
}

func (r *fakeRepository) GetCommentProjectionByProviderID(context.Context, uuid.UUID, string) (entity.ProviderCommentProjection, error) {
	return entity.ProviderCommentProjection{}, r.err
}

func (r *fakeRepository) ListWorkItemProjections(context.Context, query.WorkItemProjectionFilter) ([]entity.ProviderWorkItemProjection, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) ListComments(context.Context, query.CommentProjectionFilter) ([]entity.ProviderCommentProjection, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) GetRelationshipByIdentity(context.Context, query.RelationshipLookup) (entity.ProviderRelationship, error) {
	if r.relationship.ID != uuid.Nil {
		return r.relationship, r.err
	}
	return entity.ProviderRelationship{}, errs.ErrNotFound
}

func (r *fakeRepository) ListRelationships(context.Context, query.RelationshipFilter) ([]entity.ProviderRelationship, query.PageResult, error) {
	if r.relationship.ID != uuid.Nil {
		return []entity.ProviderRelationship{r.relationship}, query.PageResult{}, r.err
	}
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) RegisterProviderArtifactSignal(_ context.Context, signal entity.ProviderArtifactSignal, request entity.ReconciliationRequest, cursors []entity.SyncCursor) ([]entity.SyncCursor, error) {
	r.providerArtifactSignal = signal
	r.reconciliationRequest = request
	r.enqueuedSyncCursors = append(r.enqueuedSyncCursors, cursors...)
	return cursors, r.err
}

func (r *fakeRepository) EnqueueSyncCursors(_ context.Context, request entity.ReconciliationRequest, cursors []entity.SyncCursor) ([]entity.SyncCursor, error) {
	r.reconciliationRequest = request
	r.enqueuedSyncCursors = append(r.enqueuedSyncCursors, cursors...)
	return cursors, r.err
}

func (r *fakeRepository) GetSyncCursor(context.Context, uuid.UUID) (entity.SyncCursor, error) {
	return r.syncCursor, r.err
}

func (r *fakeRepository) ListSyncCursors(context.Context, query.SyncCursorFilter) ([]entity.SyncCursor, query.PageResult, error) {
	if r.syncCursor.ID == uuid.Nil {
		return nil, query.PageResult{}, r.err
	}
	return []entity.SyncCursor{r.syncCursor}, query.PageResult{}, r.err
}

func (r *fakeRepository) ClaimSyncCursor(_ context.Context, claim providerrepo.SyncCursorClaim) (entity.SyncCursor, error) {
	r.syncCursorClaim = claim
	return r.syncCursor, r.err
}

func (r *fakeRepository) ApplyReconciliationBatch(_ context.Context, completion providerrepo.ReconciliationBatchCompletion) (entity.SyncCursor, []entity.ProviderEvent, error) {
	r.reconciliationCompletion = completion
	r.syncCursor = completion.Cursor
	r.recordedProjection = completion.ProjectionUpdate
	r.recordedProviderEvents = append([]entity.ProviderEvent(nil), completion.ProviderEvents...)
	r.recordedOutboxEvents = append([]entity.OutboxEvent(nil), completion.OutboxEvents...)
	if completion.RuntimeState != nil {
		r.recordedRuntimeState = *completion.RuntimeState
	}
	if len(completion.LimitSnapshots) > 0 {
		r.recordedSnapshot = completion.LimitSnapshots[0]
	}
	return completion.Cursor, completion.ProviderEvents, r.err
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

func (r *fakeRepository) RecordProviderOperation(_ context.Context, operation entity.ProviderOperation) (entity.ProviderOperation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return entity.ProviderOperation{}, false, r.err
	}
	if r.recordedProviderOperation.ID != uuid.Nil {
		return r.recordedProviderOperation, false, nil
	}
	r.recordedProviderOperation = operation
	return operation, true, nil
}

func (r *fakeRepository) ApplyProviderOperation(_ context.Context, completion providerrepo.ProviderOperationCompletion) (entity.ProviderOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordedProviderOperation = completion.Operation
	r.recordedProjection = completion.ProjectionUpdate
	r.recordedProviderEvents = append([]entity.ProviderEvent(nil), completion.ProviderEvents...)
	r.recordedOutboxEvents = append([]entity.OutboxEvent(nil), completion.OutboxEvents...)
	return completion.Operation, r.err
}

func (r *fakeRepository) GetProviderOperationByCommand(context.Context, enum.ProviderOperationType, string) (entity.ProviderOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.missProviderOperationReplay {
		return entity.ProviderOperation{}, errs.ErrNotFound
	}
	if r.recordedProviderOperation.ID != uuid.Nil {
		return r.recordedProviderOperation, r.err
	}
	return entity.ProviderOperation{}, errs.ErrNotFound
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

type fakeAccountUsageResolver struct {
	providerSlug      enum.ProviderSlug
	allowedActionKeys []string
	err               error
}

func (r fakeAccountUsageResolver) ResolveExternalAccountUsage(_ context.Context, input ExternalAccountUsageInput) (ExternalAccountUsageResult, error) {
	if r.err != nil {
		return ExternalAccountUsageResult{}, r.err
	}
	providerSlug := r.providerSlug
	if providerSlug == "" {
		providerSlug = enum.ProviderSlugGitHub
	}
	allowedActionKeys := append([]string(nil), r.allowedActionKeys...)
	if len(allowedActionKeys) == 0 && strings.TrimSpace(input.ActionKey) != "" {
		allowedActionKeys = []string{strings.TrimSpace(input.ActionKey)}
	}
	return ExternalAccountUsageResult{
		ExternalAccountID: uuid.NewString(),
		ProviderSlug:      providerSlug,
		SecretStoreType:   secretresolver.StoreTypeEnv,
		SecretStoreRef:    "KODEX_TEST_TOKEN",
		AllowedActionKeys: allowedActionKeys,
	}, nil
}

type fakeSecretResolver struct {
	secret secretresolver.SecretValue
	err    error
	calls  int
}

func (r *fakeSecretResolver) Resolve(context.Context, secretresolver.SecretRef) (secretresolver.SecretValue, error) {
	r.calls++
	if r.err != nil {
		return secretresolver.SecretValue{}, r.err
	}
	return r.secret, nil
}

type fakeProviderAdapter struct {
	result        providerclient.ReconciliationResult
	err           error
	observedToken secretresolver.SecretValue
}

func (a *fakeProviderAdapter) ProviderSlug() enum.ProviderSlug {
	return enum.ProviderSlugGitHub
}

func (a *fakeProviderAdapter) ProbeAccount(context.Context, providerclient.AccountProbeRequest) (providerclient.AccountProbeResult, error) {
	return providerclient.AccountProbeResult{}, nil
}

func (a *fakeProviderAdapter) Reconcile(_ context.Context, request providerclient.ReconciliationRequest) (providerclient.ReconciliationResult, error) {
	a.observedToken = request.Credential.Token
	if a.err != nil {
		return providerclient.ReconciliationResult{}, a.err
	}
	return a.result, nil
}

type fakeWriteExecutor struct {
	result           providerclient.WriteResult
	err              error
	calls            int
	request          providerclient.WriteRequest
	observedTokenLen int
	beforeExecute    func()
}

func (e *fakeWriteExecutor) ProviderSlug() enum.ProviderSlug {
	return enum.ProviderSlugGitHub
}

func (e *fakeWriteExecutor) Execute(_ context.Context, request providerclient.WriteRequest) (providerclient.WriteResult, error) {
	e.calls++
	e.request = request
	e.observedTokenLen = request.Credential.Token.Len()
	if e.beforeExecute != nil {
		e.beforeExecute()
	}
	if e.err != nil {
		return providerclient.WriteResult{}, e.err
	}
	return e.result, nil
}
