package service

import (
	"context"
	"errors"
	"strings"
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
	assertEvent(t, repository.events[0], fleetEventScopeCreated, fleetAggregateScope, scope.ID)

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

func TestCreateFleetScopeReplayRequiresReadAccess(t *testing.T) {
	repository := newMemoryRepository()
	meta := commandMeta(uuid.MustParse("11111111-1111-1111-1111-111111111111"), "create-platform")
	created, err := newTestService(repository).CreateFleetScope(context.Background(), CreateFleetScopeInput{
		ScopeKey:     "platform-secondary",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Platform secondary",
		Meta:         meta,
	})
	if err != nil {
		t.Fatalf("CreateFleetScope returned error: %v", err)
	}
	service := newTestServiceWithAuthorizer(repository, authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
		if request.ActionKey == fleetActionScopeRead && request.ResourceID == created.ID.String() {
			return errs.ErrForbidden
		}
		return nil
	}))

	_, err = service.CreateFleetScope(context.Background(), CreateFleetScopeInput{
		ScopeKey:     "ignored",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Ignored",
		Meta:         meta,
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expected replay read denial, got %v", err)
	}
	if len(repository.events) != 1 {
		t.Fatalf("denied replay must not append events, got %d", len(repository.events))
	}
}

func TestCreateFleetScopeDeniedByAuthorizer(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestServiceWithAuthorizer(repository, authorizerFunc(func(context.Context, AuthorizationRequest) error {
		return errs.ErrForbidden
	}))

	_, err := service.CreateFleetScope(context.Background(), CreateFleetScopeInput{
		ScopeKey:     "platform-secondary",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Platform secondary",
		Meta:         commandMeta(uuid.MustParse("11111111-1111-1111-1111-111111111111"), "create-platform"),
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expected authorizer denial, got %v", err)
	}
	if len(repository.scopes) != 0 || len(repository.events) != 0 {
		t.Fatalf("denied command must not mutate state: scopes=%d events=%d", len(repository.scopes), len(repository.events))
	}
}

func TestUpdateFleetScopeRejectsStaleVersion(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(repository)
	scope, err := service.CreateFleetScope(context.Background(), CreateFleetScopeInput{
		ScopeKey:     "platform-secondary",
		ScopeType:    enum.FleetScopeTypePlatform,
		OwnerRefJSON: []byte("{}"),
		DisplayName:  "Platform secondary",
		Meta:         commandMeta(uuid.MustParse("11111111-1111-1111-1111-111111111111"), "create-platform"),
	})
	if err != nil {
		t.Fatalf("CreateFleetScope returned error: %v", err)
	}
	nextName := "Platform renamed"

	_, err = service.UpdateFleetScope(context.Background(), UpdateFleetScopeInput{
		FleetScopeID: scope.ID,
		DisplayName:  &nextName,
		Meta:         commandMetaWithVersion(uuid.MustParse("22222222-2222-2222-2222-222222222222"), "update-platform", 2),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expected optimistic conflict, got %v", err)
	}
	if repository.scopes[scope.ID].DisplayName != scope.DisplayName {
		t.Fatalf("stale update changed scope: %+v", repository.scopes[scope.ID])
	}
}

func TestUpdateKubernetesClusterKeepsServerWhenFieldOmitted(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(repository)
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
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

func TestLifecycleTransitionsCreateEvents(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(repository)
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)

	scope, err := service.DisableFleetScope(context.Background(), scopeID, commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555551"), "disable-scope", 1))
	if err != nil {
		t.Fatalf("DisableFleetScope returned error: %v", err)
	}
	if scope.Status != enum.FleetScopeStatusSuspended || scope.Version != 2 {
		t.Fatalf("unexpected disabled scope: %+v", scope)
	}
	scope, err = service.EnableFleetScope(context.Background(), scopeID, commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555552"), "enable-scope", 2))
	if err != nil {
		t.Fatalf("EnableFleetScope returned error: %v", err)
	}
	if scope.Status != enum.FleetScopeStatusActive || scope.Version != 3 {
		t.Fatalf("unexpected enabled scope: %+v", scope)
	}
	server, err := service.DisableServer(context.Background(), serverID, commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555553"), "disable-server", 1))
	if err != nil {
		t.Fatalf("DisableServer returned error: %v", err)
	}
	if server.Status != enum.ServerStatusSuspended || server.Version != 2 {
		t.Fatalf("unexpected disabled server: %+v", server)
	}
	server, err = service.EnableServer(context.Background(), serverID, commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555554"), "enable-server", 2))
	if err != nil {
		t.Fatalf("EnableServer returned error: %v", err)
	}
	if server.Status != enum.ServerStatusActive || server.Version != 3 {
		t.Fatalf("unexpected enabled server: %+v", server)
	}
	cluster, err := service.DisableKubernetesCluster(context.Background(), clusterID, commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555555"), "disable-cluster", 1))
	if err != nil {
		t.Fatalf("DisableKubernetesCluster returned error: %v", err)
	}
	if cluster.Status != enum.KubernetesClusterStatusSuspended || cluster.Version != 2 {
		t.Fatalf("unexpected disabled cluster: %+v", cluster)
	}
	cluster, err = service.EnableKubernetesCluster(context.Background(), clusterID, commandMetaWithVersion(uuid.MustParse("55555555-5555-5555-5555-555555555556"), "enable-cluster", 2))
	if err != nil {
		t.Fatalf("EnableKubernetesCluster returned error: %v", err)
	}
	if cluster.Status != enum.KubernetesClusterStatusActive || cluster.Version != 3 {
		t.Fatalf("unexpected enabled cluster: %+v", cluster)
	}

	if len(repository.events) != 6 {
		t.Fatalf("expected six lifecycle events, got %d", len(repository.events))
	}
	assertEvent(t, repository.events[0], fleetEventScopeDisabled, fleetAggregateScope, scopeID)
	assertEvent(t, repository.events[1], fleetEventScopeEnabled, fleetAggregateScope, scopeID)
	assertEvent(t, repository.events[2], fleetEventServerDisabled, fleetAggregateServer, serverID)
	assertEvent(t, repository.events[3], fleetEventServerEnabled, fleetAggregateServer, serverID)
	assertEvent(t, repository.events[4], fleetEventClusterDisabled, fleetAggregateCluster, clusterID)
	assertEvent(t, repository.events[5], fleetEventClusterEnabled, fleetAggregateCluster, clusterID)
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

func TestEnsurePlatformDefaultSeedRejectsUnsafeSecretReference(t *testing.T) {
	repository := newMemoryRepository()
	service := NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{
		PlatformDefaultSeed: PlatformDefaultSeed{
			SecretStoreRef: "apiVersion: v1\nkind: Secret",
		},
	})

	err := service.EnsurePlatformDefaultSeed(context.Background())
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected invalid seed error, got %v", err)
	}
	if len(repository.scopes) != 0 || len(repository.clusters) != 0 || len(repository.events) != 0 {
		t.Fatalf("invalid seed must not mutate state: scopes=%d clusters=%d events=%d", len(repository.scopes), len(repository.clusters), len(repository.events))
	}
}

func TestRunClusterConnectivityCheckStoresSnapshotAndEvents(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
	checker := &fakeConnectivityChecker{result: ConnectivityCheckResult{
		Status:         enum.ConnectivityCheckStatusSucceeded,
		HealthStatus:   enum.ClusterHealthStatusHealthy,
		CapacityStatus: enum.CapacityStatusUnknown,
		SummaryJSON:    []byte(`{"probe":"test"}`),
	}}
	service := newTestServiceWithChecker(repository, checker)

	check, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{
		ClusterID: clusterID,
		Meta:      commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555557"), "check-cluster"),
	})
	if err != nil {
		t.Fatalf("RunClusterConnectivityCheck returned error: %v", err)
	}
	if check.ID == uuid.Nil || check.Status != enum.ConnectivityCheckStatusSucceeded {
		t.Fatalf("unexpected check: %+v", check)
	}
	if checker.calls != 1 {
		t.Fatalf("expected one checker call, got %d", checker.calls)
	}
	if repository.clusters[clusterID].LastHealthStatus != enum.ClusterHealthStatusHealthy {
		t.Fatalf("cluster latest health was not updated: %+v", repository.clusters[clusterID])
	}
	snapshot, err := service.GetClusterHealthSnapshot(context.Background(), GetClusterHealthSnapshotInput{
		ClusterID: clusterID,
		Meta:      queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetClusterHealthSnapshot returned error: %v", err)
	}
	if snapshot.HealthStatus != enum.ClusterHealthStatusHealthy || string(snapshot.SummaryJSON) != `{"probe":"test"}` {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(repository.events) != 1 {
		t.Fatalf("expected one health event, got %d", len(repository.events))
	}
	assertEvent(t, repository.events[0], fleetEventHealthChecked, fleetAggregateHealth, snapshot.ID)
}

func TestRunClusterConnectivityCheckReplayReturnsStoredCheck(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
	checker := &fakeConnectivityChecker{result: ConnectivityCheckResult{
		Status:       enum.ConnectivityCheckStatusSucceeded,
		HealthStatus: enum.ClusterHealthStatusHealthy,
		SummaryJSON:  []byte(`{"probe":"test"}`),
	}}
	service := newTestServiceWithChecker(repository, checker)
	meta := commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555558"), "check-cluster")

	check, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{ClusterID: clusterID, Meta: meta})
	if err != nil {
		t.Fatalf("first check returned error: %v", err)
	}
	replayed, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{ClusterID: clusterID, Meta: meta})
	if err != nil {
		t.Fatalf("replay returned error: %v", err)
	}
	if replayed.ID != check.ID || checker.calls != 1 || len(repository.events) != 1 {
		t.Fatalf("expected stored replay without side effects: check=%s replay=%s calls=%d events=%d", check.ID, replayed.ID, checker.calls, len(repository.events))
	}
}

func TestRunClusterConnectivityCheckReplayRejectsDifferentCluster(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	otherClusterID := uuid.MustParse("44444444-4444-4444-4444-444444444445")
	seedRegistry(repository, scopeID, serverID, clusterID)
	seedCluster(repository, scopeID, serverID, otherClusterID, "cluster-b")
	checker := &fakeConnectivityChecker{result: ConnectivityCheckResult{
		Status:       enum.ConnectivityCheckStatusSucceeded,
		HealthStatus: enum.ClusterHealthStatusHealthy,
		SummaryJSON:  []byte(`{"probe":"test"}`),
	}}
	service := newTestServiceWithChecker(repository, checker)
	meta := commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555561"), "check-cluster")

	if _, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{ClusterID: clusterID, Meta: meta}); err != nil {
		t.Fatalf("first check returned error: %v", err)
	}
	_, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{ClusterID: otherClusterID, Meta: meta})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expected replay conflict for another cluster, got %v", err)
	}
	if checker.calls != 1 || len(repository.events) != 1 {
		t.Fatalf("conflicted replay must not call checker or append events: calls=%d events=%d", checker.calls, len(repository.events))
	}
}

func TestRunClusterConnectivityCheckReplayRequiresHealthReadAccess(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
	checker := &fakeConnectivityChecker{result: ConnectivityCheckResult{
		Status:       enum.ConnectivityCheckStatusSucceeded,
		HealthStatus: enum.ClusterHealthStatusHealthy,
		SummaryJSON:  []byte(`{"probe":"test"}`),
	}}
	meta := commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555562"), "check-cluster")
	if _, err := newTestServiceWithChecker(repository, checker).RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{ClusterID: clusterID, Meta: meta}); err != nil {
		t.Fatalf("first check returned error: %v", err)
	}
	replayChecker := &fakeConnectivityChecker{}
	service := NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			if request.ActionKey == fleetActionHealthRead {
				return errs.ErrForbidden
			}
			return nil
		}),
		ConnectivityChecker: replayChecker,
	})

	_, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{ClusterID: clusterID, Meta: meta})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expected replay read denial, got %v", err)
	}
	if replayChecker.calls != 0 || len(repository.events) != 1 {
		t.Fatalf("denied replay must not call checker or append events: calls=%d events=%d", replayChecker.calls, len(repository.events))
	}
}

