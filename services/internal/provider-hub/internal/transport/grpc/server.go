package grpc

import (
	"context"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	grpccasters "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ providersv1.ProviderHubServiceServer = (*Server)(nil)

type providerService interface {
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

type unaryCaster[Request any, Input any] func(*Request) (Input, error)
type unaryCaller[Input any, Output any] func(context.Context, Input) (Output, error)
type unaryResponder[Output any, Response any] func(Output) *Response

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

// GetProviderAccountRuntimeState returns provider runtime state for one external account.
func (s *Server) GetProviderAccountRuntimeState(ctx context.Context, request *providersv1.GetProviderAccountRuntimeStateRequest) (*providersv1.ProviderAccountRuntimeStateResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetProviderAccountRuntimeStateInput, s.service.GetProviderAccountRuntimeState, grpccasters.ProviderAccountRuntimeStateResponse)
}

// ListProviderAccountRuntimeStates returns provider runtime states by supported filters.
func (s *Server) ListProviderAccountRuntimeStates(ctx context.Context, request *providersv1.ListProviderAccountRuntimeStatesRequest) (*providersv1.ListProviderAccountRuntimeStatesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListProviderAccountRuntimeStatesInput, s.service.ListProviderAccountRuntimeStates, grpccasters.ListProviderAccountRuntimeStatesResponse)
}

// RecordProviderLimitSnapshot records known provider limits after an operation or signal.
func (s *Server) RecordProviderLimitSnapshot(ctx context.Context, request *providersv1.RecordProviderLimitSnapshotRequest) (*providersv1.ProviderLimitSnapshotResponse, error) {
	return handleUnary(ctx, request, grpccasters.RecordProviderLimitSnapshotInput, s.service.RecordProviderLimitSnapshot, grpccasters.ProviderLimitSnapshotResponse)
}

// ListProviderLimitSnapshots returns recorded provider limit snapshots.
func (s *Server) ListProviderLimitSnapshots(ctx context.Context, request *providersv1.ListProviderLimitSnapshotsRequest) (*providersv1.ListProviderLimitSnapshotsResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListProviderLimitSnapshotsInput, s.service.ListProviderLimitSnapshots, grpccasters.ListProviderLimitSnapshotsResponse)
}

// ListProviderOperations returns the operation log for diagnostics and audit.
func (s *Server) ListProviderOperations(ctx context.Context, request *providersv1.ListProviderOperationsRequest) (*providersv1.ListProviderOperationsResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListProviderOperationsInput, s.service.ListProviderOperations, grpccasters.ListProviderOperationsResponse)
}

func handleUnary[Request any, Input any, Output any, Response any](
	ctx context.Context,
	request *Request,
	cast unaryCaster[Request, Input],
	call unaryCaller[Input, Output],
	respond unaryResponder[Output, Response],
) (*Response, error) {
	domainInput, err := cast(request)
	if err != nil {
		return nil, err
	}
	domainOutput, err := call(ctx, domainInput)
	if err != nil {
		return nil, err
	}
	return respond(domainOutput), nil
}
