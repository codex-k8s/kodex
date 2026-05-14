package grpc

import (
	"context"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	grpccasters "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ providersv1.ProviderHubServiceServer = (*Server)(nil)

type providerService interface {
	IngestWebhookEvent(context.Context, providerservice.IngestWebhookEventInput) (entity.WebhookEvent, error)
	GetWebhookEvent(context.Context, providerservice.GetWebhookEventInput) (entity.WebhookEvent, error)
	ListWebhookEvents(context.Context, providerservice.ListWebhookEventsInput) (providerservice.ListWebhookEventsResult, error)
	RetryWebhookEventProcessing(context.Context, providerservice.RetryWebhookEventProcessingInput) (entity.WebhookEvent, error)
	GetWorkItemProjection(context.Context, providerservice.GetWorkItemProjectionInput) (entity.ProviderWorkItemProjection, error)
	FindWorkItemByProviderRef(context.Context, providerservice.FindWorkItemByProviderRefInput) (entity.ProviderWorkItemProjection, error)
	ListWorkItemProjections(context.Context, providerservice.ListWorkItemProjectionsInput) (providerservice.ListWorkItemProjectionsResult, error)
	ListComments(context.Context, providerservice.ListCommentsInput) (providerservice.ListCommentsResult, error)
	ListRelationships(context.Context, providerservice.ListRelationshipsInput) (providerservice.ListRelationshipsResult, error)
	RegisterProviderArtifactSignal(context.Context, providerservice.RegisterProviderArtifactSignalInput) (providerservice.ProviderArtifactSignalResult, error)
	EnqueueReconciliation(context.Context, providerservice.EnqueueReconciliationInput) (providerservice.EnqueueReconciliationResult, error)
	RunReconciliationBatch(context.Context, providerservice.RunReconciliationBatchInput) (providerservice.RunReconciliationBatchResult, error)
	GetSyncCursor(context.Context, providerservice.GetSyncCursorInput) (entity.SyncCursor, error)
	ListSyncCursors(context.Context, providerservice.ListSyncCursorsInput) (providerservice.ListSyncCursorsResult, error)
	GetProviderAccountRuntimeState(context.Context, providerservice.GetProviderAccountRuntimeStateInput) (entity.ProviderAccountRuntimeState, error)
	ListProviderAccountRuntimeStates(context.Context, providerservice.ListProviderAccountRuntimeStatesInput) (providerservice.ListProviderAccountRuntimeStatesResult, error)
	RecordProviderLimitSnapshot(context.Context, providerservice.RecordProviderLimitSnapshotInput) (entity.ProviderLimitSnapshot, error)
	ListProviderLimitSnapshots(context.Context, providerservice.ListProviderLimitSnapshotsInput) (providerservice.ListProviderLimitSnapshotsResult, error)
	ListProviderOperations(context.Context, providerservice.ListProviderOperationsInput) (providerservice.ListProviderOperationsResult, error)
	CreateIssue(context.Context, providerservice.CreateIssueInput) (providerservice.ProviderOperationResult, error)
	UpdateIssue(context.Context, providerservice.UpdateIssueInput) (providerservice.ProviderOperationResult, error)
	CreateComment(context.Context, providerservice.CreateCommentInput) (providerservice.ProviderOperationResult, error)
	UpdateComment(context.Context, providerservice.UpdateCommentInput) (providerservice.ProviderOperationResult, error)
	CreatePullRequest(context.Context, providerservice.CreatePullRequestInput) (providerservice.ProviderOperationResult, error)
	UpdatePullRequest(context.Context, providerservice.UpdatePullRequestInput) (providerservice.ProviderOperationResult, error)
	CreateReviewSignal(context.Context, providerservice.CreateReviewSignalInput) (providerservice.ProviderOperationResult, error)
	UpdateRelationship(context.Context, providerservice.UpdateRelationshipInput) (providerservice.ProviderOperationResult, error)
}

// Server exposes provider-hub through the generated gRPC contract.
type Server struct {
	providersv1.UnimplementedProviderHubServiceServer
	service providerService
}

// NewServer creates a provider-hub gRPC transport around domain use cases.
func NewServer(service providerService) *Server {
	if service == nil {
		panic("provider-hub service is required")
	}
	return &Server{service: service}
}

// RegisterProviderHubService registers provider-hub gRPC handlers.
func RegisterProviderHubService(registrar grpcruntime.ServiceRegistrar, service providerService) {
	providersv1.RegisterProviderHubServiceServer(registrar, NewServer(service))
}

// IngestWebhookEvent stores a verified webhook accepted by integration-gateway.
func (s *Server) IngestWebhookEvent(ctx context.Context, request *providersv1.IngestWebhookEventRequest) (*providersv1.WebhookEventResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.IngestWebhookEventInput, s.service.IngestWebhookEvent, grpccasters.WebhookEventResponse)
}

