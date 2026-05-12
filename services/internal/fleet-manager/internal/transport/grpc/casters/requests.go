package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// CreateFleetScopeInput maps a gRPC request to the domain command input.
func CreateFleetScopeInput(request *fleetv1.CreateFleetScopeRequest) (fleetservice.CreateFleetScopeInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.CreateFleetScopeInput{}, err
	}
	scopeType, err := fleetScopeTypeFromProto(request.GetScopeType())
	if err != nil {
		return fleetservice.CreateFleetScopeInput{}, err
	}
	ownerID, err := optionalUUIDPtr(request.GetScopeOwnerId())
	if err != nil {
		return fleetservice.CreateFleetScopeInput{}, err
	}
	return fleetservice.CreateFleetScopeInput{
		ScopeKey:     strings.TrimSpace(request.GetScopeKey()),
		ScopeType:    scopeType,
		ScopeOwnerID: ownerID,
		OwnerRefJSON: []byte(strings.TrimSpace(request.GetOwnerRefJson())),
		DisplayName:  strings.TrimSpace(request.GetDisplayName()),
		IsDefault:    request.GetIsDefault(),
		Meta:         meta,
	}, nil
}

// UpdateFleetScopeInput maps a gRPC request to the domain command input.
func UpdateFleetScopeInput(request *fleetv1.UpdateFleetScopeRequest) (fleetservice.UpdateFleetScopeInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.UpdateFleetScopeInput{}, err
	}
	scopeID, err := requiredUUID(request.GetFleetScopeId())
	if err != nil {
		return fleetservice.UpdateFleetScopeInput{}, err
	}
	ownerID, err := optionalUUIDPtr(request.GetScopeOwnerId())
	if err != nil {
		return fleetservice.UpdateFleetScopeInput{}, err
	}
	status, err := optionalFleetScopeStatus(request.Status)
	if err != nil {
		return fleetservice.UpdateFleetScopeInput{}, err
	}
	return fleetservice.UpdateFleetScopeInput{
		FleetScopeID:    scopeID,
		ScopeKey:        trimOptionalString(request.ScopeKey),
		ScopeOwnerID:    ownerID,
		ScopeOwnerIDSet: request.ScopeOwnerId != nil,
		OwnerRefJSON:    optionalBytes(request.OwnerRefJson),
		DisplayName:     trimOptionalString(request.DisplayName),
		Status:          status,
		IsDefault:       request.IsDefault,
		Meta:            meta,
	}, nil
}

// DisableFleetScopeInput maps a gRPC request to id and command metadata.
func DisableFleetScopeInput(request *fleetv1.DisableFleetScopeRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetFleetScopeId(), request.GetMeta())
}

// EnableFleetScopeInput maps a gRPC request to id and command metadata.
func EnableFleetScopeInput(request *fleetv1.EnableFleetScopeRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetFleetScopeId(), request.GetMeta())
}

// GetFleetScopeInput maps a gRPC request to id and query metadata.
func GetFleetScopeInput(request *fleetv1.GetFleetScopeRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetFleetScopeId(), request.GetMeta())
}

// ListFleetScopesInput maps a gRPC request to the domain read input.
func ListFleetScopesInput(request *fleetv1.ListFleetScopesRequest) (fleetservice.ListFleetScopesInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.ListFleetScopesInput{}, err
	}
	scopeTypes, err := fleetScopeTypesFromProto(request.GetScopeTypes())
	if err != nil {
		return fleetservice.ListFleetScopesInput{}, err
	}
	statuses, err := fleetScopeStatusesFromProto(request.GetStatuses())
	if err != nil {
		return fleetservice.ListFleetScopesInput{}, err
	}
	ownerID, err := optionalUUIDPtr(request.GetScopeOwnerId())
	if err != nil {
		return fleetservice.ListFleetScopesInput{}, err
	}
	return fleetservice.ListFleetScopesInput{
		ScopeTypes:   scopeTypes,
		Statuses:     statuses,
		ScopeOwnerID: ownerID,
		IsDefault:    request.IsDefault,
		Page:         pageRequestFromProto(request.GetPage()),
		Meta:         meta,
	}, nil
}

