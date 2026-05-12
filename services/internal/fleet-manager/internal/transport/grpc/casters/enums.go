package casters

import (
	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
)

type domainEnum interface {
	~string
}

var fleetScopeTypes = map[fleetv1.FleetScopeType]enum.FleetScopeType{
	fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_PLATFORM:     enum.FleetScopeTypePlatform,
	fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_ORGANIZATION: enum.FleetScopeTypeOrganization,
	fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_PROJECT:      enum.FleetScopeTypeProject,
	fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_REPOSITORY:   enum.FleetScopeTypeRepository,
	fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_SERVICE:      enum.FleetScopeTypeService,
}

var fleetScopeStatuses = map[fleetv1.FleetScopeStatus]enum.FleetScopeStatus{
	fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_ACTIVE:    enum.FleetScopeStatusActive,
	fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_SUSPENDED: enum.FleetScopeStatusSuspended,
	fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_DRAINING:  enum.FleetScopeStatusDraining,
	fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_ARCHIVED:  enum.FleetScopeStatusArchived,
}

var serverProviderTypes = map[fleetv1.ServerProviderType]enum.ServerProviderType{
	fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_BARE_METAL: enum.ServerProviderTypeBareMetal,
	fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_VPS:        enum.ServerProviderTypeVPS,
	fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_CLOUD:      enum.ServerProviderTypeCloud,
	fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_MANAGED:    enum.ServerProviderTypeManaged,
	fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_UNKNOWN:    enum.ServerProviderTypeUnknown,
}

var serverStatuses = map[fleetv1.ServerStatus]enum.ServerStatus{
	fleetv1.ServerStatus_SERVER_STATUS_ACTIVE:    enum.ServerStatusActive,
	fleetv1.ServerStatus_SERVER_STATUS_SUSPENDED: enum.ServerStatusSuspended,
	fleetv1.ServerStatus_SERVER_STATUS_DRAINING:  enum.ServerStatusDraining,
}

var kubernetesClusterStatuses = map[fleetv1.KubernetesClusterStatus]enum.KubernetesClusterStatus{
	fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_ACTIVE:      enum.KubernetesClusterStatusActive,
	fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_SUSPENDED:   enum.KubernetesClusterStatusSuspended,
	fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_DRAINING:    enum.KubernetesClusterStatusDraining,
	fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_UNREACHABLE: enum.KubernetesClusterStatusUnreachable,
}

var clusterHealthStatuses = map[fleetv1.ClusterHealthStatus]enum.ClusterHealthStatus{
	fleetv1.ClusterHealthStatus_CLUSTER_HEALTH_STATUS_UNKNOWN:   enum.ClusterHealthStatusUnknown,
	fleetv1.ClusterHealthStatus_CLUSTER_HEALTH_STATUS_HEALTHY:   enum.ClusterHealthStatusHealthy,
	fleetv1.ClusterHealthStatus_CLUSTER_HEALTH_STATUS_DEGRADED:  enum.ClusterHealthStatusDegraded,
	fleetv1.ClusterHealthStatus_CLUSTER_HEALTH_STATUS_UNHEALTHY: enum.ClusterHealthStatusUnhealthy,
}

var capacityStatuses = map[fleetv1.CapacityStatus]enum.CapacityStatus{
	fleetv1.CapacityStatus_CAPACITY_STATUS_UNKNOWN:   enum.CapacityStatusUnknown,
	fleetv1.CapacityStatus_CAPACITY_STATUS_OK:        enum.CapacityStatusOK,
	fleetv1.CapacityStatus_CAPACITY_STATUS_LIMITED:   enum.CapacityStatusLimited,
	fleetv1.CapacityStatus_CAPACITY_STATUS_EXHAUSTED: enum.CapacityStatusExhausted,
}

var connectivityCheckStatuses = map[fleetv1.ConnectivityCheckStatus]enum.ConnectivityCheckStatus{
	fleetv1.ConnectivityCheckStatus_CONNECTIVITY_CHECK_STATUS_PENDING:   enum.ConnectivityCheckStatusPending,
	fleetv1.ConnectivityCheckStatus_CONNECTIVITY_CHECK_STATUS_RUNNING:   enum.ConnectivityCheckStatusRunning,
	fleetv1.ConnectivityCheckStatus_CONNECTIVITY_CHECK_STATUS_SUCCEEDED: enum.ConnectivityCheckStatusSucceeded,
	fleetv1.ConnectivityCheckStatus_CONNECTIVITY_CHECK_STATUS_FAILED:    enum.ConnectivityCheckStatusFailed,
	fleetv1.ConnectivityCheckStatus_CONNECTIVITY_CHECK_STATUS_TIMED_OUT: enum.ConnectivityCheckStatusTimedOut,
}

func fleetScopeTypeFromProto(value fleetv1.FleetScopeType) (enum.FleetScopeType, error) {
	return enumFromProto(value, fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_UNSPECIFIED, fleetScopeTypes, false)
}

func FleetScopeTypeToProto(value enum.FleetScopeType) fleetv1.FleetScopeType {
	return enumToProto(value, fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_UNSPECIFIED, invertEnum(fleetScopeTypes))
}

func fleetScopeStatusFromProto(value fleetv1.FleetScopeStatus) (enum.FleetScopeStatus, error) {
	return enumFromProto(value, fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_UNSPECIFIED, fleetScopeStatuses, true)
}

