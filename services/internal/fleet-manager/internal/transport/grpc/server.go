// Package grpc exposes fleet-manager through the generated gRPC contract.
package grpc

import (
	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	grpcruntime "google.golang.org/grpc"
)

var _ fleetv1.FleetManagerServiceServer = (*Server)(nil)

// Server exposes the generated fleet-manager service.
//
// FLEET-2 wires the transport process only. Business operations are implemented
// in later slices and intentionally inherit generated Unimplemented responses.
type Server struct {
	fleetv1.UnimplementedFleetManagerServiceServer
}

// NewServer creates a fleet-manager gRPC transport skeleton.
func NewServer() *Server {
	return &Server{}
}

// RegisterFleetManagerService registers fleet-manager gRPC handlers.
func RegisterFleetManagerService(registrar grpcruntime.ServiceRegistrar) {
	fleetv1.RegisterFleetManagerServiceServer(registrar, NewServer())
}
