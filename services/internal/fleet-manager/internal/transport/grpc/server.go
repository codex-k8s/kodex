// Package grpc exposes fleet-manager through the generated gRPC contract.
package grpc

import (
	"context"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
	grpccasters "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ fleetv1.FleetManagerServiceServer = (*Server)(nil)

type fleetService interface {
	CreateFleetScope(context.Context, fleetservice.CreateFleetScopeInput) (entity.FleetScope, error)
	UpdateFleetScope(context.Context, fleetservice.UpdateFleetScopeInput) (entity.FleetScope, error)
	DisableFleetScope(context.Context, uuid.UUID, value.CommandMeta) (entity.FleetScope, error)
	EnableFleetScope(context.Context, uuid.UUID, value.CommandMeta) (entity.FleetScope, error)
	GetFleetScope(context.Context, uuid.UUID, value.QueryMeta) (entity.FleetScope, error)
	ListFleetScopes(context.Context, fleetservice.ListFleetScopesInput) (fleetservice.ListFleetScopesResult, error)
	RegisterServer(context.Context, fleetservice.RegisterServerInput) (entity.Server, error)
	UpdateServer(context.Context, fleetservice.UpdateServerInput) (entity.Server, error)
	DisableServer(context.Context, uuid.UUID, value.CommandMeta) (entity.Server, error)
	EnableServer(context.Context, uuid.UUID, value.CommandMeta) (entity.Server, error)
	GetServer(context.Context, uuid.UUID, value.QueryMeta) (entity.Server, error)
	ListServers(context.Context, fleetservice.ListServersInput) (fleetservice.ListServersResult, error)
	RegisterKubernetesCluster(context.Context, fleetservice.RegisterKubernetesClusterInput) (entity.KubernetesCluster, error)
	UpdateKubernetesCluster(context.Context, fleetservice.UpdateKubernetesClusterInput) (entity.KubernetesCluster, error)
	DisableKubernetesCluster(context.Context, uuid.UUID, value.CommandMeta) (entity.KubernetesCluster, error)
	EnableKubernetesCluster(context.Context, uuid.UUID, value.CommandMeta) (entity.KubernetesCluster, error)
	GetKubernetesCluster(context.Context, uuid.UUID, value.QueryMeta) (entity.KubernetesCluster, error)
	ListKubernetesClusters(context.Context, fleetservice.ListKubernetesClustersInput) (fleetservice.ListKubernetesClustersResult, error)
	RunClusterConnectivityCheck(context.Context, fleetservice.RunClusterConnectivityCheckInput) (entity.ClusterConnectivityCheck, error)
	GetClusterHealthSnapshot(context.Context, fleetservice.GetClusterHealthSnapshotInput) (entity.ClusterHealthSnapshot, error)
	ListClusterHealthSnapshots(context.Context, fleetservice.ListClusterHealthSnapshotsInput) (fleetservice.ListClusterHealthSnapshotsResult, error)
}

// Server implements the generated FleetManagerServiceServer contract.
type Server struct {
	fleetv1.UnimplementedFleetManagerServiceServer
	service fleetService
}

// NewServer creates a fleet-manager gRPC transport around domain use cases.
func NewServer(service fleetService) *Server {
	if service == nil {
		panic("fleet-manager grpc service is required")
	}
	return &Server{service: service}
}

// RegisterFleetManagerService registers fleet-manager gRPC handlers.
func RegisterFleetManagerService(registrar grpcruntime.ServiceRegistrar, service fleetService) {
	fleetv1.RegisterFleetManagerServiceServer(registrar, NewServer(service))
}

// CreateFleetScope creates a logical placement scope.
func (s *Server) CreateFleetScope(ctx context.Context, request *fleetv1.CreateFleetScopeRequest) (*fleetv1.FleetScopeResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.CreateFleetScopeInput, s.service.CreateFleetScope, grpccasters.FleetScopeResponse)
}

// UpdateFleetScope changes safe scope fields and lifecycle status.
func (s *Server) UpdateFleetScope(ctx context.Context, request *fleetv1.UpdateFleetScopeRequest) (*fleetv1.FleetScopeResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateFleetScopeInput, s.service.UpdateFleetScope, grpccasters.FleetScopeResponse)
}

// DisableFleetScope disables new placements in a scope.
func (s *Server) DisableFleetScope(ctx context.Context, request *fleetv1.DisableFleetScopeRequest) (*fleetv1.FleetScopeResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.DisableFleetScopeInput, s.service.DisableFleetScope, grpccasters.FleetScopeResponse)
}

// EnableFleetScope allows new placements in a disabled scope.
func (s *Server) EnableFleetScope(ctx context.Context, request *fleetv1.EnableFleetScopeRequest) (*fleetv1.FleetScopeResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.EnableFleetScopeInput, s.service.EnableFleetScope, grpccasters.FleetScopeResponse)
}

// GetFleetScope returns authoritative scope state.
func (s *Server) GetFleetScope(ctx context.Context, request *fleetv1.GetFleetScopeRequest) (*fleetv1.FleetScopeResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.GetFleetScopeInput, s.service.GetFleetScope, grpccasters.FleetScopeResponse)
}

