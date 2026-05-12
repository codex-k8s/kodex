package service

import (
	"context"
	"errors"
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

const maxSafeHealthMessageLen = 512

// RunClusterConnectivityCheck verifies Kubernetes API connectivity and stores a bounded health snapshot.
func (s *Service) RunClusterConnectivityCheck(ctx context.Context, input RunClusterConnectivityCheckInput) (entity.ClusterConnectivityCheck, error) {
	if err := requireID(input.ClusterID); err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	cluster, err := s.repository.GetKubernetesCluster(ctx, input.ClusterID)
	if err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, fleetActionHealthCheckRun, healthResource(cluster)); err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	if replay, ok, err := s.replayConnectivityCheck(ctx, input.Meta, cluster); ok || err != nil {
		return replay, err
	}
	checker, err := s.requireChecker()
	if err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}

	startedAt := s.clock.Now()
	result, checkErr := checker.CheckClusterConnectivity(ctx, connectivityTarget(cluster))
	finishedAt := s.clock.Now()
	result = normalizedConnectivityResult(result, checkErr, startedAt, finishedAt)

	check := entity.ClusterConnectivityCheck{
		ID:           s.ids.New(),
		ClusterID:    cluster.ID,
		Status:       result.Status,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		LatencyMS:    result.LatencyMS,
		ErrorCode:    safeHealthText(result.ErrorCode),
		ErrorMessage: safeHealthText(result.ErrorMessage),
		CreatedAt:    finishedAt,
	}
	snapshot := entity.ClusterHealthSnapshot{
		ID:             s.ids.New(),
		ClusterID:      cluster.ID,
		HealthStatus:   result.HealthStatus,
		CapacityStatus: result.CapacityStatus,
		SummaryJSON:    defaultJSON(result.SummaryJSON),
		CheckedAt:      finishedAt,
		ErrorCode:      check.ErrorCode,
		ErrorMessage:   check.ErrorMessage,
	}
	if err := validateClusterConnectivityCheck(check); err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	if err := validateClusterHealthSnapshot(snapshot); err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}

	previousHealth := cluster.LastHealthStatus
	cluster.LastHealthStatus = snapshot.HealthStatus
	cluster.LastHealthCheckedAt = &snapshot.CheckedAt

	events, err := s.healthEvents(cluster, snapshot, previousHealth)
	if err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	resultRecord, err := commandResult(input.Meta, fleetOperationRunHealthCheck, fleetAggregateHealth, check.ID, snapshot.CheckedAt)
	if err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	if err := s.repository.StoreClusterHealthCheck(ctx, cluster, check, snapshot, events, resultRecord); err != nil {
		return entity.ClusterConnectivityCheck{}, err
	}
	return check, nil
}

// GetClusterHealthSnapshot returns latest or selected cluster health state.
func (s *Service) GetClusterHealthSnapshot(ctx context.Context, input GetClusterHealthSnapshotInput) (entity.ClusterHealthSnapshot, error) {
	cluster, err := s.loadClusterForHealthRead(ctx, input.ClusterID, input.Meta)
	if err != nil {
		return entity.ClusterHealthSnapshot{}, err
	}
	if input.HealthSnapshotID == nil {
		return s.repository.GetLatestClusterHealthSnapshot(ctx, cluster.ID)
	}
	if *input.HealthSnapshotID == uuid.Nil {
		return entity.ClusterHealthSnapshot{}, errs.ErrInvalidArgument
	}
	snapshot, err := s.repository.GetClusterHealthSnapshot(ctx, *input.HealthSnapshotID)
	if err != nil {
		return entity.ClusterHealthSnapshot{}, err
	}
	if snapshot.ClusterID != cluster.ID {
		return entity.ClusterHealthSnapshot{}, errs.ErrNotFound
	}
	return snapshot, nil
}

// ListClusterHealthSnapshots returns bounded health history for one cluster.
func (s *Service) ListClusterHealthSnapshots(ctx context.Context, input ListClusterHealthSnapshotsInput) (ListClusterHealthSnapshotsResult, error) {
	cluster, err := s.loadClusterForHealthRead(ctx, input.ClusterID, input.Meta)
	if err != nil {
		return ListClusterHealthSnapshotsResult{}, err
	}
	snapshots, page, err := s.repository.ListClusterHealthSnapshots(ctx, query.ClusterHealthSnapshotFilter{
		ClusterID:    cluster.ID,
		CheckedSince: input.CheckedSince,
		Page:         input.Page,
	})
	return ListClusterHealthSnapshotsResult{Snapshots: snapshots, Page: page}, err
}

func (s *Service) loadClusterForHealthRead(ctx context.Context, clusterID uuid.UUID, meta value.QueryMeta) (entity.KubernetesCluster, error) {
	if err := requireID(clusterID); err != nil {
		return entity.KubernetesCluster{}, err
	}
	cluster, err := s.repository.GetKubernetesCluster(ctx, clusterID)
	if err != nil {
		return entity.KubernetesCluster{}, err
	}
	if err := s.authorizeQuery(ctx, meta, fleetActionHealthRead, healthResource(cluster)); err != nil {
		return entity.KubernetesCluster{}, err
	}
	return cluster, nil
}

