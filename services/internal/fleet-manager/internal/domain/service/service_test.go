package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

func TestCreateFleetScopeStoresCommandAndOutbox(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(repository)
	meta := commandMeta(uuid.MustParse("11111111-1111-1111-1111-111111111111"), "create-platform")

	scope, err := service.CreateFleetScope(context.Background(), CreateFleetScopeInput{
		ScopeKey:     "platform-secondary",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Platform secondary",
		Meta:         meta,
	})
	if err != nil {
		t.Fatalf("CreateFleetScope returned error: %v", err)
	}
	if scope.ID == uuid.Nil || scope.Version != 1 || scope.Status != enum.FleetScopeStatusActive {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if len(repository.events) != 1 {
		t.Fatalf("expected one outbox event, got %d", len(repository.events))
	}

	replayed, err := service.CreateFleetScope(context.Background(), CreateFleetScopeInput{
		ScopeKey:     "ignored",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Ignored",
		Meta:         meta,
	})
	if err != nil {
		t.Fatalf("idempotent replay returned error: %v", err)
	}
	if replayed.ID != scope.ID || len(repository.events) != 1 {
		t.Fatalf("expected replay of existing scope without new event")
	}
}

func TestUpdateKubernetesClusterKeepsServerWhenFieldOmitted(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(repository)
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	repository.scopes[scopeID] = entity.FleetScope{
		Base:         entity.Base{ID: scopeID, Version: 1, CreatedAt: now, UpdatedAt: now},
		ScopeKey:     "platform-default",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Platform default",
		Status:       enum.FleetScopeStatusActive,
	}
	repository.servers[serverID] = entity.Server{
		Base:         entity.Base{ID: serverID, Version: 1, CreatedAt: now, UpdatedAt: now},
		ServerKey:    "server-a",
		ProviderType: enum.ServerProviderTypeVPS,
		Status:       enum.ServerStatusActive,
	}
	repository.clusters[clusterID] = entity.KubernetesCluster{
		Base:             entity.Base{ID: clusterID, Version: 1, CreatedAt: now, UpdatedAt: now},
		FleetScopeID:     scopeID,
		ServerID:         &serverID,
		ClusterKey:       "cluster-a",
		Status:           enum.KubernetesClusterStatusActive,
		LastHealthStatus: enum.ClusterHealthStatusUnknown,
	}
	nextKey := "cluster-renamed"

	updated, err := service.UpdateKubernetesCluster(context.Background(), UpdateKubernetesClusterInput{
		ClusterID:  clusterID,
		ClusterKey: &nextKey,
		Meta:       commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555555"), "update-cluster", 1),
	})
	if err != nil {
		t.Fatalf("UpdateKubernetesCluster returned error: %v", err)
	}
	if updated.ServerID == nil || *updated.ServerID != serverID {
		t.Fatalf("server link was unexpectedly cleared: %+v", updated.ServerID)
	}
	if updated.ClusterKey != nextKey || updated.Version != 2 {
		t.Fatalf("cluster was not updated: %+v", updated)
	}
}

func TestEnsurePlatformDefaultSeedCreatesRegistryDataOnce(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(repository)

	if err := service.EnsurePlatformDefaultSeed(context.Background()); err != nil {
		t.Fatalf("EnsurePlatformDefaultSeed returned error: %v", err)
	}
	if len(repository.scopes) != 1 || len(repository.clusters) != 1 || len(repository.events) != 2 {
		t.Fatalf("unexpected seed state: scopes=%d clusters=%d events=%d", len(repository.scopes), len(repository.clusters), len(repository.events))
	}
	if err := service.EnsurePlatformDefaultSeed(context.Background()); err != nil {
		t.Fatalf("second EnsurePlatformDefaultSeed returned error: %v", err)
	}
	if len(repository.scopes) != 1 || len(repository.clusters) != 1 || len(repository.events) != 2 {
		t.Fatalf("seed should be idempotent: scopes=%d clusters=%d events=%d", len(repository.scopes), len(repository.clusters), len(repository.events))
	}
}

func newTestService(repository *memoryRepository) *Service {
	return NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{})
}

func commandMeta(commandID uuid.UUID, key string) value.CommandMeta {
	return value.CommandMeta{
		CommandID:      commandID,
		IdempotencyKey: key,
		Actor:          value.Actor{Type: "service", ID: "fleet-manager-test"},
		RequestID:      "request-test",
		RequestContext: value.RequestContext{Source: "test"},
	}
}

func commandMetaWithVersion(commandID uuid.UUID, key string, version int64) value.CommandMeta {
	meta := commandMeta(commandID, key)
	meta.ExpectedVersion = &version
	return meta
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
}

type sequentialIDs struct{}

func (sequentialIDs) New() uuid.UUID {
	return uuid.New()
}

type memoryRepository struct {
	scopes   map[uuid.UUID]entity.FleetScope
	servers  map[uuid.UUID]entity.Server
	clusters map[uuid.UUID]entity.KubernetesCluster
	commands map[string]entity.CommandResult
	events   []entity.OutboxEvent
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		scopes:   map[uuid.UUID]entity.FleetScope{},
		servers:  map[uuid.UUID]entity.Server{},
		clusters: map[uuid.UUID]entity.KubernetesCluster{},
		commands: map[string]entity.CommandResult{},
	}
}

