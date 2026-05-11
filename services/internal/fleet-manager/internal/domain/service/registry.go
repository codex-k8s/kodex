package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// EnsurePlatformDefaultSeed creates bootstrap default scope and cluster as fleet-owned data.
func (s *Service) EnsurePlatformDefaultSeed(ctx context.Context) error {
	now := s.clock.Now()
	scope := entity.FleetScope{
		Base:         newBase(s.seed.FleetScopeID, now),
		ScopeKey:     s.seed.ScopeKey,
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  s.seed.ScopeDisplayName,
		Status:       enum.FleetScopeStatusActive,
		IsDefault:    true,
	}
	cluster := entity.KubernetesCluster{
		Base:                newBase(s.seed.ClusterID, now),
		FleetScopeID:        scope.ID,
		ClusterKey:          s.seed.ClusterKey,
		Status:              enum.KubernetesClusterStatusActive,
		IsDefault:           true,
		APIEndpointRef:      strings.TrimSpace(s.seed.APIEndpointRef),
		SecretStoreType:     strings.TrimSpace(s.seed.SecretStoreType),
		SecretStoreRef:      strings.TrimSpace(s.seed.SecretStoreRef),
		KubernetesVersion:   strings.TrimSpace(s.seed.KubernetesVersion),
		Region:              strings.TrimSpace(s.seed.Region),
		CapacityClass:       strings.TrimSpace(s.seed.CapacityClass),
		LastHealthStatus:    enum.ClusterHealthStatusUnknown,
		LastHealthCheckedAt: nil,
	}
	scopeEvent, err := s.scopeEvent(fleetEventScopeCreated, scope)
	if err != nil {
		return err
	}
	clusterEvent, err := s.clusterEvent(fleetEventClusterCreated, cluster)
	if err != nil {
		return err
	}
	return s.repository.EnsurePlatformDefaultSeed(ctx, scope, cluster, []entity.OutboxEvent{scopeEvent, clusterEvent})
}

// CreateFleetScope creates a logical placement scope.
func (s *Service) CreateFleetScope(ctx context.Context, input CreateFleetScopeInput) (entity.FleetScope, error) {
	if err := s.authorizeCommand(ctx, input.Meta, fleetActionScopeCreate, globalFleetResource(accesscatalog.ResourceFleetScope)); err != nil {
		return entity.FleetScope{}, err
	}
	if replay, ok, err := replayAggregate(s, ctx, input.Meta, fleetOperationCreateScope, fleetAggregateScope, s.repository.GetFleetScope); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	scope := entity.FleetScope{
		Base:         newBase(s.ids.New(), now),
		ScopeKey:     trimString(input.ScopeKey),
		ScopeType:    input.ScopeType,
		ScopeOwnerID: input.ScopeOwnerID,
		OwnerRefJSON: defaultJSON(input.OwnerRefJSON),
		DisplayName:  trimString(input.DisplayName),
		Status:       enum.FleetScopeStatusActive,
		IsDefault:    input.IsDefault,
	}
	if err := validateFleetScope(scope); err != nil {
		return entity.FleetScope{}, err
	}
	return s.createScope(ctx, input.Meta, scope, fleetOperationCreateScope, fleetEventScopeCreated)
}

// UpdateFleetScope changes safe fleet scope fields.
func (s *Service) UpdateFleetScope(ctx context.Context, input UpdateFleetScopeInput) (entity.FleetScope, error) {
	current, err := s.loadScopeForMutation(ctx, input.FleetScopeID, input.Meta, fleetActionScopeUpdate, fleetOperationUpdateScope)
	if err != nil {
		return entity.FleetScope{}, err
	}
	if replay, ok, err := replayTarget(s, ctx, input.Meta, fleetOperationUpdateScope, fleetAggregateScope, input.FleetScopeID, current); ok || err != nil {
		return replay, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.FleetScope{}, err
	}
	updated := current
	updated.Base = updatedBase(current.Base, s.clock.Now())
	updated.ScopeKey = applyString(current.ScopeKey, input.ScopeKey)
	if input.ScopeOwnerIDSet {
		updated.ScopeOwnerID = input.ScopeOwnerID
	}
	updated.OwnerRefJSON = applyBytes(current.OwnerRefJSON, input.OwnerRefJSON)
	updated.DisplayName = applyString(current.DisplayName, input.DisplayName)
	if input.Status != "" {
		updated.Status = input.Status
	}
	if input.IsDefault != nil {
		updated.IsDefault = *input.IsDefault
	}
	if err := validateFleetScope(updated); err != nil {
		return entity.FleetScope{}, err
	}
	return s.updateScope(ctx, input.Meta, updated, previousVersion, fleetOperationUpdateScope, fleetEventScopeUpdated)
}

