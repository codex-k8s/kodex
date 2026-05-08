package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/google/uuid"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerEmbedsGeneratedUnimplementedContract(t *testing.T) {
	t.Parallel()

	server := NewServer(fakeService{})
	_, err := server.RegisterProviderArtifactSignal(context.Background(), &providersv1.RegisterProviderArtifactSignalRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("RegisterProviderArtifactSignal() code = %s, want unimplemented", status.Code(err))
	}
}

func TestRecordProviderLimitSnapshotMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	commandID := uuid.NewString()
	capturedAt := time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)
	remaining := int64(42)
	response, err := NewServer(fakeService{}).RecordProviderLimitSnapshot(context.Background(), &providersv1.RecordProviderLimitSnapshotRequest{
		ExternalAccountId: accountID.String(),
		ProviderSlug:      "github",
		LimitClass:        "core",
		Remaining:         &remaining,
		CapturedAt:        capturedAt.Format(time.RFC3339Nano),
		Source:            "provider_hub",
		Meta:              &providersv1.CommandMeta{CommandId: &commandID, RequestId: "req-1"},
	})
	if err != nil {
		t.Fatalf("RecordProviderLimitSnapshot(): %v", err)
	}
	if response.GetLimitSnapshot().GetExternalAccountId() != accountID.String() {
		t.Fatalf("external account = %s, want %s", response.GetLimitSnapshot().GetExternalAccountId(), accountID)
	}
	if response.GetLimitSnapshot().GetRemaining() != remaining {
		t.Fatalf("remaining = %d, want %d", response.GetLimitSnapshot().GetRemaining(), remaining)
	}
}

func TestIngestWebhookEventMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.NewString()
	receivedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	response, err := NewServer(fakeService{}).IngestWebhookEvent(context.Background(), &providersv1.IngestWebhookEventRequest{
		ProviderSlug: "github",
		DeliveryId:   "delivery-1",
		EventName:    "issues",
		PayloadJson:  `{"issue":{"id":1}}`,
		ReceivedAt:   receivedAt.Format(time.RFC3339Nano),
		Meta:         &providersv1.CommandMeta{CommandId: &commandID, RequestId: "req-1"},
	})
	if err != nil {
		t.Fatalf("IngestWebhookEvent(): %v", err)
	}
	if response.GetWebhookEvent().GetDeliveryId() != "delivery-1" {
		t.Fatalf("delivery id = %s, want delivery-1", response.GetWebhookEvent().GetDeliveryId())
	}
	if response.GetWebhookEvent().GetProcessingStatus() != providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_PROCESSED {
		t.Fatalf("status = %s, want processed", response.GetWebhookEvent().GetProcessingStatus())
	}
}

func TestEnqueueReconciliationMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.NewString()
	response, err := NewServer(fakeService{}).EnqueueReconciliation(context.Background(), &providersv1.EnqueueReconciliationRequest{
		ProviderSlug:  "github",
		ScopeType:     providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_REPOSITORY,
		ScopeRef:      "codex-k8s/kodex",
		ArtifactKinds: []providersv1.SyncArtifactKind{providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_ISSUE},
		Priority:      providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_HOT,
		Meta:          &providersv1.CommandMeta{CommandId: &commandID, RequestId: "req-1"},
	})
	if err != nil {
		t.Fatalf("EnqueueReconciliation(): %v", err)
	}
	if len(response.GetSyncCursors()) != 1 {
		t.Fatalf("sync cursors = %d, want 1", len(response.GetSyncCursors()))
	}
	cursor := response.GetSyncCursors()[0]
	if cursor.GetScopeRef() != "codex-k8s/kodex" || cursor.GetArtifactKind() != providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_ISSUE {
		t.Fatalf("cursor = %+v, want issue cursor", cursor)
	}
}

func TestGetWorkItemProjectionMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	projectionID := uuid.New()
	response, err := NewServer(fakeService{}).GetWorkItemProjection(context.Background(), &providersv1.GetWorkItemProjectionRequest{
		WorkItemProjectionId: projectionID.String(),
		Meta:                 &providersv1.QueryMeta{Actor: &providersv1.Actor{Type: "user", Id: uuid.NewString()}},
	})
	if err != nil {
		t.Fatalf("GetWorkItemProjection(): %v", err)
	}
	projection := response.GetWorkItemProjection()
	if projection.GetWorkItemProjectionId() != projectionID.String() {
		t.Fatalf("projection id = %s, want %s", projection.GetWorkItemProjectionId(), projectionID)
	}
	if projection.GetKind() != providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE {
		t.Fatalf("kind = %s, want issue", projection.GetKind())
	}
	if len(projection.GetLabels()) != 1 || projection.GetLabels()[0] != "bug" {
		t.Fatalf("labels = %+v, want bug", projection.GetLabels())
	}
}

func TestNewServerPanicsWithoutService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer() did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestUnaryErrorInterceptorMapsDomainErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "invalid argument", err: errs.ErrInvalidArgument, code: codes.InvalidArgument},
		{name: "forbidden", err: errs.ErrForbidden, code: codes.PermissionDenied},
		{name: "not found", err: errs.ErrNotFound, code: codes.NotFound},
		{name: "already exists", err: errs.ErrAlreadyExists, code: codes.AlreadyExists},
		{name: "conflict", err: errs.ErrConflict, code: codes.Aborted},
		{name: "precondition", err: errs.ErrPreconditionFailed, code: codes.FailedPrecondition},
		{name: "dependency", err: errs.ErrDependencyUnavailable, code: codes.Unavailable},
		{name: "unknown", err: errors.New("boom"), code: codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			interceptor := UnaryErrorInterceptor(nil)
			info := &grpcruntime.UnaryServerInfo{FullMethod: "/kodex.providers.v1.ProviderHubService/Test"}
			_, err := interceptor(context.Background(), nil, info, func(context.Context, any) (any, error) {
				return nil, tc.err
			})
			if status.Code(err) != tc.code {
				t.Fatalf("status code = %s, want %s", status.Code(err), tc.code)
			}
		})
	}
}

type fakeService struct{}

func (fakeService) IngestWebhookEvent(_ context.Context, input providerservice.IngestWebhookEventInput) (entity.WebhookEvent, error) {
	return entity.WebhookEvent{
		ID:               uuid.New(),
		ProviderSlug:     input.ProviderSlug,
		DeliveryID:       input.DeliveryID,
		EventName:        input.EventName,
		ReceivedAt:       input.ReceivedAt,
		ProcessingStatus: enum.WebhookProcessingStatusProcessed,
		PayloadJSON:      input.PayloadJSON,
		RetainUntil:      input.ReceivedAt.Add(24 * time.Hour),
	}, nil
}

func (fakeService) GetWebhookEvent(context.Context, providerservice.GetWebhookEventInput) (entity.WebhookEvent, error) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	return entity.WebhookEvent{
		ID:               uuid.New(),
		ProviderSlug:     enum.ProviderSlugGitHub,
		DeliveryID:       "delivery-1",
		EventName:        "issues",
		ReceivedAt:       now,
		ProcessingStatus: enum.WebhookProcessingStatusProcessed,
		PayloadJSON:      []byte(`{}`),
		RetainUntil:      now.Add(24 * time.Hour),
	}, nil
}

func (fakeService) GetWorkItemProjection(_ context.Context, input providerservice.GetWorkItemProjectionInput) (entity.ProviderWorkItemProjection, error) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	return entity.ProviderWorkItemProjection{
		Base:               entity.Base{ID: input.WorkItemProjectionID, Version: 1, CreatedAt: now, UpdatedAt: now},
		ProviderSlug:       enum.ProviderSlugGitHub,
		ProviderWorkItemID: "55",
		RepositoryFullName: "codex-k8s/kodex",
		Kind:               enum.WorkItemKindIssue,
		Number:             7,
		URL:                "https://github.com/codex-k8s/kodex/issues/7",
		Title:              "Issue",
		State:              "open",
		LabelsJSON:         []byte(`["bug"]`),
		AssigneesJSON:      []byte(`["kodex-agent"]`),
		WatermarkStatus:    enum.WorkItemWatermarkStatusValid,
		ProviderUpdatedAt:  &now,
		SyncedAt:           now,
		DriftStatus:        enum.WorkItemDriftStatusFresh,
	}, nil
}

