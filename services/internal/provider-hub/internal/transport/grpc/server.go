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
	GetProviderAccountRuntimeState(context.Context, providerservice.GetProviderAccountRuntimeStateInput) (entity.ProviderAccountRuntimeState, error)
	ListProviderAccountRuntimeStates(context.Context, providerservice.ListProviderAccountRuntimeStatesInput) (providerservice.ListProviderAccountRuntimeStatesResult, error)
	RecordProviderLimitSnapshot(context.Context, providerservice.RecordProviderLimitSnapshotInput) (entity.ProviderLimitSnapshot, error)
	ListProviderLimitSnapshots(context.Context, providerservice.ListProviderLimitSnapshotsInput) (providerservice.ListProviderLimitSnapshotsResult, error)
	ListProviderOperations(context.Context, providerservice.ListProviderOperationsInput) (providerservice.ListProviderOperationsResult, error)
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
