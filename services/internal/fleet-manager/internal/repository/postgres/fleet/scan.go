package fleet

import (
	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
)

func scanFleetScope(row postgreslib.RowScanner) (entity.FleetScope, error) {
	var scope entity.FleetScope
	var scopeOwnerID pgtype.UUID
	var ownerRef []byte
	var scopeType, status string
	err := row.Scan(
		&scope.ID,
		&scope.ScopeKey,
		&scopeType,
		&scopeOwnerID,
		&ownerRef,
		&scope.DisplayName,
		&status,
		&scope.IsDefault,
		&scope.Version,
		&scope.CreatedAt,
		&scope.UpdatedAt,
	)
	scope.ScopeType = enum.FleetScopeType(scopeType)
	scope.ScopeOwnerID = postgreslib.UUIDPtrFromPG(scopeOwnerID)
	scope.OwnerRefJSON = append(scope.OwnerRefJSON[:0], ownerRef...)
	scope.Status = enum.FleetScopeStatus(status)
	return scope, err
}

func scanServer(row postgreslib.RowScanner) (entity.Server, error) {
	var server entity.Server
	var providerType, status string
	err := row.Scan(
		&server.ID,
		&server.ServerKey,
		&providerType,
		&status,
		&server.PrimaryAddressRef,
		&server.Region,
		&server.CapacityClass,
		&server.SecretStoreType,
		&server.SecretStoreRef,
		&server.Version,
		&server.CreatedAt,
		&server.UpdatedAt,
	)
	server.ProviderType = enum.ServerProviderType(providerType)
	server.Status = enum.ServerStatus(status)
	return server, err
}

func scanKubernetesCluster(row postgreslib.RowScanner) (entity.KubernetesCluster, error) {
	var cluster entity.KubernetesCluster
	var serverID pgtype.UUID
	var lastHealthCheckedAt pgtype.Timestamptz
	var status, healthStatus string
	err := row.Scan(
		&cluster.ID,
		&cluster.FleetScopeID,
		&serverID,
		&cluster.ClusterKey,
		&status,
		&cluster.IsDefault,
		&cluster.APIEndpointRef,
		&cluster.SecretStoreType,
		&cluster.SecretStoreRef,
		&cluster.KubernetesVersion,
		&cluster.Region,
		&cluster.CapacityClass,
		&healthStatus,
		&lastHealthCheckedAt,
		&cluster.Version,
		&cluster.CreatedAt,
		&cluster.UpdatedAt,
	)
	cluster.ServerID = postgreslib.UUIDPtrFromPG(serverID)
	cluster.Status = enum.KubernetesClusterStatus(status)
	cluster.LastHealthStatus = enum.ClusterHealthStatus(healthStatus)
	cluster.LastHealthCheckedAt = postgreslib.TimePtrFromPG(lastHealthCheckedAt)
	return cluster, err
}

func scanClusterConnectivityCheck(row postgreslib.RowScanner) (entity.ClusterConnectivityCheck, error) {
	var check entity.ClusterConnectivityCheck
	var startedAt, finishedAt pgtype.Timestamptz
	var latencyMS pgtype.Int8
	var status string
	err := row.Scan(
		&check.ID,
		&check.ClusterID,
		&status,
		&startedAt,
		&finishedAt,
		&latencyMS,
		&check.ErrorCode,
		&check.ErrorMessage,
		&check.CreatedAt,
	)
	check.Status = enum.ConnectivityCheckStatus(status)
	check.StartedAt = postgreslib.TimePtrFromPG(startedAt)
	check.FinishedAt = postgreslib.TimePtrFromPG(finishedAt)
	if latencyMS.Valid {
		check.LatencyMS = &latencyMS.Int64
	}
	return check, err
}

func scanClusterHealthSnapshot(row postgreslib.RowScanner) (entity.ClusterHealthSnapshot, error) {
	var snapshot entity.ClusterHealthSnapshot
	var summary []byte
	var healthStatus, capacityStatus string
	err := row.Scan(
		&snapshot.ID,
		&snapshot.ClusterID,
		&healthStatus,
		&capacityStatus,
		&summary,
		&snapshot.CheckedAt,
		&snapshot.ErrorCode,
		&snapshot.ErrorMessage,
	)
	snapshot.HealthStatus = enum.ClusterHealthStatus(healthStatus)
	snapshot.CapacityStatus = enum.CapacityStatus(capacityStatus)
	snapshot.SummaryJSON = append(snapshot.SummaryJSON[:0], summary...)
	return snapshot, err
}

func scanPlacementRule(row postgreslib.RowScanner) (entity.PlacementRule, error) {
	var rule entity.PlacementRule
	var matchJSON, constraintsJSON []byte
	var status string
	err := row.Scan(
		&rule.ID,
		&rule.FleetScopeID,
		&rule.RuleKey,
		&status,
		&rule.Priority,
		&matchJSON,
		&constraintsJSON,
		&rule.Version,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	rule.Status = enum.PlacementRuleStatus(status)
	rule.MatchJSON = append(rule.MatchJSON[:0], matchJSON...)
	rule.ConstraintsJSON = append(rule.ConstraintsJSON[:0], constraintsJSON...)
	return rule, err
}

func scanPlacementDecision(row postgreslib.RowScanner) (entity.PlacementDecision, error) {
	var decision entity.PlacementDecision
	var commandID, fleetScopeID, clusterID, projectID, repositoryID pgtype.UUID
	var inputJSON []byte
	var status, runtimeMode string
	err := row.Scan(
		&decision.ID,
		&commandID,
		&decision.RequestFingerprint,
		&status,
		&fleetScopeID,
		&clusterID,
		&projectID,
		&repositoryID,
		&runtimeMode,
		&decision.RuntimeProfile,
		&inputJSON,
		&decision.ReasonCode,
		&decision.ReasonMessage,
		&decision.UsedDefaultPath,
		&decision.CreatedAt,
	)
	decision.CommandID = postgreslib.UUIDPtrFromPG(commandID)
	decision.Status = enum.PlacementDecisionStatus(status)
	decision.FleetScopeID = postgreslib.UUIDPtrFromPG(fleetScopeID)
	decision.ClusterID = postgreslib.UUIDPtrFromPG(clusterID)
	decision.ProjectID = postgreslib.UUIDPtrFromPG(projectID)
	decision.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	decision.RuntimeMode = enum.RuntimeMode(runtimeMode)
	decision.InputJSON = append(decision.InputJSON[:0], inputJSON...)
	return decision, err
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	var payload []byte
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.ActorType,
		&result.ActorID,
		&result.Operation,
		&result.AggregateType,
		&result.AggregateID,
		&payload,
		&result.CreatedAt,
	)
	result.CommandID = postgreslib.UUIDPtrFromPG(commandID)
	result.ResultPayload = append(result.ResultPayload[:0], payload...)
	return result, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	record, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(
			record.Identity.RowID,
			record.Identity.TypeName,
			record.Identity.ContractVersion,
			record.Identity.SubjectKind,
			record.Identity.SubjectID,
			record.Body,
			record.Identity.CreatedAt,
			record.Delivery.Attempts,
		),
		PublishedAt:         record.Delivery.SentAt,
		NextAttemptAt:       record.Delivery.RetryAt,
		LockedUntil:         record.Delivery.LeaseUntil,
		FailureKind:         record.Failure.FailureCode,
		FailedPermanentlyAt: record.Failure.DeadAt,
		LastError:           record.Failure.ErrorText,
	}, nil
}