// RegisterServerInput maps a gRPC request to the domain command input.
func RegisterServerInput(request *fleetv1.RegisterServerRequest) (fleetservice.RegisterServerInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.RegisterServerInput{}, err
	}
	providerType, err := serverProviderTypeFromProto(request.GetProviderType())
	if err != nil {
		return fleetservice.RegisterServerInput{}, err
	}
	return fleetservice.RegisterServerInput{
		ServerKey:         strings.TrimSpace(request.GetServerKey()),
		ProviderType:      providerType,
		PrimaryAddressRef: strings.TrimSpace(request.GetPrimaryAddressRef()),
		Region:            strings.TrimSpace(request.GetRegion()),
		CapacityClass:     strings.TrimSpace(request.GetCapacityClass()),
		SecretStoreType:   strings.TrimSpace(request.GetSecretStoreType()),
		SecretStoreRef:    strings.TrimSpace(request.GetSecretStoreRef()),
		Meta:              meta,
	}, nil
}

// UpdateServerInput maps a gRPC request to the domain command input.
func UpdateServerInput(request *fleetv1.UpdateServerRequest) (fleetservice.UpdateServerInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.UpdateServerInput{}, err
	}
	serverID, err := requiredUUID(request.GetServerId())
	if err != nil {
		return fleetservice.UpdateServerInput{}, err
	}
	providerType, err := optionalServerProviderType(request.ProviderType)
	if err != nil {
		return fleetservice.UpdateServerInput{}, err
	}
	status, err := optionalServerStatus(request.Status)
	if err != nil {
		return fleetservice.UpdateServerInput{}, err
	}
	return fleetservice.UpdateServerInput{
		ServerID:          serverID,
		ServerKey:         trimOptionalString(request.ServerKey),
		ProviderType:      providerType,
		Status:            status,
		PrimaryAddressRef: trimOptionalString(request.PrimaryAddressRef),
		Region:            trimOptionalString(request.Region),
		CapacityClass:     trimOptionalString(request.CapacityClass),
		SecretStoreType:   trimOptionalString(request.SecretStoreType),
		SecretStoreRef:    trimOptionalString(request.SecretStoreRef),
		Meta:              meta,
	}, nil
}

func DisableServerInput(request *fleetv1.DisableServerRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetServerId(), request.GetMeta())
}

func EnableServerInput(request *fleetv1.EnableServerRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetServerId(), request.GetMeta())
}

func GetServerInput(request *fleetv1.GetServerRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetServerId(), request.GetMeta())
}

func ListServersInput(request *fleetv1.ListServersRequest) (fleetservice.ListServersInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.ListServersInput{}, err
	}
	statuses, err := serverStatusesFromProto(request.GetStatuses())
	if err != nil {
		return fleetservice.ListServersInput{}, err
	}
	providerTypes, err := serverProviderTypesFromProto(request.GetProviderTypes())
	if err != nil {
		return fleetservice.ListServersInput{}, err
	}
	return fleetservice.ListServersInput{
		Statuses:      statuses,
		ProviderTypes: providerTypes,
		Region:        strings.TrimSpace(request.GetRegion()),
		CapacityClass: strings.TrimSpace(request.GetCapacityClass()),
		Page:          pageRequestFromProto(request.GetPage()),
		Meta:          meta,
	}, nil
}

