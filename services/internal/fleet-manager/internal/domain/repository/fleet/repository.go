// Package fleet defines persistence ports owned by the fleet-manager domain.
package fleet

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
)

// Clock provides deterministic timestamps for domain commands.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides deterministic identifiers for aggregates and events.
type IDGenerator interface {
	New() uuid.UUID
}

// Repository is the domain persistence contract for fleet-manager infrastructure state.
type Repository interface {
	// Ping checks that the fleet database is reachable.
	Ping(ctx context.Context) error
	// GetCommandResult returns an applied idempotent command result.
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	// CreateFleetScope stores a new fleet scope, command result and event atomically.
	CreateFleetScope(ctx context.Context, scope entity.FleetScope, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateFleetScope stores a versioned fleet scope mutation, command result and event atomically.
	UpdateFleetScope(ctx context.Context, scope entity.FleetScope, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error
	// GetFleetScope returns a fleet scope by id.
	GetFleetScope(ctx context.Context, id uuid.UUID) (entity.FleetScope, error)
	// ListFleetScopes returns fleet scopes by filter.
	ListFleetScopes(ctx context.Context, filter query.FleetScopeFilter) ([]entity.FleetScope, query.PageResult, error)
	// RegisterServer stores a new server, command result and event atomically.
	RegisterServer(ctx context.Context, server entity.Server, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateServer stores a versioned server mutation, command result and event atomically.
	UpdateServer(ctx context.Context, server entity.Server, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error
	// GetServer returns a server by id.
	GetServer(ctx context.Context, id uuid.UUID) (entity.Server, error)
	// ListServers returns servers by filter.
	ListServers(ctx context.Context, filter query.ServerFilter) ([]entity.Server, query.PageResult, error)
	// RegisterKubernetesCluster stores a new Kubernetes cluster, command result and event atomically.
	RegisterKubernetesCluster(ctx context.Context, cluster entity.KubernetesCluster, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateKubernetesCluster stores a versioned Kubernetes cluster mutation, command result and event atomically.
	UpdateKubernetesCluster(ctx context.Context, cluster entity.KubernetesCluster, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error
	// GetKubernetesCluster returns a Kubernetes cluster by id.
	GetKubernetesCluster(ctx context.Context, id uuid.UUID) (entity.KubernetesCluster, error)
	// ListKubernetesClusters returns Kubernetes clusters by filter.
	ListKubernetesClusters(ctx context.Context, filter query.KubernetesClusterFilter) ([]entity.KubernetesCluster, query.PageResult, error)
	// StoreClusterHealthCheck stores connectivity check, health snapshot, latest cluster health, command result and events atomically.
	StoreClusterHealthCheck(ctx context.Context, cluster entity.KubernetesCluster, check entity.ClusterConnectivityCheck, snapshot entity.ClusterHealthSnapshot, events []entity.OutboxEvent, result entity.CommandResult) error
	// GetClusterConnectivityCheck returns one connectivity check by id.
	GetClusterConnectivityCheck(ctx context.Context, id uuid.UUID) (entity.ClusterConnectivityCheck, error)
	// GetClusterHealthSnapshot returns one health snapshot by id.
	GetClusterHealthSnapshot(ctx context.Context, id uuid.UUID) (entity.ClusterHealthSnapshot, error)
	// GetLatestClusterHealthSnapshot returns the newest health snapshot for one cluster.
	GetLatestClusterHealthSnapshot(ctx context.Context, clusterID uuid.UUID) (entity.ClusterHealthSnapshot, error)
	// ListClusterHealthSnapshots returns health snapshots by filter.
	ListClusterHealthSnapshots(ctx context.Context, filter query.ClusterHealthSnapshotFilter) ([]entity.ClusterHealthSnapshot, query.PageResult, error)
	// EnsurePlatformDefaultSeed stores bootstrap default fleet data if it is absent.
	EnsurePlatformDefaultSeed(ctx context.Context, scope entity.FleetScope, cluster entity.KubernetesCluster, events []entity.OutboxEvent) error
	// AppendOutboxEvent stores one fleet domain event in the local outbox.
	AppendOutboxEvent(ctx context.Context, event entity.OutboxEvent) error
	// ClaimOutboxEvents leases unpublished outbox events for delivery.
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	// MarkOutboxEventPublished marks a leased outbox event as published.
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	// MarkOutboxEventFailed schedules a leased outbox event for retry.
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	// MarkOutboxEventPermanentlyFailed moves a leased outbox event to terminal failure.
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}