// DisableFleetScope disables new placements in a scope.
func (s *Service) DisableFleetScope(ctx context.Context, id uuid.UUID, meta value.CommandMeta) (entity.FleetScope, error) {
	return s.changeScopeStatus(ctx, id, meta, fleetActionScopeDisable, fleetOperationDisableScope, enum.FleetScopeStatusSuspended, fleetEventScopeDisabled)
}

// EnableFleetScope allows new placements in a scope.
func (s *Service) EnableFleetScope(ctx context.Context, id uuid.UUID, meta value.CommandMeta) (entity.FleetScope, error) {
	return s.changeScopeStatus(ctx, id, meta, fleetActionScopeEnable, fleetOperationEnableScope, enum.FleetScopeStatusActive, fleetEventScopeEnabled)
}

// GetFleetScope returns authoritative fleet scope state.
func (s *Service) GetFleetScope(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.FleetScope, error) {
	return getAggregate(s, ctx, id, meta, fleetActionScopeRead, s.repository.GetFleetScope, scopeResource)
}

// ListFleetScopes returns fleet scopes matching filter.
func (s *Service) ListFleetScopes(ctx context.Context, input ListFleetScopesInput) (ListFleetScopesResult, error) {
	if err := s.authorizeList(ctx, input.Meta, fleetActionScopeList, accesscatalog.ResourceFleetScope); err != nil {
		return ListFleetScopesResult{}, err
	}
	filter := query.FleetScopeFilter{}
	filter.ScopeTypes = input.ScopeTypes
	filter.Statuses = input.Statuses
	filter.ScopeOwnerID = input.ScopeOwnerID
	filter.IsDefault = input.IsDefault
	filter.Page = input.Page
	scopes, page, err := s.repository.ListFleetScopes(ctx, filter)
	return ListFleetScopesResult{Scopes: scopes, Page: page}, err
}

func (s *Service) createScope(ctx context.Context, meta value.CommandMeta, scope entity.FleetScope, operation string, eventType string) (entity.FleetScope, error) {
	return persistCreated(ctx, meta, operation, fleetAggregateScope, scope, scope.ID, scope.UpdatedAt, eventType, s.scopeEvent, s.repository.CreateFleetScope)
}

func (s *Service) updateScope(ctx context.Context, meta value.CommandMeta, scope entity.FleetScope, previousVersion int64, operation string, eventType string) (entity.FleetScope, error) {
	return persistUpdated(ctx, meta, operation, fleetAggregateScope, scope, scope.ID, scope.UpdatedAt, previousVersion, eventType, s.scopeEvent, s.repository.UpdateFleetScope)
}

func (s *Service) changeScopeStatus(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string, status enum.FleetScopeStatus, eventType string) (entity.FleetScope, error) {
	return changeStatus(ctx, s, id, meta, action, operation, fleetAggregateScope, status, eventType, s.loadScopeForMutation, setScopeStatus, s.updateScope)
}