func (fakeService) FindWorkItemByProviderRef(context.Context, providerservice.FindWorkItemByProviderRefInput) (entity.ProviderWorkItemProjection, error) {
	return fakeService{}.GetWorkItemProjection(context.Background(), providerservice.GetWorkItemProjectionInput{WorkItemProjectionID: uuid.New()})
}

func (fakeService) ListWorkItemProjections(context.Context, providerservice.ListWorkItemProjectionsInput) (providerservice.ListWorkItemProjectionsResult, error) {
	item, err := fakeService{}.GetWorkItemProjection(context.Background(), providerservice.GetWorkItemProjectionInput{WorkItemProjectionID: uuid.New()})
	if err != nil {
		return providerservice.ListWorkItemProjectionsResult{}, err
	}
	return providerservice.ListWorkItemProjectionsResult{
		WorkItemProjections: []entity.ProviderWorkItemProjection{item},
		Page:                query.PageResult{},
	}, nil
}

func (fakeService) ListComments(context.Context, providerservice.ListCommentsInput) (providerservice.ListCommentsResult, error) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	return providerservice.ListCommentsResult{
		Comments: []entity.ProviderCommentProjection{{
			Base:                entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ProviderCommentID:   "900",
			Kind:                enum.CommentKindComment,
			AuthorProviderLogin: "kodex-agent",
			BodyDigest:          "digest",
			Summary:             "comment",
			ProviderCreatedAt:   &now,
			ProviderUpdatedAt:   &now,
		}},
		Page: query.PageResult{},
	}, nil
}

func (fakeService) ListRelationships(context.Context, providerservice.ListRelationshipsInput) (providerservice.ListRelationshipsResult, error) {
	return providerservice.ListRelationshipsResult{
		Relationships: []entity.ProviderRelationship{{
			ID:                uuid.New(),
			SourceWorkItemID:  uuid.New(),
			TargetProviderRef: "https://github.com/codex-k8s/kodex/pull/8",
			RelationshipType:  "source",
			Source:            enum.RelationshipSourceWatermark,
			Confidence:        enum.RelationshipConfidenceConfirmed,
			CreatedAt:         time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		}},
		Page: query.PageResult{},
	}, nil
}

func (fakeService) EnqueueReconciliation(_ context.Context, input providerservice.EnqueueReconciliationInput) (providerservice.EnqueueReconciliationResult, error) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cursors := make([]entity.SyncCursor, 0, len(input.ArtifactKinds))
	for _, artifactKind := range input.ArtifactKinds {
		cursors = append(cursors, entity.SyncCursor{
			Base:                entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ProviderSlug:        input.ProviderSlug,
			ScopeType:           input.ScopeType,
			ScopeRef:            input.ScopeRef,
			ArtifactKind:        artifactKind,
			Priority:            input.Priority,
			RateBudgetStateJSON: []byte(`{}`),
		})
	}
	return providerservice.EnqueueReconciliationResult{SyncCursors: cursors}, nil
}

func (fakeService) RunReconciliationBatch(_ context.Context, input providerservice.RunReconciliationBatchInput) (providerservice.RunReconciliationBatchResult, error) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cursorID := uuid.New()
	if input.SyncCursorID != nil {
		cursorID = *input.SyncCursorID
	}
	leaseUntil := now.Add(30 * time.Second)
	return providerservice.RunReconciliationBatchResult{
		SyncCursor: entity.SyncCursor{
			Base:                entity.Base{ID: cursorID, Version: 2, CreatedAt: now, UpdatedAt: now},
			ProviderSlug:        enum.ProviderSlugGitHub,
			ScopeType:           enum.SyncCursorScopeRepository,
			ScopeRef:            "codex-k8s/kodex",
			ArtifactKind:        enum.SyncArtifactIssue,
			Priority:            enum.SyncCursorPriorityHot,
			LastCheckedAt:       &now,
			RateBudgetStateJSON: []byte(`{}`),
			LeaseOwner:          input.LeaseOwner,
			LeaseUntil:          &leaseUntil,
		},
	}, nil
}

