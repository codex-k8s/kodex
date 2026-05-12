package fleet

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

func fleetScopeArgs(scope entity.FleetScope) pgx.NamedArgs {
	return withBaseArgs(scope.Base, pgx.NamedArgs{
		"scope_key":      scope.ScopeKey,
		"scope_type":     string(scope.ScopeType),
		"scope_owner_id": postgreslib.NullableUUID(scope.ScopeOwnerID),
		"owner_ref_json": postgreslib.JSONPayload(scope.OwnerRefJSON),
		"display_name":   scope.DisplayName,
		"status":         string(scope.Status),
		"is_default":     scope.IsDefault,
	})
}

func fleetScopeUpdateArgs(scope entity.FleetScope, previousVersion int64) pgx.NamedArgs {
	args := fleetScopeArgs(scope)
	args["previous_version"] = previousVersion
	return args
}

func serverArgs(server entity.Server) pgx.NamedArgs {
	return withBaseArgs(server.Base, pgx.NamedArgs{
		"server_key":          server.ServerKey,
		"provider_type":       string(server.ProviderType),
		"status":              string(server.Status),
		"primary_address_ref": server.PrimaryAddressRef,
		"region":              server.Region,
		"capacity_class":      server.CapacityClass,
		"secret_store_type":   server.SecretStoreType,
		"secret_store_ref":    server.SecretStoreRef,
	})
}

func serverUpdateArgs(server entity.Server, previousVersion int64) pgx.NamedArgs {
	args := serverArgs(server)
	args["previous_version"] = previousVersion
	return args
}

func kubernetesClusterArgs(cluster entity.KubernetesCluster) pgx.NamedArgs {
	return withBaseArgs(cluster.Base, pgx.NamedArgs{
		"fleet_scope_id":         cluster.FleetScopeID,
		"server_id":              postgreslib.NullableUUID(cluster.ServerID),
		"cluster_key":            cluster.ClusterKey,
		"status":                 string(cluster.Status),
		"is_default":             cluster.IsDefault,
		"api_endpoint_ref":       cluster.APIEndpointRef,
		"secret_store_type":      cluster.SecretStoreType,
		"secret_store_ref":       cluster.SecretStoreRef,
		"kubernetes_version":     cluster.KubernetesVersion,
		"region":                 cluster.Region,
		"capacity_class":         cluster.CapacityClass,
		"last_health_status":     string(cluster.LastHealthStatus),
		"last_health_checked_at": postgreslib.NullableTime(cluster.LastHealthCheckedAt),
	})
}

func kubernetesClusterUpdateArgs(cluster entity.KubernetesCluster, previousVersion int64) pgx.NamedArgs {
	args := kubernetesClusterArgs(cluster)
	args["previous_version"] = previousVersion
	return args
}

func kubernetesClusterHealthArgs(cluster entity.KubernetesCluster) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                     cluster.ID,
		"last_health_status":     string(cluster.LastHealthStatus),
		"last_health_checked_at": postgreslib.NullableTime(cluster.LastHealthCheckedAt),
	}
}

func clusterConnectivityCheckArgs(check entity.ClusterConnectivityCheck) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":            check.ID,
		"cluster_id":    check.ClusterID,
		"status":        string(check.Status),
		"started_at":    postgreslib.NullableTime(check.StartedAt),
		"finished_at":   postgreslib.NullableTime(check.FinishedAt),
		"latency_ms":    nullableInt64(check.LatencyMS),
		"error_code":    check.ErrorCode,
		"error_message": check.ErrorMessage,
		"created_at":    check.CreatedAt,
	}
}

func clusterHealthSnapshotArgs(snapshot entity.ClusterHealthSnapshot) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":              snapshot.ID,
		"cluster_id":      snapshot.ClusterID,
		"health_status":   string(snapshot.HealthStatus),
		"capacity_status": string(snapshot.CapacityStatus),
		"summary_json":    postgreslib.JSONPayload(snapshot.SummaryJSON),
		"checked_at":      snapshot.CheckedAt,
		"error_code":      snapshot.ErrorCode,
		"error_message":   snapshot.ErrorMessage,
	}
}

func placementRuleArgs(rule entity.PlacementRule) pgx.NamedArgs {
	return withBaseArgs(rule.Base, pgx.NamedArgs{
		"fleet_scope_id":   rule.FleetScopeID,
		"rule_key":         rule.RuleKey,
		"status":           string(rule.Status),
		"priority":         rule.Priority,
		"match_json":       postgreslib.JSONPayload(rule.MatchJSON),
		"constraints_json": postgreslib.JSONPayload(rule.ConstraintsJSON),
	})
}

func placementRuleUpdateArgs(rule entity.PlacementRule, previousVersion int64) pgx.NamedArgs {
	args := placementRuleArgs(rule)
	args["previous_version"] = previousVersion
	return args
}