// RegisterServer registers a managed or external server reference.
func (s *Service) RegisterServer(ctx context.Context, input RegisterServerInput) (entity.Server, error) {
	if err := s.authorizeCommand(ctx, input.Meta, fleetActionServerRegister, globalFleetResource(accesscatalog.ResourceFleetServer)); err != nil {
		return entity.Server{}, err
	}
	if replay, ok, err := replayAggregate(s, ctx, input.Meta, fleetOperationRegisterServer, fleetAggregateServer, s.repository.GetServer); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	server := entity.Server{
		Base:              newBase(s.ids.New(), now),
		ServerKey:         trimString(input.ServerKey),
		ProviderType:      defaultServerProvider(input.ProviderType),
		Status:            enum.ServerStatusActive,
		PrimaryAddressRef: trimString(input.PrimaryAddressRef),
		Region:            trimString(input.Region),
		CapacityClass:     trimString(input.CapacityClass),
		SecretStoreType:   trimString(input.SecretStoreType),
		SecretStoreRef:    trimString(input.SecretStoreRef),
	}
	if err := validateServer(server); err != nil {
		return entity.Server{}, err
	}
	return s.createServer(ctx, input.Meta, server, fleetOperationRegisterServer, fleetEventServerCreated)
}

// UpdateServer changes safe server fields.
func (s *Service) UpdateServer(ctx context.Context, input UpdateServerInput) (entity.Server, error) {
	current, err := s.loadServerForMutation(ctx, input.ServerID, input.Meta, fleetActionServerUpdate, fleetOperationUpdateServer)
	if err != nil {
		return entity.Server{}, err
	}
	if replay, ok, err := replayTarget(s, ctx, input.Meta, fleetOperationUpdateServer, fleetAggregateServer, input.ServerID, current); ok || err != nil {
		return replay, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Server{}, err
	}
	updated := current
	updated.Base = updatedBase(current.Base, s.clock.Now())
	updated.ServerKey = applyString(current.ServerKey, input.ServerKey)
	if input.ProviderType != "" {
		updated.ProviderType = input.ProviderType
	}
	if input.Status != "" {
		updated.Status = input.Status
	}
	updated.PrimaryAddressRef = applyString(current.PrimaryAddressRef, input.PrimaryAddressRef)
	updated.Region = applyString(current.Region, input.Region)
	updated.CapacityClass = applyString(current.CapacityClass, input.CapacityClass)
	updated.SecretStoreType = applyString(current.SecretStoreType, input.SecretStoreType)
	updated.SecretStoreRef = applyString(current.SecretStoreRef, input.SecretStoreRef)
	if err := validateServer(updated); err != nil {
		return entity.Server{}, err
	}
	return s.updateServer(ctx, input.Meta, updated, previousVersion, fleetOperationUpdateServer, fleetEventServerUpdated)
}

// DisableServer disables new placements through a server.
func (s *Service) DisableServer(ctx context.Context, id uuid.UUID, meta value.CommandMeta) (entity.Server, error) {
	return s.changeServerStatus(ctx, id, meta, fleetActionServerDisable, fleetOperationDisableServer, enum.ServerStatusSuspended, fleetEventServerDisabled)
}

// EnableServer allows new placements through a server.
func (s *Service) EnableServer(ctx context.Context, id uuid.UUID, meta value.CommandMeta) (entity.Server, error) {
	return s.changeServerStatus(ctx, id, meta, fleetActionServerEnable, fleetOperationEnableServer, enum.ServerStatusActive, fleetEventServerEnabled)
}

// GetServer returns authoritative server state.
func (s *Service) GetServer(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.Server, error) {
	return getAggregate(s, ctx, id, meta, fleetActionServerRead, s.repository.GetServer, serverResource)
}

// ListServers returns servers matching filter.
func (s *Service) ListServers(ctx context.Context, input ListServersInput) (ListServersResult, error) {
	if err := s.authorizeList(ctx, input.Meta, fleetActionServerList, accesscatalog.ResourceFleetServer); err != nil {
		return ListServersResult{}, err
	}
	servers, page, err := s.repository.ListServers(ctx, query.ServerFilter{
		Statuses:      input.Statuses,
		ProviderTypes: input.ProviderTypes,
		Region:        input.Region,
		CapacityClass: input.CapacityClass,
		Page:          input.Page,
	})
	return ListServersResult{Servers: servers, Page: page}, err
}