func TestRunClusterConnectivityCheckDeniedByAuthorizer(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
	checker := &fakeConnectivityChecker{}
	service := NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			if request.ActionKey == fleetActionHealthCheckRun {
				return errs.ErrForbidden
			}
			return nil
		}),
		ConnectivityChecker: checker,
	})

	_, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{
		ClusterID: clusterID,
		Meta:      commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555559"), "check-cluster"),
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if checker.calls != 0 || len(repository.healthSnapshots) != 0 || len(repository.events) != 0 {
		t.Fatalf("denied check must not mutate state: calls=%d snapshots=%d events=%d", checker.calls, len(repository.healthSnapshots), len(repository.events))
	}
}

func TestRunClusterConnectivityCheckScrubsCheckerError(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
	secretValue := "kubeconfig-token-value"
	service := newTestServiceWithChecker(repository, &fakeConnectivityChecker{err: errors.New(secretValue)})

	check, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{
		ClusterID: clusterID,
		Meta:      commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555563"), "check-cluster"),
	})
	if err != nil {
		t.Fatalf("RunClusterConnectivityCheck returned error: %v", err)
	}
	snapshot, err := service.GetClusterHealthSnapshot(context.Background(), GetClusterHealthSnapshotInput{ClusterID: clusterID, Meta: queryMeta()})
	if err != nil {
		t.Fatalf("GetClusterHealthSnapshot returned error: %v", err)
	}
	if check.ErrorCode != "connectivity_check_failed" || check.ErrorMessage != "Cluster connectivity check failed" {
		t.Fatalf("unexpected sanitized check error: %+v", check)
	}
	values := []string{
		check.ErrorCode,
		check.ErrorMessage,
		snapshot.ErrorCode,
		snapshot.ErrorMessage,
		string(snapshot.SummaryJSON),
		string(repository.events[0].Payload),
	}
	for _, value := range values {
		if strings.Contains(value, secretValue) {
			t.Fatalf("secret leaked into persisted health data: %s", value)
		}
	}
}