// ListFleetScopes returns scopes by type, owner, status or default flag.
func (s *Server) ListFleetScopes(ctx context.Context, request *fleetv1.ListFleetScopesRequest) (*fleetv1.ListFleetScopesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListFleetScopesInput, s.service.ListFleetScopes, grpccasters.ListFleetScopesResponse)
}

// RegisterServer registers a server or external host reference.
func (s *Server) RegisterServer(ctx context.Context, request *fleetv1.RegisterServerRequest) (*fleetv1.ServerResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RegisterServerInput, s.service.RegisterServer, grpccasters.ServerResponse)
}

// UpdateServer changes safe server metadata and lifecycle status.
func (s *Server) UpdateServer(ctx context.Context, request *fleetv1.UpdateServerRequest) (*fleetv1.ServerResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateServerInput, s.service.UpdateServer, grpccasters.ServerResponse)
}

// DisableServer disables server use for new placements.
func (s *Server) DisableServer(ctx context.Context, request *fleetv1.DisableServerRequest) (*fleetv1.ServerResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.DisableServerInput, s.service.DisableServer, grpccasters.ServerResponse)
}

// EnableServer allows new placements through a previously disabled server.
func (s *Server) EnableServer(ctx context.Context, request *fleetv1.EnableServerRequest) (*fleetv1.ServerResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.EnableServerInput, s.service.EnableServer, grpccasters.ServerResponse)
}

// GetServer returns authoritative server state.
func (s *Server) GetServer(ctx context.Context, request *fleetv1.GetServerRequest) (*fleetv1.ServerResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.GetServerInput, s.service.GetServer, grpccasters.ServerResponse)
}

// ListServers returns servers by status, provider, region or capacity class.
func (s *Server) ListServers(ctx context.Context, request *fleetv1.ListServersRequest) (*fleetv1.ListServersResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListServersInput, s.service.ListServers, grpccasters.ListServersResponse)
}

// RegisterKubernetesCluster registers one Kubernetes cluster in a fleet scope.
func (s *Server) RegisterKubernetesCluster(ctx context.Context, request *fleetv1.RegisterKubernetesClusterRequest) (*fleetv1.KubernetesClusterResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RegisterKubernetesClusterInput, s.service.RegisterKubernetesCluster, grpccasters.KubernetesClusterResponse)
}

// UpdateKubernetesCluster changes safe cluster metadata and lifecycle status.
func (s *Server) UpdateKubernetesCluster(ctx context.Context, request *fleetv1.UpdateKubernetesClusterRequest) (*fleetv1.KubernetesClusterResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdateKubernetesClusterInput, s.service.UpdateKubernetesCluster, grpccasters.KubernetesClusterResponse)
}

// DisableKubernetesCluster disables new placements in a cluster.
func (s *Server) DisableKubernetesCluster(ctx context.Context, request *fleetv1.DisableKubernetesClusterRequest) (*fleetv1.KubernetesClusterResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.DisableKubernetesClusterInput, s.service.DisableKubernetesCluster, grpccasters.KubernetesClusterResponse)
}

// EnableKubernetesCluster allows new placements in a previously disabled cluster.
func (s *Server) EnableKubernetesCluster(ctx context.Context, request *fleetv1.EnableKubernetesClusterRequest) (*fleetv1.KubernetesClusterResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.EnableKubernetesClusterInput, s.service.EnableKubernetesCluster, grpccasters.KubernetesClusterResponse)
}

// GetKubernetesCluster returns authoritative cluster state.
func (s *Server) GetKubernetesCluster(ctx context.Context, request *fleetv1.GetKubernetesClusterRequest) (*fleetv1.KubernetesClusterResponse, error) {
	return grpcserver.HandleUnaryPair(ctx, request, grpccasters.GetKubernetesClusterInput, s.service.GetKubernetesCluster, grpccasters.KubernetesClusterResponse)
}

// ListKubernetesClusters returns clusters by scope, server, status, region or health.
func (s *Server) ListKubernetesClusters(ctx context.Context, request *fleetv1.ListKubernetesClustersRequest) (*fleetv1.ListKubernetesClustersResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListKubernetesClustersInput, s.service.ListKubernetesClusters, grpccasters.ListKubernetesClustersResponse)
}

// RunClusterConnectivityCheck verifies cluster connectivity and stores a health snapshot.
func (s *Server) RunClusterConnectivityCheck(ctx context.Context, request *fleetv1.RunClusterConnectivityCheckRequest) (*fleetv1.ClusterConnectivityCheckResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RunClusterConnectivityCheckInput, s.service.RunClusterConnectivityCheck, grpccasters.ClusterConnectivityCheckResponse)
}

// GetClusterHealthSnapshot returns latest or selected health state.
func (s *Server) GetClusterHealthSnapshot(ctx context.Context, request *fleetv1.GetClusterHealthSnapshotRequest) (*fleetv1.ClusterHealthSnapshotResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetClusterHealthSnapshotInput, s.service.GetClusterHealthSnapshot, grpccasters.ClusterHealthSnapshotResponse)
}

// ListClusterHealthSnapshots returns cluster health history.
func (s *Server) ListClusterHealthSnapshots(ctx context.Context, request *fleetv1.ListClusterHealthSnapshotsRequest) (*fleetv1.ListClusterHealthSnapshotsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListClusterHealthSnapshotsInput, s.service.ListClusterHealthSnapshots, grpccasters.ListClusterHealthSnapshotsResponse)
}