func FleetScopeStatusToProto(value enum.FleetScopeStatus) fleetv1.FleetScopeStatus {
	return enumToProto(value, fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_UNSPECIFIED, invertEnum(fleetScopeStatuses))
}

func serverProviderTypeFromProto(value fleetv1.ServerProviderType) (enum.ServerProviderType, error) {
	return enumFromProto(value, fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_UNSPECIFIED, serverProviderTypes, true)
}

func ServerProviderTypeToProto(value enum.ServerProviderType) fleetv1.ServerProviderType {
	return enumToProto(value, fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_UNSPECIFIED, invertEnum(serverProviderTypes))
}

func serverStatusFromProto(value fleetv1.ServerStatus) (enum.ServerStatus, error) {
	return enumFromProto(value, fleetv1.ServerStatus_SERVER_STATUS_UNSPECIFIED, serverStatuses, true)
}

func ServerStatusToProto(value enum.ServerStatus) fleetv1.ServerStatus {
	return enumToProto(value, fleetv1.ServerStatus_SERVER_STATUS_UNSPECIFIED, invertEnum(serverStatuses))
}

func kubernetesClusterStatusFromProto(value fleetv1.KubernetesClusterStatus) (enum.KubernetesClusterStatus, error) {
	return enumFromProto(value, fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_UNSPECIFIED, kubernetesClusterStatuses, true)
}

func KubernetesClusterStatusToProto(value enum.KubernetesClusterStatus) fleetv1.KubernetesClusterStatus {
	return enumToProto(value, fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_UNSPECIFIED, invertEnum(kubernetesClusterStatuses))
}

func clusterHealthStatusesFromProto(values []fleetv1.ClusterHealthStatus) ([]enum.ClusterHealthStatus, error) {
	return enumsFromProto(values, clusterHealthStatuses, fleetv1.ClusterHealthStatus_CLUSTER_HEALTH_STATUS_UNSPECIFIED)
}

func ClusterHealthStatusToProto(value enum.ClusterHealthStatus) fleetv1.ClusterHealthStatus {
	return enumToProto(value, fleetv1.ClusterHealthStatus_CLUSTER_HEALTH_STATUS_UNSPECIFIED, invertEnum(clusterHealthStatuses))
}

func CapacityStatusToProto(value enum.CapacityStatus) fleetv1.CapacityStatus {
	return enumToProto(value, fleetv1.CapacityStatus_CAPACITY_STATUS_UNSPECIFIED, invertEnum(capacityStatuses))
}

func ConnectivityCheckStatusToProto(value enum.ConnectivityCheckStatus) fleetv1.ConnectivityCheckStatus {
	return enumToProto(value, fleetv1.ConnectivityCheckStatus_CONNECTIVITY_CHECK_STATUS_UNSPECIFIED, invertEnum(connectivityCheckStatuses))
}

func fleetScopeTypesFromProto(values []fleetv1.FleetScopeType) ([]enum.FleetScopeType, error) {
	return enumsFromProto(values, fleetScopeTypes, fleetv1.FleetScopeType_FLEET_SCOPE_TYPE_UNSPECIFIED)
}

func fleetScopeStatusesFromProto(values []fleetv1.FleetScopeStatus) ([]enum.FleetScopeStatus, error) {
	return enumsFromProto(values, fleetScopeStatuses, fleetv1.FleetScopeStatus_FLEET_SCOPE_STATUS_UNSPECIFIED)
}

func serverProviderTypesFromProto(values []fleetv1.ServerProviderType) ([]enum.ServerProviderType, error) {
	return enumsFromProto(values, serverProviderTypes, fleetv1.ServerProviderType_SERVER_PROVIDER_TYPE_UNSPECIFIED)
}

func serverStatusesFromProto(values []fleetv1.ServerStatus) ([]enum.ServerStatus, error) {
	return enumsFromProto(values, serverStatuses, fleetv1.ServerStatus_SERVER_STATUS_UNSPECIFIED)
}

func kubernetesClusterStatusesFromProto(values []fleetv1.KubernetesClusterStatus) ([]enum.KubernetesClusterStatus, error) {
	return enumsFromProto(values, kubernetesClusterStatuses, fleetv1.KubernetesClusterStatus_KUBERNETES_CLUSTER_STATUS_UNSPECIFIED)
}

func enumFromProto[P comparable, D domainEnum](value P, unspecified P, values map[P]D, optional bool) (D, error) {
	if value == unspecified {
		if optional {
			var zero D
			return zero, nil
		}
		return "", errs.ErrInvalidArgument
	}
	result, ok := values[value]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return result, nil
}

func enumToProto[P comparable, D domainEnum](value D, unspecified P, values map[D]P) P {
	result, ok := values[value]
	if !ok {
		return unspecified
	}
	return result
}

func enumsFromProto[P comparable, D domainEnum](items []P, values map[P]D, unspecified P) ([]D, error) {
	result := make([]D, 0, len(items))
	for index := range items {
		converted, err := enumFromProto(items[index], unspecified, values, false)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func invertEnum[P comparable, D domainEnum](values map[P]D) map[D]P {
	inverted := make(map[D]P, len(values))
	for protoValue, domainValue := range values {
		inverted[domainValue] = protoValue
	}
	return inverted
}