func TestRunClusterConnectivityCheckEmitsDegradedEvent(t *testing.T) {
	repository := newMemoryRepository()
	scopeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	serverID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	clusterID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	seedRegistry(repository, scopeID, serverID, clusterID)
	cluster := repository.clusters[clusterID]
	cluster.LastHealthStatus = enum.ClusterHealthStatusHealthy
	repository.clusters[clusterID] = cluster
	service := newTestServiceWithChecker(repository, &fakeConnectivityChecker{result: ConnectivityCheckResult{
		Status:         enum.ConnectivityCheckStatusFailed,
		HealthStatus:   enum.ClusterHealthStatusDegraded,
		CapacityStatus: enum.CapacityStatusLimited,
		ErrorCode:      "capacity_limited",
		ErrorMessage:   "Cluster health is degraded",
		SummaryJSON:    []byte(`{"probe":"test"}`),
	}})

	_, err := service.RunClusterConnectivityCheck(context.Background(), RunClusterConnectivityCheckInput{
		ClusterID: clusterID,
		Meta:      commandMeta(uuid.MustParse("55555555-5555-5555-5555-555555555560"), "check-cluster"),
	})
	if err != nil {
		t.Fatalf("RunClusterConnectivityCheck returned error: %v", err)
	}
	if len(repository.events) != 2 {
		t.Fatalf("expected checked and degraded events, got %d", len(repository.events))
	}
	assertEvent(t, repository.events[0], fleetEventHealthChecked, fleetAggregateHealth, repository.events[0].AggregateID)
	assertEvent(t, repository.events[1], fleetEventHealthDegraded, fleetAggregateHealth, repository.events[0].AggregateID)
}