// GetWebhookEvent returns a stored webhook for diagnostics.
func (s *Server) GetWebhookEvent(ctx context.Context, request *providersv1.GetWebhookEventRequest) (*providersv1.WebhookEventResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetWebhookEventInput, s.service.GetWebhookEvent, grpccasters.WebhookEventResponse)
}

// ListWebhookEvents returns raw webhook events by operational filters.
func (s *Server) ListWebhookEvents(ctx context.Context, request *providersv1.ListWebhookEventsRequest) (*providersv1.ListWebhookEventsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListWebhookEventsInput, s.service.ListWebhookEvents, grpccasters.ListWebhookEventsResponse)
}

// RetryWebhookEventProcessing repeats normalization for a stored webhook.
func (s *Server) RetryWebhookEventProcessing(ctx context.Context, request *providersv1.RetryWebhookEventProcessingRequest) (*providersv1.WebhookEventResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RetryWebhookEventProcessingInput, s.service.RetryWebhookEventProcessing, grpccasters.WebhookEventResponse)
}

// GetWorkItemProjection returns a normalized Issue or PR/MR projection.
func (s *Server) GetWorkItemProjection(ctx context.Context, request *providersv1.GetWorkItemProjectionRequest) (*providersv1.WorkItemProjectionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetWorkItemProjectionInput, s.service.GetWorkItemProjection, grpccasters.WorkItemProjectionResponse)
}

// FindWorkItemByProviderRef finds a projection by provider-native reference.
func (s *Server) FindWorkItemByProviderRef(ctx context.Context, request *providersv1.FindWorkItemByProviderRefRequest) (*providersv1.WorkItemProjectionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.FindWorkItemByProviderRefInput, s.service.FindWorkItemByProviderRef, grpccasters.WorkItemProjectionResponse)
}

// ListWorkItemProjections returns normalized work items by supported filters.
func (s *Server) ListWorkItemProjections(ctx context.Context, request *providersv1.ListWorkItemProjectionsRequest) (*providersv1.ListWorkItemProjectionsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListWorkItemProjectionsInput, s.service.ListWorkItemProjections, grpccasters.ListWorkItemProjectionsResponse)
}

// ListComments returns normalized comments and review signals for a work item.
func (s *Server) ListComments(ctx context.Context, request *providersv1.ListCommentsRequest) (*providersv1.ListCommentsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListCommentsInput, s.service.ListComments, grpccasters.ListCommentsResponse)
}

// ListRelationships returns normalized provider-native relationships.
func (s *Server) ListRelationships(ctx context.Context, request *providersv1.ListRelationshipsRequest) (*providersv1.ListRelationshipsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListRelationshipsInput, s.service.ListRelationships, grpccasters.ListRelationshipsResponse)
}

// RegisterProviderArtifactSignal accepts an internal signal and accelerates reconciliation.
func (s *Server) RegisterProviderArtifactSignal(ctx context.Context, request *providersv1.RegisterProviderArtifactSignalRequest) (*providersv1.ProviderArtifactSignalResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RegisterProviderArtifactSignalInput, s.service.RegisterProviderArtifactSignal, grpccasters.ProviderArtifactSignalResponse)
}

// EnqueueReconciliation schedules reconciliation cursors for one provider scope.
func (s *Server) EnqueueReconciliation(ctx context.Context, request *providersv1.EnqueueReconciliationRequest) (*providersv1.ReconciliationRequestResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.EnqueueReconciliationInput, s.service.EnqueueReconciliation, grpccasters.ReconciliationRequestResponse)
}

// RunReconciliationBatch leases one cursor for a reconciliation worker.
func (s *Server) RunReconciliationBatch(ctx context.Context, request *providersv1.RunReconciliationBatchRequest) (*providersv1.RunReconciliationBatchResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RunReconciliationBatchInput, s.service.RunReconciliationBatch, grpccasters.RunReconciliationBatchResponse)
}

// GetSyncCursor returns one reconciliation cursor.
func (s *Server) GetSyncCursor(ctx context.Context, request *providersv1.GetSyncCursorRequest) (*providersv1.SyncCursorResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetSyncCursorInput, s.service.GetSyncCursor, grpccasters.SyncCursorResponse)
}