func (s *Service) createServer(ctx context.Context, meta value.CommandMeta, server entity.Server, operation string, eventType string) (entity.Server, error) {
	save := s.repository.RegisterServer
	return persistCreated(ctx, meta, operation, fleetAggregateServer, server, server.ID, server.UpdatedAt, eventType, s.serverEvent, save)
}

func (s *Service) updateServer(ctx context.Context, meta value.CommandMeta, server entity.Server, previousVersion int64, operation string, eventType string) (entity.Server, error) {
	save := s.repository.UpdateServer
	return persistUpdated(ctx, meta, operation, fleetAggregateServer, server, server.ID, server.UpdatedAt, previousVersion, eventType, s.serverEvent, save)
}

func (s *Service) changeServerStatus(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string, status enum.ServerStatus, eventType string) (entity.Server, error) {
	load := s.loadServerForMutation
	return changeStatus(ctx, s, id, meta, action, operation, fleetAggregateServer, status, eventType, load, setServerStatus, s.updateServer)
}

// RegisterKubernetesCluster registers one Kubernetes cluster.
func (s *Service) RegisterKubernetesCluster(ctx context.Context, input RegisterKubernetesClusterInput) (entity.KubernetesCluster, error) {
	if err := s.authorizeCommand(ctx, input.Meta, fleetActionClusterRegister, fleetResource(accesscatalog.ResourceFleetCluster, uuid.Nil, &input.FleetScopeID)); err != nil {
		return entity.KubernetesCluster{}, err
	}
	if replay, ok, err := replayAggregate(s, ctx, input.Meta, fleetOperationRegisterCluster, fleetAggregateCluster, s.repository.GetKubernetesCluster); ok || err != nil {
		return replay, err
	}
	if _, err := s.repository.GetFleetScope(ctx, input.FleetScopeID); err != nil {
		return entity.KubernetesCluster{}, err
	}
	if input.ServerID != nil {
		if _, err := s.repository.GetServer(ctx, *input.ServerID); err != nil {
			return entity.KubernetesCluster{}, err
		}
	}
	now := s.clock.Now()
	cluster := entity.KubernetesCluster{
		Base:                newBase(s.ids.New(), now),
		FleetScopeID:        input.FleetScopeID,
		ServerID:            input.ServerID,
		ClusterKey:          trimString(input.ClusterKey),
		Status:              enum.KubernetesClusterStatusActive,
		IsDefault:           input.IsDefault,
		APIEndpointRef:      trimString(input.APIEndpointRef),
		SecretStoreType:     trimString(input.SecretStoreType),
		SecretStoreRef:      trimString(input.SecretStoreRef),
		KubernetesVersion:   trimString(input.KubernetesVersion),
		Region:              trimString(input.Region),
		CapacityClass:       trimString(input.CapacityClass),
		LastHealthStatus:    enum.ClusterHealthStatusUnknown,
		LastHealthCheckedAt: nil,
	}
	if err := validateKubernetesCluster(cluster); err != nil {
		return entity.KubernetesCluster{}, err
	}
	return s.createCluster(ctx, input.Meta, cluster, fleetOperationRegisterCluster, fleetEventClusterCreated)
}