func newTestService(repository *memoryRepository) *Service {
	return NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{})
}

func newTestServiceWithAuthorizer(repository *memoryRepository, authorizer Authorizer) *Service {
	return NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{Authorizer: authorizer})
}

func newTestServiceWithChecker(repository *memoryRepository, checker ConnectivityChecker) *Service {
	return NewWithConfig(repository, fixedClock{}, sequentialIDs{}, Config{ConnectivityChecker: checker})
}

type fakeConnectivityChecker struct {
	result ConnectivityCheckResult
	err    error
	calls  int
}

func (c *fakeConnectivityChecker) CheckClusterConnectivity(context.Context, ConnectivityCheckTarget) (ConnectivityCheckResult, error) {
	c.calls++
	return c.result, c.err
}

type authorizerFunc func(context.Context, AuthorizationRequest) error

func (fn authorizerFunc) Authorize(ctx context.Context, request AuthorizationRequest) error {
	return fn(ctx, request)
}

func queryMeta() value.QueryMeta {
	return value.QueryMeta{
		Actor:          value.Actor{Type: "service", ID: "fleet-manager-test"},
		RequestID:      "request-test",
		RequestContext: value.RequestContext{Source: "test"},
	}
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

func seedRegistry(repository *memoryRepository, scopeID uuid.UUID, serverID uuid.UUID, clusterID uuid.UUID) {
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
}

func seedCluster(repository *memoryRepository, scopeID uuid.UUID, serverID uuid.UUID, clusterID uuid.UUID, key string) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	repository.clusters[clusterID] = entity.KubernetesCluster{
		Base:             entity.Base{ID: clusterID, Version: 1, CreatedAt: now, UpdatedAt: now},
		FleetScopeID:     scopeID,
		ServerID:         &serverID,
		ClusterKey:       key,
		Status:           enum.KubernetesClusterStatusActive,
		LastHealthStatus: enum.ClusterHealthStatusUnknown,
	}
}