// CreateIssue records a typed provider issue creation command.
func (s *Server) CreateIssue(ctx context.Context, request *providersv1.CreateIssueRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateIssueInput, s.service.CreateIssue, grpccasters.ProviderOperationResponse)
}

// UpdateIssue records a typed provider issue update command.
func (s *Server) UpdateIssue(ctx context.Context, request *providersv1.UpdateIssueRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateIssueInput, s.service.UpdateIssue, grpccasters.ProviderOperationResponse)
}

// CreateComment records a typed provider comment creation command.
func (s *Server) CreateComment(ctx context.Context, request *providersv1.CreateCommentRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateCommentInput, s.service.CreateComment, grpccasters.ProviderOperationResponse)
}

// UpdateComment records a typed provider comment update command.
func (s *Server) UpdateComment(ctx context.Context, request *providersv1.UpdateCommentRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateCommentInput, s.service.UpdateComment, grpccasters.ProviderOperationResponse)
}

// CreatePullRequest records a typed provider PR/MR creation command.
func (s *Server) CreatePullRequest(ctx context.Context, request *providersv1.CreatePullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreatePullRequestInput, s.service.CreatePullRequest, grpccasters.ProviderOperationResponse)
}

// UpdatePullRequest records a typed provider PR/MR update command.
func (s *Server) UpdatePullRequest(ctx context.Context, request *providersv1.UpdatePullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdatePullRequestInput, s.service.UpdatePullRequest, grpccasters.ProviderOperationResponse)
}

// CreateReviewSignal records a typed provider review signal command.
func (s *Server) CreateReviewSignal(ctx context.Context, request *providersv1.CreateReviewSignalRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateReviewSignalInput, s.service.CreateReviewSignal, grpccasters.ProviderOperationResponse)
}

// UpdateRelationship records one provider relationship update command.
func (s *Server) UpdateRelationship(ctx context.Context, request *providersv1.UpdateRelationshipRequest) (*providersv1.ProviderOperationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateRelationshipInput, s.service.UpdateRelationship, grpccasters.ProviderOperationResponse)
}

// ListSyncCursors returns reconciliation cursors by supported filters.
func (s *Server) ListSyncCursors(ctx context.Context, request *providersv1.ListSyncCursorsRequest) (*providersv1.ListSyncCursorsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListSyncCursorsInput, s.service.ListSyncCursors, grpccasters.ListSyncCursorsResponse)
}

// GetProviderAccountRuntimeState returns provider runtime state for one external account.
func (s *Server) GetProviderAccountRuntimeState(ctx context.Context, request *providersv1.GetProviderAccountRuntimeStateRequest) (*providersv1.ProviderAccountRuntimeStateResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetProviderAccountRuntimeStateInput, s.service.GetProviderAccountRuntimeState, grpccasters.ProviderAccountRuntimeStateResponse)
}

// ListProviderAccountRuntimeStates returns provider runtime states by supported filters.
func (s *Server) ListProviderAccountRuntimeStates(ctx context.Context, request *providersv1.ListProviderAccountRuntimeStatesRequest) (*providersv1.ListProviderAccountRuntimeStatesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListProviderAccountRuntimeStatesInput, s.service.ListProviderAccountRuntimeStates, grpccasters.ListProviderAccountRuntimeStatesResponse)
}

// RecordProviderLimitSnapshot records known provider limits after an operation or signal.
func (s *Server) RecordProviderLimitSnapshot(ctx context.Context, request *providersv1.RecordProviderLimitSnapshotRequest) (*providersv1.ProviderLimitSnapshotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RecordProviderLimitSnapshotInput, s.service.RecordProviderLimitSnapshot, grpccasters.ProviderLimitSnapshotResponse)
}

// ListProviderLimitSnapshots returns recorded provider limit snapshots.
func (s *Server) ListProviderLimitSnapshots(ctx context.Context, request *providersv1.ListProviderLimitSnapshotsRequest) (*providersv1.ListProviderLimitSnapshotsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListProviderLimitSnapshotsInput, s.service.ListProviderLimitSnapshots, grpccasters.ListProviderLimitSnapshotsResponse)
}

// ListProviderOperations returns the operation log for diagnostics and audit.
func (s *Server) ListProviderOperations(ctx context.Context, request *providersv1.ListProviderOperationsRequest) (*providersv1.ListProviderOperationsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListProviderOperationsInput, s.service.ListProviderOperations, grpccasters.ListProviderOperationsResponse)
}
