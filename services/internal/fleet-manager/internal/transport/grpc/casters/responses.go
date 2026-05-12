package casters

import (
	"time"

	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
)

// FleetScopeResponse maps one fleet scope aggregate to gRPC.
func FleetScopeResponse(scope entity.FleetScope) *fleetv1.FleetScopeResponse {
	return &fleetv1.FleetScopeResponse{Scope: FleetScopeToProto(scope)}
}

func FleetScopeToProto(scope entity.FleetScope) *fleetv1.FleetScope {
	return &fleetv1.FleetScope{
		Id:           scope.ID.String(),
		ScopeKey:     scope.ScopeKey,
		ScopeType:    FleetScopeTypeToProto(scope.ScopeType),
		ScopeOwnerId: uuidPtrString(scope.ScopeOwnerID),
		OwnerRefJson: string(scope.OwnerRefJSON),
		DisplayName:  scope.DisplayName,
		Status:       FleetScopeStatusToProto(scope.Status),
		IsDefault:    scope.IsDefault,
		CreatedAt:    formatTime(scope.CreatedAt),
		UpdatedAt:    formatTime(scope.UpdatedAt),
		Version:      scope.Version,
	}
}

func ListFleetScopesResponse(result fleetservice.ListFleetScopesResult) *fleetv1.ListFleetScopesResponse {
	return &fleetv1.ListFleetScopesResponse{Scopes: mapSlice(result.Scopes, FleetScopeToProto), Page: pageResponseToProto(result.Page)}
}

// ServerResponse maps one server aggregate to gRPC.
func ServerResponse(server entity.Server) *fleetv1.ServerResponse {
	return &fleetv1.ServerResponse{Server: ServerToProto(server)}
}

func ServerToProto(server entity.Server) *fleetv1.Server {
	return &fleetv1.Server{
		Id:                server.ID.String(),
		ServerKey:         server.ServerKey,
		ProviderType:      ServerProviderTypeToProto(server.ProviderType),
		Status:            ServerStatusToProto(server.Status),
		PrimaryAddressRef: server.PrimaryAddressRef,
		Region:            server.Region,
		CapacityClass:     server.CapacityClass,
		SecretStoreType:   server.SecretStoreType,
		SecretStoreRef:    server.SecretStoreRef,
		CreatedAt:         formatTime(server.CreatedAt),
		UpdatedAt:         formatTime(server.UpdatedAt),
		Version:           server.Version,
	}
}

func ListServersResponse(result fleetservice.ListServersResult) *fleetv1.ListServersResponse {
	return &fleetv1.ListServersResponse{Servers: mapSlice(result.Servers, ServerToProto), Page: pageResponseToProto(result.Page)}
}

// KubernetesClusterResponse maps one Kubernetes cluster aggregate to gRPC.
func KubernetesClusterResponse(cluster entity.KubernetesCluster) *fleetv1.KubernetesClusterResponse {
	return &fleetv1.KubernetesClusterResponse{Cluster: KubernetesClusterToProto(cluster)}
}

func KubernetesClusterToProto(cluster entity.KubernetesCluster) *fleetv1.KubernetesCluster {
	return &fleetv1.KubernetesCluster{
		Id:                  cluster.ID.String(),
		FleetScopeId:        cluster.FleetScopeID.String(),
		ServerId:            uuidPtrString(cluster.ServerID),
		ClusterKey:          cluster.ClusterKey,
		Status:              KubernetesClusterStatusToProto(cluster.Status),
		IsDefault:           cluster.IsDefault,
		ApiEndpointRef:      cluster.APIEndpointRef,
		SecretStoreType:     cluster.SecretStoreType,
		SecretStoreRef:      cluster.SecretStoreRef,
		KubernetesVersion:   cluster.KubernetesVersion,
		Region:              cluster.Region,
		CapacityClass:       cluster.CapacityClass,
		LastHealthStatus:    ClusterHealthStatusToProto(cluster.LastHealthStatus),
		LastHealthCheckedAt: timePtrString(cluster.LastHealthCheckedAt),
		CreatedAt:           formatTime(cluster.CreatedAt),
		UpdatedAt:           formatTime(cluster.UpdatedAt),
		Version:             cluster.Version,
	}
}

func ListKubernetesClustersResponse(result fleetservice.ListKubernetesClustersResult) *fleetv1.ListKubernetesClustersResponse {
	return &fleetv1.ListKubernetesClustersResponse{Clusters: mapSlice(result.Clusters, KubernetesClusterToProto), Page: pageResponseToProto(result.Page)}
}

// ClusterConnectivityCheckResponse maps one connectivity check to gRPC.
func ClusterConnectivityCheckResponse(check entity.ClusterConnectivityCheck) *fleetv1.ClusterConnectivityCheckResponse {
	return &fleetv1.ClusterConnectivityCheckResponse{ConnectivityCheck: ClusterConnectivityCheckToProto(check)}
}

func ClusterConnectivityCheckToProto(check entity.ClusterConnectivityCheck) *fleetv1.ClusterConnectivityCheck {
	return &fleetv1.ClusterConnectivityCheck{
		Id:           check.ID.String(),
		ClusterId:    check.ClusterID.String(),
		Status:       ConnectivityCheckStatusToProto(check.Status),
		StartedAt:    timePtrString(check.StartedAt),
		FinishedAt:   timePtrString(check.FinishedAt),
		LatencyMs:    int64Ptr(check.LatencyMS),
		ErrorCode:    check.ErrorCode,
		ErrorMessage: check.ErrorMessage,
		CreatedAt:    formatTime(check.CreatedAt),
	}
}

// ClusterHealthSnapshotResponse maps one health snapshot to gRPC.
func ClusterHealthSnapshotResponse(snapshot entity.ClusterHealthSnapshot) *fleetv1.ClusterHealthSnapshotResponse {
	return &fleetv1.ClusterHealthSnapshotResponse{HealthSnapshot: ClusterHealthSnapshotToProto(snapshot)}
}

func ClusterHealthSnapshotToProto(snapshot entity.ClusterHealthSnapshot) *fleetv1.ClusterHealthSnapshot {
	return &fleetv1.ClusterHealthSnapshot{
		Id:             snapshot.ID.String(),
		ClusterId:      snapshot.ClusterID.String(),
		HealthStatus:   ClusterHealthStatusToProto(snapshot.HealthStatus),
		CapacityStatus: CapacityStatusToProto(snapshot.CapacityStatus),
		SummaryJson:    string(snapshot.SummaryJSON),
		CheckedAt:      formatTime(snapshot.CheckedAt),
		ErrorCode:      snapshot.ErrorCode,
		ErrorMessage:   snapshot.ErrorMessage,
	}
}

func ListClusterHealthSnapshotsResponse(result fleetservice.ListClusterHealthSnapshotsResult) *fleetv1.ListClusterHealthSnapshotsResponse {
	return &fleetv1.ListClusterHealthSnapshotsResponse{HealthSnapshots: mapSlice(result.Snapshots, ClusterHealthSnapshotToProto), Page: pageResponseToProto(result.Page)}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func timePtrString(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func int64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func mapSlice[I any, O any](items []I, convert func(I) *O) []*O {
	result := make([]*O, 0, len(items))
	for index := range items {
		result = append(result, convert(items[index]))
	}
	return result
}
