// Package service implements fleet-manager domain use cases.
package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// CreateFleetScopeInput contains fields required to create a fleet scope.
type CreateFleetScopeInput struct {
	ScopeKey     string
	ScopeType    enum.FleetScopeType
	ScopeOwnerID *uuid.UUID
	OwnerRefJSON []byte
	DisplayName  string
	IsDefault    bool
	Meta         value.CommandMeta
}

// UpdateFleetScopeInput changes safe fleet scope fields.
type UpdateFleetScopeInput struct {
	FleetScopeID    uuid.UUID
	ScopeKey        *string
	ScopeOwnerID    *uuid.UUID
	ScopeOwnerIDSet bool
	OwnerRefJSON    *[]byte
	DisplayName     *string
	Status          enum.FleetScopeStatus
	IsDefault       *bool
	Meta            value.CommandMeta
}

// ListFleetScopesInput selects fleet scopes.
type ListFleetScopesInput struct {
	ScopeTypes   []enum.FleetScopeType
	Statuses     []enum.FleetScopeStatus
	ScopeOwnerID *uuid.UUID
	IsDefault    *bool
	Page         value.PageRequest
	Meta         value.QueryMeta
}

// ListFleetScopesResult returns fleet scopes and paging metadata.
type ListFleetScopesResult struct {
	Scopes []entity.FleetScope
	Page   value.PageResult
}

// RegisterServerInput contains fields required to register a server.
type RegisterServerInput struct {
	ServerKey         string
	ProviderType      enum.ServerProviderType
	PrimaryAddressRef string
	Region            string
	CapacityClass     string
	SecretStoreType   string
	SecretStoreRef    string
	Meta              value.CommandMeta
}

// UpdateServerInput changes safe server fields.
type UpdateServerInput struct {
	ServerID          uuid.UUID
	ServerKey         *string
	ProviderType      enum.ServerProviderType
	Status            enum.ServerStatus
	PrimaryAddressRef *string
	Region            *string
	CapacityClass     *string
	SecretStoreType   *string
	SecretStoreRef    *string
	Meta              value.CommandMeta
}

// ListServersInput selects servers.
type ListServersInput struct {
	Statuses      []enum.ServerStatus
	ProviderTypes []enum.ServerProviderType
	Region        string
	CapacityClass string
	Page          value.PageRequest
	Meta          value.QueryMeta
}

// ListServersResult returns servers and paging metadata.
type ListServersResult struct {
	Servers []entity.Server
	Page    value.PageResult
}

// RegisterKubernetesClusterInput contains fields required to register a Kubernetes cluster.
type RegisterKubernetesClusterInput struct {
	FleetScopeID      uuid.UUID
	ServerID          *uuid.UUID
	ClusterKey        string
	IsDefault         bool
	APIEndpointRef    string
	SecretStoreType   string
	SecretStoreRef    string
	KubernetesVersion string
	Region            string
	CapacityClass     string
	Meta              value.CommandMeta
}

// UpdateKubernetesClusterInput changes safe Kubernetes cluster fields.
type UpdateKubernetesClusterInput struct {
	ClusterID         uuid.UUID
	FleetScopeID      *uuid.UUID
	ServerID          *uuid.UUID
	ServerIDSet       bool
	ClusterKey        *string
	Status            enum.KubernetesClusterStatus
	IsDefault         *bool
	APIEndpointRef    *string
	SecretStoreType   *string
	SecretStoreRef    *string
	KubernetesVersion *string
	Region            *string
	CapacityClass     *string
	Meta              value.CommandMeta
}

// ListKubernetesClustersInput selects Kubernetes clusters.
type ListKubernetesClustersInput struct {
	FleetScopeID   *uuid.UUID
	ServerID       *uuid.UUID
	Statuses       []enum.KubernetesClusterStatus
	HealthStatuses []enum.ClusterHealthStatus
	Region         string
	CapacityClass  string
	IsDefault      *bool
	Page           value.PageRequest
	Meta           value.QueryMeta
}

// ListKubernetesClustersResult returns Kubernetes clusters and paging metadata.
type ListKubernetesClustersResult struct {
	Clusters []entity.KubernetesCluster
	Page     value.PageResult
}

// PlatformDefaultSeed describes bootstrap data for a single-install default path.
type PlatformDefaultSeed struct {
	FleetScopeID      uuid.UUID
	ClusterID         uuid.UUID
	ScopeKey          string
	ScopeDisplayName  string
	ClusterKey        string
	APIEndpointRef    string
	SecretStoreType   string
	SecretStoreRef    string
	KubernetesVersion string
	Region            string
	CapacityClass     string
}
