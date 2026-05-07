// Package grpc exposes runtime-manager through the generated gRPC contract.
package grpc

import (
	"context"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	grpccasters "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ runtimev1.RuntimeManagerServiceServer = (*Server)(nil)

type runtimeService interface {
	ReserveSlot(context.Context, runtimeservice.ReserveSlotInput) (entity.Slot, error)
	ExtendSlotLease(context.Context, runtimeservice.ExtendSlotLeaseInput) (entity.Slot, error)
	ReleaseSlot(context.Context, runtimeservice.ReleaseSlotInput) (entity.Slot, error)
	MarkSlotFailed(context.Context, runtimeservice.MarkSlotFailedInput) (entity.Slot, error)
	GetSlot(context.Context, runtimeservice.GetSlotInput) (entity.Slot, error)
	ListSlots(context.Context, runtimeservice.ListSlotsInput) (runtimeservice.ListSlotsResult, error)
}

// Server exposes the generated runtime-manager service.
type Server struct {
	runtimev1.UnimplementedRuntimeManagerServiceServer
	service runtimeService
}

// NewServer creates a runtime-manager gRPC transport.
func NewServer(service runtimeService) *Server {
	if service == nil {
		panic("runtime-manager grpc service is required")
	}
	return &Server{service: service}
}

// RegisterRuntimeManagerService registers runtime-manager gRPC handlers.
func RegisterRuntimeManagerService(registrar grpcruntime.ServiceRegistrar, service runtimeService) {
	runtimev1.RegisterRuntimeManagerServiceServer(registrar, NewServer(service))
}

// ReserveSlot reserves or creates a runtime slot.
func (s *Server) ReserveSlot(ctx context.Context, request *runtimev1.ReserveSlotRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReserveSlotInput, s.service.ReserveSlot, grpccasters.SlotResponse)
}

// ExtendSlotLease extends active slot lease.
func (s *Server) ExtendSlotLease(ctx context.Context, request *runtimev1.ExtendSlotLeaseRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ExtendSlotLeaseInput, s.service.ExtendSlotLease, grpccasters.SlotResponse)
}

// ReleaseSlot releases a slot after work finishes.
func (s *Server) ReleaseSlot(ctx context.Context, request *runtimev1.ReleaseSlotRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ReleaseSlotInput, s.service.ReleaseSlot, grpccasters.SlotResponse)
}

// MarkSlotFailed marks a slot as failed with a classified error.
func (s *Server) MarkSlotFailed(ctx context.Context, request *runtimev1.MarkSlotFailedRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.MarkSlotFailedInput, s.service.MarkSlotFailed, grpccasters.SlotResponse)
}

// GetSlot returns authoritative slot state.
func (s *Server) GetSlot(ctx context.Context, request *runtimev1.GetSlotRequest) (*runtimev1.SlotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetSlotInput, s.service.GetSlot, grpccasters.SlotResponse)
}

// ListSlots returns slots by project, status, runtime profile or fleet scope.
func (s *Server) ListSlots(ctx context.Context, request *runtimev1.ListSlotsRequest) (*runtimev1.ListSlotsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListSlotsInput, s.service.ListSlots, grpccasters.ListSlotsResponse)
}