// UpdateKubernetesCluster changes safe Kubernetes cluster fields.
func (s *Service) UpdateKubernetesCluster(ctx context.Context, input UpdateKubernetesClusterInput) (entity.KubernetesCluster, error) {
	current, err := s.loadClusterForMutation(ctx, input.ClusterID, input.Meta, fleetActionClusterUpdate, fleetOperationUpdateCluster)
	if err != nil {
		return entity.KubernetesCluster{}, err
	}
	if replay, ok, err := replayTarget(s, ctx, input.Meta, fleetOperationUpdateCluster, fleetAggregateCluster, input.ClusterID, current); ok || err != nil {
		return replay, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.KubernetesCluster{}, err
	}
	updated := current
	updated.Base = updatedBase(current.Base, s.clock.Now())
	if input.FleetScopeID != nil {
		if _, err := s.repository.GetFleetScope(ctx, *input.FleetScopeID); err != nil {
			return entity.KubernetesCluster{}, err
		}
		updated.FleetScopeID = *input.FleetScopeID
	}
	if input.ServerIDSet {
		if input.ServerID != nil {
			if _, err := s.repository.GetServer(ctx, *input.ServerID); err != nil {
				return entity.KubernetesCluster{}, err
			}
		}
		updated.ServerID = input.ServerID
	}
	updated.ClusterKey = applyString(current.ClusterKey, input.ClusterKey)
	if input.Status != "" {
		updated.Status = input.Status
	}
	if input.IsDefault != nil {
		updated.IsDefault = *input.IsDefault
	}
	updated.APIEndpointRef = applyString(current.APIEndpointRef, input.APIEndpointRef)
	updated.SecretStoreType = applyString(current.SecretStoreType, input.SecretStoreType)
	updated.SecretStoreRef = applyString(current.SecretStoreRef, input.SecretStoreRef)
	updated.KubernetesVersion = applyString(current.KubernetesVersion, input.KubernetesVersion)
	updated.Region = applyString(current.Region, input.Region)
	updated.CapacityClass = applyString(current.CapacityClass, input.CapacityClass)
	if err := validateKubernetesCluster(updated); err != nil {
		return entity.KubernetesCluster{}, err
	}
	return s.updateCluster(ctx, input.Meta, updated, previousVersion, fleetOperationUpdateCluster, fleetEventClusterUpdated)
}

// DisableKubernetesCluster disables new placements in a cluster.
func (s *Service) DisableKubernetesCluster(ctx context.Context, id uuid.UUID, meta value.CommandMeta) (entity.KubernetesCluster, error) {
	return s.changeClusterStatus(ctx, id, meta, fleetActionClusterDisable, fleetOperationDisableCluster, enum.KubernetesClusterStatusSuspended, fleetEventClusterDisabled)
}

// EnableKubernetesCluster allows new placements in a cluster.
func (s *Service) EnableKubernetesCluster(ctx context.Context, id uuid.UUID, meta value.CommandMeta) (entity.KubernetesCluster, error) {
	return s.changeClusterStatus(ctx, id, meta, fleetActionClusterEnable, fleetOperationEnableCluster, enum.KubernetesClusterStatusActive, fleetEventClusterEnabled)
}

// GetKubernetesCluster returns authoritative Kubernetes cluster state.
func (s *Service) GetKubernetesCluster(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.KubernetesCluster, error) {
	return getAggregate(s, ctx, id, meta, fleetActionClusterRead, s.repository.GetKubernetesCluster, clusterResource)
}

func getAggregate[T any](
	s *Service,
	ctx context.Context,
	id uuid.UUID,
	meta value.QueryMeta,
	action string,
	load func(context.Context, uuid.UUID) (T, error),
	resource func(T) resourceRef,
) (T, error) {
	if err := requireID(id); err != nil {
		var empty T
		return empty, err
	}
	aggregate, err := load(ctx, id)
	if err != nil {
		var empty T
		return empty, err
	}
	if err := s.authorizeQuery(ctx, meta, action, resource(aggregate)); err != nil {
		var empty T
		return empty, err
	}
	return aggregate, nil
}

func scopeResource(scope entity.FleetScope) resourceRef {
	return fleetResource(accesscatalog.ResourceFleetScope, scope.ID, nil)
}

func serverResource(server entity.Server) resourceRef {
	return fleetResource(accesscatalog.ResourceFleetServer, server.ID, nil)
}

func clusterResource(cluster entity.KubernetesCluster) resourceRef {
	return fleetResource(accesscatalog.ResourceFleetCluster, cluster.ID, &cluster.FleetScopeID)
}