// RegisterKubernetesClusterInput maps a gRPC request to the domain command input.
func RegisterKubernetesClusterInput(request *fleetv1.RegisterKubernetesClusterRequest) (fleetservice.RegisterKubernetesClusterInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.RegisterKubernetesClusterInput{}, err
	}
	scopeID, err := requiredUUID(request.GetFleetScopeId())
	if err != nil {
		return fleetservice.RegisterKubernetesClusterInput{}, err
	}
	serverID, err := optionalUUIDPtr(request.GetServerId())
	if err != nil {
		return fleetservice.RegisterKubernetesClusterInput{}, err
	}
	return fleetservice.RegisterKubernetesClusterInput{
		FleetScopeID:      scopeID,
		ServerID:          serverID,
		ClusterKey:        strings.TrimSpace(request.GetClusterKey()),
		IsDefault:         request.GetIsDefault(),
		APIEndpointRef:    strings.TrimSpace(request.GetApiEndpointRef()),
		SecretStoreType:   strings.TrimSpace(request.GetSecretStoreType()),
		SecretStoreRef:    strings.TrimSpace(request.GetSecretStoreRef()),
		KubernetesVersion: strings.TrimSpace(request.GetKubernetesVersion()),
		Region:            strings.TrimSpace(request.GetRegion()),
		CapacityClass:     strings.TrimSpace(request.GetCapacityClass()),
		Meta:              meta,
	}, nil
}

// UpdateKubernetesClusterInput maps a gRPC request to the domain command input.
func UpdateKubernetesClusterInput(request *fleetv1.UpdateKubernetesClusterRequest) (fleetservice.UpdateKubernetesClusterInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.UpdateKubernetesClusterInput{}, err
	}
	clusterID, err := requiredUUID(request.GetClusterId())
	if err != nil {
		return fleetservice.UpdateKubernetesClusterInput{}, err
	}
	scopeID, err := optionalUUIDPtr(request.GetFleetScopeId())
	if err != nil {
		return fleetservice.UpdateKubernetesClusterInput{}, err
	}
	serverID, err := optionalUUIDPtr(request.GetServerId())
	if err != nil {
		return fleetservice.UpdateKubernetesClusterInput{}, err
	}
	status, err := optionalKubernetesClusterStatus(request.Status)
	if err != nil {
		return fleetservice.UpdateKubernetesClusterInput{}, err
	}
	return fleetservice.UpdateKubernetesClusterInput{
		ClusterID:         clusterID,
		FleetScopeID:      scopeID,
		ServerID:          serverID,
		ServerIDSet:       request.ServerId != nil,
		ClusterKey:        trimOptionalString(request.ClusterKey),
		Status:            status,
		IsDefault:         request.IsDefault,
		APIEndpointRef:    trimOptionalString(request.ApiEndpointRef),
		SecretStoreType:   trimOptionalString(request.SecretStoreType),
		SecretStoreRef:    trimOptionalString(request.SecretStoreRef),
		KubernetesVersion: trimOptionalString(request.KubernetesVersion),
		Region:            trimOptionalString(request.Region),
		CapacityClass:     trimOptionalString(request.CapacityClass),
		Meta:              meta,
	}, nil
}

func DisableKubernetesClusterInput(request *fleetv1.DisableKubernetesClusterRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetClusterId(), request.GetMeta())
}

func EnableKubernetesClusterInput(request *fleetv1.EnableKubernetesClusterRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetClusterId(), request.GetMeta())
}

func GetKubernetesClusterInput(request *fleetv1.GetKubernetesClusterRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetClusterId(), request.GetMeta())
}

func ListKubernetesClustersInput(request *fleetv1.ListKubernetesClustersRequest) (fleetservice.ListKubernetesClustersInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.ListKubernetesClustersInput{}, err
	}
	scopeID, err := optionalUUIDPtr(request.GetFleetScopeId())
	if err != nil {
		return fleetservice.ListKubernetesClustersInput{}, err
	}
	serverID, err := optionalUUIDPtr(request.GetServerId())
	if err != nil {
		return fleetservice.ListKubernetesClustersInput{}, err
	}
	statuses, err := kubernetesClusterStatusesFromProto(request.GetStatuses())
	if err != nil {
		return fleetservice.ListKubernetesClustersInput{}, err
	}
	healthStatuses, err := clusterHealthStatusesFromProto(request.GetHealthStatuses())
	if err != nil {
		return fleetservice.ListKubernetesClustersInput{}, err
	}
	return fleetservice.ListKubernetesClustersInput{
		FleetScopeID:   scopeID,
		ServerID:       serverID,
		Statuses:       statuses,
		HealthStatuses: healthStatuses,
		Region:         strings.TrimSpace(request.GetRegion()),
		CapacityClass:  strings.TrimSpace(request.GetCapacityClass()),
		IsDefault:      request.IsDefault,
		Page:           pageRequestFromProto(request.GetPage()),
		Meta:           meta,
	}, nil
}

