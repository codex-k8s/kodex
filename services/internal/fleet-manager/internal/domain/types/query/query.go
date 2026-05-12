// Package query contains fleet-manager read filters and idempotency lookups.
package query

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// PageResult describes list continuation state.
type PageResult = value.PageResult

// CommandIdentity identifies a previously applied idempotent command.
type CommandIdentity struct {
	CommandID      uuid.UUID
	IdempotencyKey string
	Operation      string
	Actor          value.Actor
}

// FleetScopeFilter selects fleet scopes for authoritative reads.
type FleetScopeFilter struct {
	ScopeTypes   []enum.FleetScopeType
	Statuses     []enum.FleetScopeStatus
	ScopeOwnerID *uuid.UUID
	IsDefault    *bool
	Page         value.PageRequest
}

// ServerFilter selects servers for authoritative reads.
type ServerFilter struct {
	Statuses      []enum.ServerStatus
	ProviderTypes []enum.ServerProviderType
	Region        string
	CapacityClass string
	Page          value.PageRequest
}

// KubernetesClusterFilter selects Kubernetes clusters for authoritative reads.
type KubernetesClusterFilter struct {
	FleetScopeID   *uuid.UUID
	ServerID       *uuid.UUID
	Statuses       []enum.KubernetesClusterStatus
	HealthStatuses []enum.ClusterHealthStatus
	Region         string
	CapacityClass  string
	IsDefault      *bool
	Page           value.PageRequest
}

// ClusterHealthSnapshotFilter selects health snapshots for authoritative reads.
type ClusterHealthSnapshotFilter struct {
	ClusterID    uuid.UUID
	CheckedSince *time.Time
	Page         value.PageRequest
}

// PlacementRuleFilter selects placement rules for authoritative reads.
type PlacementRuleFilter struct {
	FleetScopeID *uuid.UUID
	Statuses     []enum.PlacementRuleStatus
	Page         value.PageRequest
}

// PlacementDecisionFilter selects placement decisions for authoritative reads.
type PlacementDecisionFilter struct {
	ProjectID    *uuid.UUID
	RepositoryID *uuid.UUID
	FleetScopeID *uuid.UUID
	ClusterID    *uuid.UUID
	Statuses     []enum.PlacementDecisionStatus
	Page         value.PageRequest
}
