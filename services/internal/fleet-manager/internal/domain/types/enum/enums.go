// Package enum contains closed string sets owned by fleet-manager.
package enum

// FleetScopeStatus describes whether a scope can receive new placements.
type FleetScopeStatus string

const (
	FleetScopeStatusActive    FleetScopeStatus = "active"
	FleetScopeStatusSuspended FleetScopeStatus = "suspended"
	FleetScopeStatusDraining  FleetScopeStatus = "draining"
	FleetScopeStatusArchived  FleetScopeStatus = "archived"
)

// FleetScopeType describes the owner level for a placement scope.
type FleetScopeType string

const (
	FleetScopeTypePlatform     FleetScopeType = "platform"
	FleetScopeTypeOrganization FleetScopeType = "organization"
	FleetScopeTypeProject      FleetScopeType = "project"
	FleetScopeTypeRepository   FleetScopeType = "repository"
	FleetScopeTypeService      FleetScopeType = "service"
)

// ServerProviderType describes how a server is sourced.
type ServerProviderType string

const (
	ServerProviderTypeBareMetal ServerProviderType = "bare_metal"
	ServerProviderTypeVPS       ServerProviderType = "vps"
	ServerProviderTypeCloud     ServerProviderType = "cloud"
	ServerProviderTypeManaged   ServerProviderType = "managed"
	ServerProviderTypeUnknown   ServerProviderType = "unknown"
)

// ServerStatus describes whether a server can back active clusters.
type ServerStatus string

const (
	ServerStatusActive    ServerStatus = "active"
	ServerStatusSuspended ServerStatus = "suspended"
	ServerStatusDraining  ServerStatus = "draining"
)

// KubernetesClusterStatus describes whether a cluster can receive runtime placements.
type KubernetesClusterStatus string

const (
	KubernetesClusterStatusActive      KubernetesClusterStatus = "active"
	KubernetesClusterStatusSuspended   KubernetesClusterStatus = "suspended"
	KubernetesClusterStatusDraining    KubernetesClusterStatus = "draining"
	KubernetesClusterStatusUnreachable KubernetesClusterStatus = "unreachable"
)

// ClusterHealthStatus summarizes the latest known cluster health.
type ClusterHealthStatus string

const (
	ClusterHealthStatusHealthy   ClusterHealthStatus = "healthy"
	ClusterHealthStatusDegraded  ClusterHealthStatus = "degraded"
	ClusterHealthStatusUnhealthy ClusterHealthStatus = "unhealthy"
	ClusterHealthStatusUnknown   ClusterHealthStatus = "unknown"
)

// CapacityStatus summarizes whether a cluster can accept more work.
type CapacityStatus string

const (
	CapacityStatusOK        CapacityStatus = "ok"
	CapacityStatusLimited   CapacityStatus = "limited"
	CapacityStatusExhausted CapacityStatus = "exhausted"
	CapacityStatusUnknown   CapacityStatus = "unknown"
)

// ConnectivityCheckStatus describes one Kubernetes API connectivity attempt.
type ConnectivityCheckStatus string

const (
	ConnectivityCheckStatusPending   ConnectivityCheckStatus = "pending"
	ConnectivityCheckStatusRunning   ConnectivityCheckStatus = "running"
	ConnectivityCheckStatusSucceeded ConnectivityCheckStatus = "succeeded"
	ConnectivityCheckStatusFailed    ConnectivityCheckStatus = "failed"
	ConnectivityCheckStatusTimedOut  ConnectivityCheckStatus = "timed_out"
)

// RuntimeMode describes the requested runtime isolation mode for placement.
type RuntimeMode string

const (
	RuntimeModeCodeOnly           RuntimeMode = "code_only"
	RuntimeModeFullEnv            RuntimeMode = "full_env"
	RuntimeModeReadOnlyProduction RuntimeMode = "read_only_production"
	RuntimeModePlatformJob        RuntimeMode = "platform_job"
)

// PlacementRuleStatus describes whether a rule participates in placement resolution.
type PlacementRuleStatus string

const (
	PlacementRuleStatusActive   PlacementRuleStatus = "active"
	PlacementRuleStatusDisabled PlacementRuleStatus = "disabled"
	PlacementRuleStatusArchived PlacementRuleStatus = "archived"
)

// PlacementDecisionStatus describes the result of placement resolution.
type PlacementDecisionStatus string

const (
	PlacementDecisionStatusResolved PlacementDecisionStatus = "resolved"
	PlacementDecisionStatusRejected PlacementDecisionStatus = "rejected"
)
