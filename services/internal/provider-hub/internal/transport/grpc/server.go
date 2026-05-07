package grpc

import (
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	grpcruntime "google.golang.org/grpc"
)

var _ providersv1.ProviderHubServiceServer = (*Server)(nil)

// Server exposes provider-hub through the generated gRPC contract.
//
// The initial scaffold registers the full contract with generated
// Unimplemented methods. Concrete operations are enabled together with domain
// logic, storage methods and provider adapters.
type Server struct {
	providersv1.UnimplementedProviderHubServiceServer
}

// NewServer creates a provider-hub gRPC transport around domain use cases.
func NewServer(service *providerservice.Service) *Server {
	if service == nil {
		panic("provider-hub service is required")
	}
	return &Server{}
}

// RegisterProviderHubService registers provider-hub gRPC handlers.
func RegisterProviderHubService(registrar grpcruntime.ServiceRegistrar, service *providerservice.Service) {
	providersv1.RegisterProviderHubServiceServer(registrar, NewServer(service))
}
