package grpc

import (
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	grpcruntime "google.golang.org/grpc"
)

var _ projectsv1.ProjectCatalogServiceServer = (*Server)(nil)

// Server implements the generated ProjectCatalogServiceServer contract.
type Server struct {
	projectsv1.UnimplementedProjectCatalogServiceServer
}

// NewServer creates a project-catalog gRPC transport shell.
func NewServer() *Server {
	return &Server{}
}

// RegisterProjectCatalogService registers project-catalog gRPC handlers.
func RegisterProjectCatalogService(registrar grpcruntime.ServiceRegistrar) {
	projectsv1.RegisterProjectCatalogServiceServer(registrar, NewServer())
}