func assertEvent(t *testing.T, event entity.OutboxEvent, eventType string, aggregateType string, aggregateID uuid.UUID) {
	t.Helper()
	if event.EventType != eventType || event.AggregateType != aggregateType || event.AggregateID != aggregateID {
		t.Fatalf("unexpected event: got type=%s aggregate_type=%s aggregate_id=%s", event.EventType, event.AggregateType, event.AggregateID)
	}
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
	scopes          map[uuid.UUID]entity.FleetScope
	servers         map[uuid.UUID]entity.Server
	clusters        map[uuid.UUID]entity.KubernetesCluster
	checks          map[uuid.UUID]entity.ClusterConnectivityCheck
	healthSnapshots map[uuid.UUID]entity.ClusterHealthSnapshot
	commands        map[string]entity.CommandResult
	events          []entity.OutboxEvent
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		scopes:          map[uuid.UUID]entity.FleetScope{},
		servers:         map[uuid.UUID]entity.Server{},
		clusters:        map[uuid.UUID]entity.KubernetesCluster{},
		checks:          map[uuid.UUID]entity.ClusterConnectivityCheck{},
		healthSnapshots: map[uuid.UUID]entity.ClusterHealthSnapshot{},
		commands:        map[string]entity.CommandResult{},
	}
}

func (r *memoryRepository) Ping(context.Context) error { return nil }

func (r *memoryRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	for _, result := range r.commands {
		if identity.CommandID != uuid.Nil &&
			result.CommandID != nil &&
			*result.CommandID == identity.CommandID &&
			result.Operation == identity.Operation &&
			result.ActorType == identity.Actor.Type &&
			result.ActorID == identity.Actor.ID {
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

func (r *memoryRepository) StoreClusterHealthCheck(_ context.Context, cluster entity.KubernetesCluster, check entity.ClusterConnectivityCheck, snapshot entity.ClusterHealthSnapshot, events []entity.OutboxEvent, result entity.CommandResult) error {
	if _, ok := r.clusters[cluster.ID]; !ok {
		return errs.ErrNotFound
	}
	r.clusters[cluster.ID] = cluster
	r.checks[check.ID] = check
	r.healthSnapshots[snapshot.ID] = snapshot
	r.storeCommand(result)
	r.events = append(r.events, events...)
	return nil
}

func (r *memoryRepository) GetClusterConnectivityCheck(_ context.Context, id uuid.UUID) (entity.ClusterConnectivityCheck, error) {
	check, ok := r.checks[id]
	if !ok {
		return entity.ClusterConnectivityCheck{}, errs.ErrNotFound
	}
	return check, nil
}

func (r *memoryRepository) GetClusterHealthSnapshot(_ context.Context, id uuid.UUID) (entity.ClusterHealthSnapshot, error) {
	snapshot, ok := r.healthSnapshots[id]
	if !ok {
		return entity.ClusterHealthSnapshot{}, errs.ErrNotFound
	}
	return snapshot, nil
}

func (r *memoryRepository) GetLatestClusterHealthSnapshot(_ context.Context, clusterID uuid.UUID) (entity.ClusterHealthSnapshot, error) {
	var latest entity.ClusterHealthSnapshot
	for _, snapshot := range r.healthSnapshots {
		if snapshot.ClusterID != clusterID {
			continue
		}
		if latest.ID == uuid.Nil || snapshot.CheckedAt.After(latest.CheckedAt) {
			latest = snapshot
		}
	}
	if latest.ID == uuid.Nil {
		return entity.ClusterHealthSnapshot{}, errs.ErrNotFound
	}
	return latest, nil
}

func (r *memoryRepository) ListClusterHealthSnapshots(_ context.Context, filter query.ClusterHealthSnapshotFilter) ([]entity.ClusterHealthSnapshot, query.PageResult, error) {
	items := make([]entity.ClusterHealthSnapshot, 0, len(r.healthSnapshots))
	for _, snapshot := range r.healthSnapshots {
		if snapshot.ClusterID != filter.ClusterID {
			continue
		}
		if filter.CheckedSince != nil && snapshot.CheckedAt.Before(*filter.CheckedSince) {
			continue
		}
		items = append(items, snapshot)
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