// RunClusterConnectivityCheckInput maps a gRPC request to the domain command input.
func RunClusterConnectivityCheckInput(request *fleetv1.RunClusterConnectivityCheckRequest) (fleetservice.RunClusterConnectivityCheckInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return fleetservice.RunClusterConnectivityCheckInput{}, err
	}
	clusterID, err := requiredUUID(request.GetClusterId())
	if err != nil {
		return fleetservice.RunClusterConnectivityCheckInput{}, err
	}
	return fleetservice.RunClusterConnectivityCheckInput{ClusterID: clusterID, Meta: meta}, nil
}

// GetClusterHealthSnapshotInput maps a gRPC request to the domain read input.
func GetClusterHealthSnapshotInput(request *fleetv1.GetClusterHealthSnapshotRequest) (fleetservice.GetClusterHealthSnapshotInput, error) {
	clusterID, meta, err := idWithQueryMeta(request.GetClusterId(), request.GetMeta())
	if err != nil {
		return fleetservice.GetClusterHealthSnapshotInput{}, err
	}
	snapshotID, err := optionalUUIDPtr(request.GetHealthSnapshotId())
	if err != nil {
		return fleetservice.GetClusterHealthSnapshotInput{}, err
	}
	return fleetservice.GetClusterHealthSnapshotInput{ClusterID: clusterID, HealthSnapshotID: snapshotID, Meta: meta}, nil
}

// ListClusterHealthSnapshotsInput maps a gRPC request to the domain read input.
func ListClusterHealthSnapshotsInput(request *fleetv1.ListClusterHealthSnapshotsRequest) (fleetservice.ListClusterHealthSnapshotsInput, error) {
	clusterID, meta, err := idWithQueryMeta(request.GetClusterId(), request.GetMeta())
	if err != nil {
		return fleetservice.ListClusterHealthSnapshotsInput{}, err
	}
	checkedSince, err := optionalTimePtr(request.GetCheckedSince())
	if err != nil {
		return fleetservice.ListClusterHealthSnapshotsInput{}, err
	}
	return fleetservice.ListClusterHealthSnapshotsInput{
		ClusterID:    clusterID,
		CheckedSince: checkedSince,
		Page:         pageRequestFromProto(request.GetPage()),
		Meta:         meta,
	}, nil
}

func optionalFleetScopeStatus(value *fleetv1.FleetScopeStatus) (enum.FleetScopeStatus, error) {
	if value == nil {
		return "", nil
	}
	return fleetScopeStatusFromProto(*value)
}

func optionalServerProviderType(value *fleetv1.ServerProviderType) (enum.ServerProviderType, error) {
	if value == nil {
		return "", nil
	}
	return serverProviderTypeFromProto(*value)
}

func optionalServerStatus(value *fleetv1.ServerStatus) (enum.ServerStatus, error) {
	if value == nil {
		return "", nil
	}
	return serverStatusFromProto(*value)
}

func optionalKubernetesClusterStatus(value *fleetv1.KubernetesClusterStatus) (enum.KubernetesClusterStatus, error) {
	if value == nil {
		return "", nil
	}
	return kubernetesClusterStatusFromProto(*value)
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func optionalBytes(value *string) *[]byte {
	if value == nil {
		return nil
	}
	payload := []byte(strings.TrimSpace(*value))
	return &payload
}

func optionalTimePtr(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
