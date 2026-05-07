package grpc

import (
	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	grpcruntime "google.golang.org/grpc"
)

var _ packagesv1.PackageHubServiceServer = (*Server)(nil)

// Server exposes PackageHubService over gRPC.
type Server struct {
	packagesv1.UnimplementedPackageHubServiceServer

	service *packageservice.Service
}

// NewServer creates a package-hub gRPC server boundary.
func NewServer(service *packageservice.Service) *Server {
	if service == nil {
		service = packageservice.New()
	}
	return &Server{service: service}
}

// RegisterPackageHubService registers package-hub handlers in a gRPC runtime.
func RegisterPackageHubService(registrar grpcruntime.ServiceRegistrar, service *packageservice.Service) {
	packagesv1.RegisterPackageHubServiceServer(registrar, NewServer(service))
}

func (server *Server) ready() bool {
	return server != nil && server.service != nil
}