// ListKubernetesClusters returns Kubernetes clusters matching filter.
func (s *Service) ListKubernetesClusters(ctx context.Context, input ListKubernetesClustersInput) (ListKubernetesClustersResult, error) {
	if err := s.authorizeList(ctx, input.Meta, fleetActionClusterList, accesscatalog.ResourceFleetCluster); err != nil {
		return ListKubernetesClustersResult{}, err
	}
	clusters, page, err := s.repository.ListKubernetesClusters(ctx, query.KubernetesClusterFilter{
		FleetScopeID:   input.FleetScopeID,
		ServerID:       input.ServerID,
		Statuses:       input.Statuses,
		HealthStatuses: input.HealthStatuses,
		Region:         input.Region,
		CapacityClass:  input.CapacityClass,
		IsDefault:      input.IsDefault,
		Page:           input.Page,
	})
	return ListKubernetesClustersResult{Clusters: clusters, Page: page}, err
}

func (s *Service) createCluster(ctx context.Context, meta value.CommandMeta, cluster entity.KubernetesCluster, operation string, eventType string) (entity.KubernetesCluster, error) {
	buildEvent := s.clusterEvent
	save := s.repository.RegisterKubernetesCluster
	return persistCreated(ctx, meta, operation, fleetAggregateCluster, cluster, cluster.ID, cluster.UpdatedAt, eventType, buildEvent, save)
}

func (s *Service) updateCluster(ctx context.Context, meta value.CommandMeta, cluster entity.KubernetesCluster, previousVersion int64, operation string, eventType string) (entity.KubernetesCluster, error) {
	buildEvent := s.clusterEvent
	save := s.repository.UpdateKubernetesCluster
	return persistUpdated(ctx, meta, operation, fleetAggregateCluster, cluster, cluster.ID, cluster.UpdatedAt, previousVersion, eventType, buildEvent, save)
}

func (s *Service) changeClusterStatus(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string, status enum.KubernetesClusterStatus, eventType string) (entity.KubernetesCluster, error) {
	load := s.loadClusterForMutation
	save := s.updateCluster
	return changeStatus(ctx, s, id, meta, action, operation, fleetAggregateCluster, status, eventType, load, setClusterStatus, save)
}

func changeStatus[T any, S ~string](
	ctx context.Context,
	s *Service,
	id uuid.UUID,
	meta value.CommandMeta,
	action string,
	operation string,
	aggregateType string,
	status S,
	eventType string,
	load func(context.Context, uuid.UUID, value.CommandMeta, string, string) (T, error),
	set func(T, time.Time, S) T,
	save func(context.Context, value.CommandMeta, T, int64, string, string) (T, error),
) (T, error) {
	current, err := load(ctx, id, meta, action, operation)
	if err != nil {
		var empty T
		return empty, err
	}
	if replayed, ok, err := replayTarget(s, ctx, meta, operation, aggregateType, id, current); ok || err != nil {
		return replayed, err
	}
	previousVersion, err := expectedVersion(meta)
	if err != nil {
		var empty T
		return empty, err
	}
	updated := set(current, s.clock.Now(), status)
	return save(ctx, meta, updated, previousVersion, operation, eventType)
}

func setScopeStatus(scope entity.FleetScope, now time.Time, status enum.FleetScopeStatus) entity.FleetScope {
	scope.Base = updatedBase(scope.Base, now)
	scope.Status = status
	return scope
}

func setServerStatus(server entity.Server, now time.Time, status enum.ServerStatus) entity.Server {
	server.Base = updatedBase(server.Base, now)
	server.Status = status
	return server
}

func setClusterStatus(cluster entity.KubernetesCluster, now time.Time, status enum.KubernetesClusterStatus) entity.KubernetesCluster {
	cluster.Base = updatedBase(cluster.Base, now)
	cluster.Status = status
	return cluster
}

func (s *Service) loadScopeForMutation(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string) (entity.FleetScope, error) {
	load := s.repository.GetFleetScope
	return loadForMutation(s, ctx, id, meta, action, operation, fleetAggregateScope, load, scopeResource)
}