func (r *memoryRepository) Ping(context.Context) error { return nil }

func (r *memoryRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	for _, result := range r.commands {
		if identity.CommandID != uuid.Nil && result.CommandID != nil && *result.CommandID == identity.CommandID {
			return result, nil
		}
		if identity.CommandID == uuid.Nil &&
			result.Operation == identity.Operation &&
			result.ActorType == identity.Actor.Type &&
			result.ActorID == identity.Actor.ID &&
			result.IdempotencyKey == identity.IdempotencyKey {
			return result, nil
		}
	}
	return entity.CommandResult{}, errs.ErrNotFound
}

func (r *memoryRepository) CreateFleetScope(_ context.Context, scope entity.FleetScope, event entity.OutboxEvent, result entity.CommandResult) error {
	if _, exists := r.scopes[scope.ID]; exists {
		return errs.ErrAlreadyExists
	}
	r.scopes[scope.ID] = scope
	r.storeCommand(result)
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) UpdateFleetScope(_ context.Context, scope entity.FleetScope, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error {
	current, ok := r.scopes[scope.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.scopes[scope.ID] = scope
	r.storeCommand(result)
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetFleetScope(_ context.Context, id uuid.UUID) (entity.FleetScope, error) {
	scope, ok := r.scopes[id]
	if !ok {
		return entity.FleetScope{}, errs.ErrNotFound
	}
	return scope, nil
}

func (r *memoryRepository) ListFleetScopes(context.Context, query.FleetScopeFilter) ([]entity.FleetScope, query.PageResult, error) {
	items := make([]entity.FleetScope, 0, len(r.scopes))
	for _, item := range r.scopes {
		items = append(items, item)
	}
	return items, query.PageResult{}, nil
}

func (r *memoryRepository) RegisterServer(_ context.Context, server entity.Server, event entity.OutboxEvent, result entity.CommandResult) error {
	r.servers[server.ID] = server
	r.storeCommand(result)
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) UpdateServer(_ context.Context, server entity.Server, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error {
	current, ok := r.servers[server.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.servers[server.ID] = server
	r.storeCommand(result)
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetServer(_ context.Context, id uuid.UUID) (entity.Server, error) {
	server, ok := r.servers[id]
	if !ok {
		return entity.Server{}, errs.ErrNotFound
	}
	return server, nil
}

func (r *memoryRepository) ListServers(context.Context, query.ServerFilter) ([]entity.Server, query.PageResult, error) {
	items := make([]entity.Server, 0, len(r.servers))
	for _, item := range r.servers {
		items = append(items, item)
	}
	return items, query.PageResult{}, nil
}

func (r *memoryRepository) RegisterKubernetesCluster(_ context.Context, cluster entity.KubernetesCluster, event entity.OutboxEvent, result entity.CommandResult) error {
	r.clusters[cluster.ID] = cluster
	r.storeCommand(result)
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) UpdateKubernetesCluster(_ context.Context, cluster entity.KubernetesCluster, previousVersion int64, event entity.OutboxEvent, result entity.CommandResult) error {
	current, ok := r.clusters[cluster.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.clusters[cluster.ID] = cluster
	r.storeCommand(result)
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetKubernetesCluster(_ context.Context, id uuid.UUID) (entity.KubernetesCluster, error) {
	cluster, ok := r.clusters[id]
	if !ok {
		return entity.KubernetesCluster{}, errs.ErrNotFound
	}
	return cluster, nil
}

func (r *memoryRepository) ListKubernetesClusters(context.Context, query.KubernetesClusterFilter) ([]entity.KubernetesCluster, query.PageResult, error) {
	items := make([]entity.KubernetesCluster, 0, len(r.clusters))
	for _, item := range r.clusters {
		items = append(items, item)
	}
	return items, query.PageResult{}, nil
}

func (r *memoryRepository) EnsurePlatformDefaultSeed(_ context.Context, scope entity.FleetScope, cluster entity.KubernetesCluster, events []entity.OutboxEvent) error {
	if _, exists := r.scopes[scope.ID]; !exists {
		r.scopes[scope.ID] = scope
		if len(events) > 0 {
			r.events = append(r.events, events[0])
		}
	}
	if _, exists := r.clusters[cluster.ID]; !exists {
		r.clusters[cluster.ID] = cluster
		if len(events) > 1 {
			r.events = append(r.events, events[1])
		}
	}
	return nil
}

func (r *memoryRepository) AppendOutboxEvent(_ context.Context, event entity.OutboxEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, nil
}

func (r *memoryRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return nil
}

func (r *memoryRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return nil
}

func (r *memoryRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return nil
}

func (r *memoryRepository) storeCommand(result entity.CommandResult) {
	if result.Key == "" {
		panic(errors.New("empty command key"))
	}
	r.commands[result.Key] = result
}
