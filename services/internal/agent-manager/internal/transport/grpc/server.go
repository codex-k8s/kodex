package grpc

import (
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	grpcruntime "google.golang.org/grpc"
)

var _ agentsv1.AgentManagerServiceServer = (*Server)(nil)

// Server exposes AgentManagerService over gRPC.
type Server struct {
	agentsv1.UnimplementedAgentManagerServiceServer

	service *agentservice.Service
}

// NewServer creates an agent-manager gRPC server boundary.
func NewServer(service *agentservice.Service) *Server {
	if service == nil {
		panic("agent-manager domain service is required")
	}
	return &Server{service: service}
}

// RegisterAgentManagerService registers agent-manager handlers in a gRPC runtime.
func RegisterAgentManagerService(registrar grpcruntime.ServiceRegistrar, service *agentservice.Service) {
	agentsv1.RegisterAgentManagerServiceServer(registrar, NewServer(service))
}