func (s *Service) loadServerForMutation(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string) (entity.Server, error) {
	resource := serverResource
	return loadForMutation(s, ctx, id, meta, action, operation, fleetAggregateServer, s.repository.GetServer, resource)
}

func (s *Service) loadClusterForMutation(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string) (entity.KubernetesCluster, error) {
	load := s.repository.GetKubernetesCluster
	resource := clusterResource
	return loadForMutation(s, ctx, id, meta, action, operation, fleetAggregateCluster, load, resource)
}

func loadForMutation[T any](
	s *Service,
	ctx context.Context,
	id uuid.UUID,
	meta value.CommandMeta,
	action string,
	operation string,
	aggregateType string,
	load func(context.Context, uuid.UUID) (T, error),
	resource func(T) resourceRef,
) (T, error) {
	if err := requireID(id); err != nil {
		var empty T
		return empty, err
	}
	aggregate, err := load(ctx, id)
	if err != nil {
		var empty T
		return empty, err
	}
	if err := s.authorizeCommand(ctx, meta, action, resource(aggregate)); err != nil {
		var empty T
		return empty, err
	}
	if _, ok, err := s.findCommandResult(ctx, meta, operation, aggregateType); ok || err != nil {
		return aggregate, err
	}
	return aggregate, nil
}

func mutationArtifacts(
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	aggregateID uuid.UUID,
	occurredAt time.Time,
	buildEvent func() (entity.OutboxEvent, error),
) (entity.CommandResult, entity.OutboxEvent, error) {
	result, err := commandResult(meta, operation, aggregateType, aggregateID, occurredAt)
	if err != nil {
		return entity.CommandResult{}, entity.OutboxEvent{}, err
	}
	event, err := buildEvent()
	if err != nil {
		return entity.CommandResult{}, entity.OutboxEvent{}, err
	}
	return result, event, nil
}

func persistCreated[T any](
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	aggregate T,
	aggregateID uuid.UUID,
	occurredAt time.Time,
	eventType string,
	buildEvent func(string, T) (entity.OutboxEvent, error),
	save func(context.Context, T, entity.OutboxEvent, entity.CommandResult) error,
) (T, error) {
	result, event, err := mutationArtifacts(meta, operation, aggregateType, aggregateID, occurredAt, func() (entity.OutboxEvent, error) {
		return buildEvent(eventType, aggregate)
	})
	if err != nil {
		var empty T
		return empty, err
	}
	if err := save(ctx, aggregate, event, result); err != nil {
		var empty T
		return empty, err
	}
	return aggregate, nil
}

func persistUpdated[T any](
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	aggregate T,
	aggregateID uuid.UUID,
	occurredAt time.Time,
	previousVersion int64,
	eventType string,
	buildEvent func(string, T) (entity.OutboxEvent, error),
	save func(context.Context, T, int64, entity.OutboxEvent, entity.CommandResult) error,
) (T, error) {
	result, event, err := mutationArtifacts(meta, operation, aggregateType, aggregateID, occurredAt, func() (entity.OutboxEvent, error) {
		return buildEvent(eventType, aggregate)
	})
	if err != nil {
		var empty T
		return empty, err
	}
	if err := save(ctx, aggregate, previousVersion, event, result); err != nil {
		var empty T
		return empty, err
	}
	return aggregate, nil
}

func replayAggregate[T any](
	s *Service,
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	load func(context.Context, uuid.UUID) (T, error),
) (T, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, operation, aggregateType)
	if err != nil || !ok {
		var empty T
		return empty, ok, err
	}
	aggregate, err := load(ctx, result.AggregateID)
	return aggregate, true, err
}

func replayTarget[T any](
	s *Service,
	ctx context.Context,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	id uuid.UUID,
	current T,
) (T, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, operation, aggregateType)
	if err != nil || !ok {
		var empty T
		return empty, ok, err
	}
	if result.AggregateID != id {
		var empty T
		return empty, true, errs.ErrConflict
	}
	return current, true, nil
}
