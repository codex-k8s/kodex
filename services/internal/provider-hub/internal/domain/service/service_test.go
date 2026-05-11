package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
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
	requestID := uuid.New()
	commentCursorID := uuid.New()
	pullRequestCursorID := uuid.New()
	relationshipCursorID := uuid.New()
	externalAccountID := uuid.New()
	repository := &fakeRepository{}
	service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: []uuid.UUID{requestID, commentCursorID, pullRequestCursorID, relationshipCursorID}})

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
		repository.reconciliationRequest.IdempotencyKey != "artifact-signal:slot-agent-signal-1" ||
		repository.reconciliationRequest.ExternalAccountID != externalAccountID ||
		repository.reconciliationRequest.ScopeType != enum.SyncCursorScopeWorkItem ||
		repository.reconciliationRequest.ScopeRef != "codex-k8s/kodex#pull_request:688" ||
		repository.reconciliationRequest.Priority != enum.SyncCursorPriorityHot {
		t.Fatalf("request = %+v, want hot work item signal request", repository.reconciliationRequest)
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
	service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

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

type fakeRepository struct {
	err                    error
	calls                  int
	recordedSnapshot       entity.ProviderLimitSnapshot
	recordedRuntimeState   entity.ProviderAccountRuntimeState
	recordedWebhook        entity.WebhookEvent
	recordedProjection     providerrepo.ProjectionUpdate
	processWebhookErr      error
	webhookAfterProcess    *entity.WebhookEvent
	recordedProviderEvents []entity.ProviderEvent
	recordedOutboxEvents   []entity.OutboxEvent
	workItemProjection     entity.ProviderWorkItemProjection
	lastWorkItemLookup     query.ProviderTargetLookup
	reconciliationRequest  entity.ReconciliationRequest
	enqueuedSyncCursors    []entity.SyncCursor
	syncCursor             entity.SyncCursor
	syncCursorClaim        providerrepo.SyncCursorClaim
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

func (r *fakeRepository) ListWorkItemProjections(context.Context, query.WorkItemProjectionFilter) ([]entity.ProviderWorkItemProjection, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) ListComments(context.Context, query.CommentProjectionFilter) ([]entity.ProviderCommentProjection, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) ListRelationships(context.Context, query.RelationshipFilter) ([]entity.ProviderRelationship, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
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
