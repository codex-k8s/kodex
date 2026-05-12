package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	fleetevents "github.com/codex-k8s/kodex/libs/go/platformevents/fleet"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
)

func (s *Service) aggregateEvent(eventType string, aggregateType string, aggregateID uuid.UUID, occurredAt time.Time, payload fleetevents.Payload) (entity.OutboxEvent, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(
			s.ids.New(),
			eventType,
			fleetevents.SchemaVersion,
			aggregateType,
			aggregateID,
			body,
			occurredAt,
			0,
		),
		NextAttemptAt: occurredAt,
	}, nil
}

func (s *Service) scopeEvent(eventType string, scope entity.FleetScope) (entity.OutboxEvent, error) {
	return s.aggregateEvent(eventType, fleetAggregateScope, scope.ID, scope.UpdatedAt, fleetevents.Payload{
		FleetScopeID: scope.ID.String(),
		ScopeKey:     scope.ScopeKey,
		ScopeType:    string(scope.ScopeType),
		Status:       string(scope.Status),
		IsDefault:    scope.IsDefault,
		Version:      scope.Version,
	})
}

func (s *Service) serverEvent(eventType string, server entity.Server) (entity.OutboxEvent, error) {
	return s.aggregateEvent(eventType, fleetAggregateServer, server.ID, server.UpdatedAt, fleetevents.Payload{
		ServerID:  server.ID.String(),
		ServerKey: server.ServerKey,
		Status:    string(server.Status),
		Version:   server.Version,
	})
}

func (s *Service) clusterEvent(eventType string, cluster entity.KubernetesCluster) (entity.OutboxEvent, error) {
	return s.aggregateEvent(eventType, fleetAggregateCluster, cluster.ID, cluster.UpdatedAt, fleetevents.Payload{
		ClusterID:    cluster.ID.String(),
		ClusterKey:   cluster.ClusterKey,
		FleetScopeID: cluster.FleetScopeID.String(),
		ServerID:     uuidPtrString(cluster.ServerID),
		Status:       string(cluster.Status),
		HealthStatus: string(cluster.LastHealthStatus),
		IsDefault:    cluster.IsDefault,
		Version:      cluster.Version,
	})
}

func (s *Service) healthEvent(eventType string, cluster entity.KubernetesCluster, snapshot entity.ClusterHealthSnapshot, previousStatus string) (entity.OutboxEvent, error) {
	return s.aggregateEvent(eventType, fleetAggregateHealth, snapshot.ID, snapshot.CheckedAt, fleetevents.Payload{
		ClusterID:      cluster.ID.String(),
		ClusterKey:     cluster.ClusterKey,
		FleetScopeID:   cluster.FleetScopeID.String(),
		ServerID:       uuidPtrString(cluster.ServerID),
		HealthStatus:   string(snapshot.HealthStatus),
		CapacityStatus: string(snapshot.CapacityStatus),
		PreviousStatus: previousStatus,
		ErrorCode:      snapshot.ErrorCode,
		ErrorMessage:   snapshot.ErrorMessage,
	})
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	return id.String()
}