func (s *Service) replayConnectivityCheck(ctx context.Context, meta value.CommandMeta, cluster entity.KubernetesCluster) (entity.ClusterConnectivityCheck, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, fleetOperationRunHealthCheck, fleetAggregateHealth)
	if err != nil || !ok {
		return entity.ClusterConnectivityCheck{}, ok, err
	}
	check, err := s.repository.GetClusterConnectivityCheck(ctx, result.AggregateID)
	if err != nil {
		return entity.ClusterConnectivityCheck{}, true, err
	}
	if check.ClusterID != cluster.ID {
		return entity.ClusterConnectivityCheck{}, true, errs.ErrConflict
	}
	queryMeta := value.QueryMeta{
		Actor:          meta.Actor,
		RequestID:      meta.RequestID,
		RequestContext: meta.RequestContext,
	}
	if err := s.authorizeQuery(ctx, queryMeta, fleetActionHealthRead, healthResource(cluster)); err != nil {
		return entity.ClusterConnectivityCheck{}, true, err
	}
	return check, true, nil
}

func (s *Service) healthEvents(cluster entity.KubernetesCluster, snapshot entity.ClusterHealthSnapshot, previousHealth enum.ClusterHealthStatus) ([]entity.OutboxEvent, error) {
	previousStatus := string(defaultClusterHealth(previousHealth))
	checked, err := s.healthEvent(fleetEventHealthChecked, cluster, snapshot, previousStatus)
	if err != nil {
		return nil, err
	}
	events := []entity.OutboxEvent{checked}
	if emitsHealthDegraded(previousHealth, snapshot.HealthStatus) {
		degraded, err := s.healthEvent(fleetEventHealthDegraded, cluster, snapshot, previousStatus)
		if err != nil {
			return nil, err
		}
		events = append(events, degraded)
	}
	return events, nil
}

func emitsHealthDegraded(previous enum.ClusterHealthStatus, next enum.ClusterHealthStatus) bool {
	previous = defaultClusterHealth(previous)
	next = defaultClusterHealth(next)
	if previous == next {
		return false
	}
	return next == enum.ClusterHealthStatusDegraded || next == enum.ClusterHealthStatusUnhealthy
}

func connectivityTarget(cluster entity.KubernetesCluster) ConnectivityCheckTarget {
	return ConnectivityCheckTarget{
		ClusterID:       cluster.ID,
		ClusterKey:      cluster.ClusterKey,
		APIEndpointRef:  cluster.APIEndpointRef,
		SecretStoreType: cluster.SecretStoreType,
		SecretStoreRef:  cluster.SecretStoreRef,
	}
}

func normalizedConnectivityResult(result ConnectivityCheckResult, err error, startedAt time.Time, finishedAt time.Time) ConnectivityCheckResult {
	if err != nil {
		return failedConnectivityResult(err, startedAt, finishedAt)
	}
	result.Status = defaultConnectivityStatus(result.Status)
	result.HealthStatus = defaultClusterHealth(result.HealthStatus)
	result.CapacityStatus = defaultCapacityStatus(result.CapacityStatus)
	result.ErrorCode = safeHealthText(result.ErrorCode)
	result.ErrorMessage = safeHealthText(result.ErrorMessage)
	if result.SummaryJSON == nil {
		result.SummaryJSON = []byte("{}")
	}
	if result.LatencyMS == nil {
		result.LatencyMS = latencyMillis(startedAt, finishedAt)
	}
	return result
}

func failedConnectivityResult(err error, startedAt time.Time, finishedAt time.Time) ConnectivityCheckResult {
	status := enum.ConnectivityCheckStatusFailed
	code := "connectivity_check_failed"
	message := "Cluster connectivity check failed"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		status = enum.ConnectivityCheckStatusTimedOut
		code = "connectivity_check_timed_out"
		message = "Cluster connectivity check timed out"
	}
	return ConnectivityCheckResult{
		Status:         status,
		HealthStatus:   enum.ClusterHealthStatusUnhealthy,
		CapacityStatus: enum.CapacityStatusUnknown,
		SummaryJSON:    []byte("{}"),
		LatencyMS:      latencyMillis(startedAt, finishedAt),
		ErrorCode:      code,
		ErrorMessage:   message,
	}
}

func latencyMillis(startedAt time.Time, finishedAt time.Time) *int64 {
	if startedAt.IsZero() || finishedAt.IsZero() || finishedAt.Before(startedAt) {
		return nil
	}
	value := finishedAt.Sub(startedAt).Milliseconds()
	return &value
}

func safeHealthText(value string) string {
	trimmed := strings.TrimSpace(value)
	runes := []rune(trimmed)
	if len(runes) <= maxSafeHealthMessageLen {
		return trimmed
	}
	return string(runes[:maxSafeHealthMessageLen])
}

func healthResource(cluster entity.KubernetesCluster) resourceRef {
	return fleetResource(accesscatalog.ResourceFleetHealth, cluster.ID, &cluster.FleetScopeID)
}
