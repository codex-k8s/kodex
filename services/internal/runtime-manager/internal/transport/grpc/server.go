// Package grpc exposes runtime-manager through the generated gRPC contract.
package grpc

import (
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	grpcruntime "google.golang.org/grpc"
)

var _ runtimev1.RuntimeManagerServiceServer = (*Server)(nil)

// Server exposes the generated runtime-manager service.
type Server struct {
	runtimev1.UnimplementedRuntimeManagerServiceServer
}

// NewServer creates a runtime-manager gRPC transport.
func NewServer() *Server {
	return &Server{}
}

// RegisterRuntimeManagerService registers runtime-manager gRPC handlers.
func RegisterRuntimeManagerService(registrar grpcruntime.ServiceRegistrar) {
	runtimev1.RegisterRuntimeManagerServiceServer(registrar, NewServer())
}