func placementDecisionArgs(decision entity.PlacementDecision) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  decision.ID,
		"command_id":          nullableCommandID(decision.CommandID),
		"request_fingerprint": decision.RequestFingerprint,
		"status":              string(decision.Status),
		"fleet_scope_id":      postgreslib.NullableUUID(decision.FleetScopeID),
		"cluster_id":          postgreslib.NullableUUID(decision.ClusterID),
		"project_id":          postgreslib.NullableUUID(decision.ProjectID),
		"repository_id":       postgreslib.NullableUUID(decision.RepositoryID),
		"runtime_mode":        string(decision.RuntimeMode),
		"runtime_profile":     decision.RuntimeProfile,
		"input_json":          postgreslib.JSONPayload(decision.InputJSON),
		"reason_code":         decision.ReasonCode,
		"reason_message":      decision.ReasonMessage,
		"used_default_path":   decision.UsedDefaultPath,
		"created_at":          decision.CreatedAt,
	}
}

func clusterHealthSnapshotFilterArgs(filter query.ClusterHealthSnapshotFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"cluster_id":    filter.ClusterID,
		"checked_since": postgreslib.NullableTime(filter.CheckedSince),
	})
}

func placementRuleFilterArgs(filter query.PlacementRuleFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"fleet_scope_id": postgreslib.NullableUUID(filter.FleetScopeID),
		"statuses":       postgreslib.StringValues(filter.Statuses),
	})
}

func placementDecisionFilterArgs(filter query.PlacementDecisionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_id":     postgreslib.NullableUUID(filter.ProjectID),
		"repository_id":  postgreslib.NullableUUID(filter.RepositoryID),
		"fleet_scope_id": postgreslib.NullableUUID(filter.FleetScopeID),
		"cluster_id":     postgreslib.NullableUUID(filter.ClusterID),
		"statuses":       postgreslib.StringValues(filter.Statuses),
	})
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"command_id":      postgreslib.NullableCommandID(identity.CommandID),
		"idempotency_key": postgreslib.IdempotencyLookupKey(identity.CommandID, identity.IdempotencyKey),
		"operation":       identity.Operation,
		"actor_type":      identity.Actor.Type,
		"actor_id":        identity.Actor.ID,
	}
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	return pgx.NamedArgs{
		"key":             result.Key,
		"command_id":      nullableCommandID(result.CommandID),
		"idempotency_key": result.IdempotencyKey,
		"actor_type":      result.ActorType,
		"actor_id":        result.ActorID,
		"operation":       result.Operation,
		"aggregate_type":  result.AggregateType,
		"aggregate_id":    result.AggregateID,
		"result_payload":  postgreslib.JSONPayload(result.ResultPayload),
		"created_at":      result.CreatedAt,
	}
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	return postgreslib.OutboxCreateArgs(
		event.ID,
		event.EventType,
		event.SchemaVersion,
		event.AggregateType,
		event.AggregateID,
		event.Payload,
		event.OccurredAt,
		event.PublishedAt,
	)
}

func fleetScopeFilterArgs(filter query.FleetScopeFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_types":    postgreslib.StringValues(filter.ScopeTypes),
		"statuses":       postgreslib.StringValues(filter.Statuses),
		"scope_owner_id": postgreslib.NullableUUID(filter.ScopeOwnerID),
		"is_default":     nullableBool(filter.IsDefault),
	})
}

func serverFilterArgs(filter query.ServerFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"statuses":       postgreslib.StringValues(filter.Statuses),
		"provider_types": postgreslib.StringValues(filter.ProviderTypes),
		"region":         filter.Region,
		"capacity_class": filter.CapacityClass,
	})
}

func kubernetesClusterFilterArgs(filter query.KubernetesClusterFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"fleet_scope_id":  postgreslib.NullableUUID(filter.FleetScopeID),
		"server_id":       postgreslib.NullableUUID(filter.ServerID),
		"statuses":        postgreslib.StringValues(filter.Statuses),
		"health_statuses": postgreslib.StringValues(filter.HealthStatuses),
		"region":          filter.Region,
		"capacity_class":  filter.CapacityClass,
		"is_default":      nullableBool(filter.IsDefault),
	})
}

type pageQueryArgs struct {
	args       pgx.NamedArgs
	limit      int32
	nextOffset int32
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	limit, offset, nextOffset := pageBounds(page)
	args["limit"] = limit + 1
	args["offset"] = offset
	return pageQueryArgs{args: args, limit: limit, nextOffset: nextOffset}
}

func pageBounds(page value.PageRequest) (limit int32, offset int32, nextOffset int32) {
	limit, offset, nextOffset = postgreslib.OffsetPageBounds(page.PageSize, page.PageToken, defaultPageSize, maxPageSize)
	return limit, offset, nextOffset
}

func pageResult[T any](items []T, limit int32, nextOffset int32) ([]T, value.PageResult) {
	values, token := postgreslib.TrimOffsetPage(items, limit, nextOffset)
	if token == "" {
		return values, value.PageResult{}
	}
	return values, value.PageResult{NextPageToken: token}
}

func withBaseArgs(base entity.Base, args pgx.NamedArgs) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(args, base.ID, base.Version, base.CreatedAt, base.UpdatedAt)
}

func nullableCommandID(id *uuid.UUID) any {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