func (fakeService) GetSyncCursor(_ context.Context, input providerservice.GetSyncCursorInput) (entity.SyncCursor, error) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	return entity.SyncCursor{
		Base:                entity.Base{ID: input.SyncCursorID, Version: 1, CreatedAt: now, UpdatedAt: now},
		ProviderSlug:        enum.ProviderSlugGitHub,
		ScopeType:           enum.SyncCursorScopeRepository,
		ScopeRef:            "codex-k8s/kodex",
		ArtifactKind:        enum.SyncArtifactIssue,
		Priority:            enum.SyncCursorPriorityHot,
		RateBudgetStateJSON: []byte(`{}`),
	}, nil
}

func (fakeService) ListSyncCursors(context.Context, providerservice.ListSyncCursorsInput) (providerservice.ListSyncCursorsResult, error) {
	cursor, err := fakeService{}.GetSyncCursor(context.Background(), providerservice.GetSyncCursorInput{SyncCursorID: uuid.New()})
	if err != nil {
		return providerservice.ListSyncCursorsResult{}, err
	}
	return providerservice.ListSyncCursorsResult{SyncCursors: []entity.SyncCursor{cursor}, Page: query.PageResult{}}, nil
}

func (fakeService) ListWebhookEvents(context.Context, providerservice.ListWebhookEventsInput) (providerservice.ListWebhookEventsResult, error) {
	return providerservice.ListWebhookEventsResult{Page: query.PageResult{}}, nil
}

func (fakeService) RetryWebhookEventProcessing(ctx context.Context, input providerservice.RetryWebhookEventProcessingInput) (entity.WebhookEvent, error) {
	return fakeService{}.GetWebhookEvent(ctx, providerservice.GetWebhookEventInput{WebhookEventID: input.WebhookEventID})
}

func (fakeService) GetProviderAccountRuntimeState(context.Context, providerservice.GetProviderAccountRuntimeStateInput) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{
		Base:              entity.Base{ID: uuid.New(), Version: 1},
		ExternalAccountID: uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		Status:            enum.ProviderAccountRuntimeStatusActive,
	}, nil
}

func (fakeService) ListProviderAccountRuntimeStates(context.Context, providerservice.ListProviderAccountRuntimeStatesInput) (providerservice.ListProviderAccountRuntimeStatesResult, error) {
	return providerservice.ListProviderAccountRuntimeStatesResult{
		RuntimeStates: []entity.ProviderAccountRuntimeState{{
			Base:              entity.Base{ID: uuid.New(), Version: 1},
			ExternalAccountID: uuid.New(),
			ProviderSlug:      enum.ProviderSlugGitHub,
			Status:            enum.ProviderAccountRuntimeStatusActive,
		}},
		Page: query.PageResult{},
	}, nil
}

func (fakeService) RecordProviderLimitSnapshot(_ context.Context, input providerservice.RecordProviderLimitSnapshotInput) (entity.ProviderLimitSnapshot, error) {
	return entity.ProviderLimitSnapshot{
		ID:                uuid.New(),
		ExternalAccountID: input.ExternalAccountID,
		ProviderSlug:      input.ProviderSlug,
		LimitClass:        input.LimitClass,
		Remaining:         input.Remaining,
		LimitValue:        input.LimitValue,
		ResetAt:           input.ResetAt,
		CapturedAt:        input.CapturedAt,
		Source:            input.Source,
	}, nil
}

func (fakeService) ListProviderLimitSnapshots(context.Context, providerservice.ListProviderLimitSnapshotsInput) (providerservice.ListProviderLimitSnapshotsResult, error) {
	return providerservice.ListProviderLimitSnapshotsResult{Page: query.PageResult{}}, nil
}

func (fakeService) ListProviderOperations(context.Context, providerservice.ListProviderOperationsInput) (providerservice.ListProviderOperationsResult, error) {
	return providerservice.ListProviderOperationsResult{Page: query.PageResult{}}, nil
}
