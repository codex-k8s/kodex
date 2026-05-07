package runtime

import (
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
)

func scanSlot(row postgreslib.RowScanner) (entity.Slot, error) {
	var slot entity.Slot
	var fleetScopeID pgtype.UUID
	var clusterID pgtype.UUID
	var agentRunID pgtype.UUID
	var projectID pgtype.UUID
	var repositoryIDsJSON []byte
	var leaseUntil pgtype.Timestamptz
	err := row.Scan(
		&slot.ID,
		&slot.SlotKey,
		&slot.Status,
		&slot.RuntimeMode,
		&slot.IsPrewarmed,
		&fleetScopeID,
		&clusterID,
		&slot.NamespaceName,
		&agentRunID,
		&projectID,
		&repositoryIDsJSON,
		&slot.RuntimeProfile,
		&slot.Fingerprint,
		&slot.LeaseOwner,
		&leaseUntil,
		&slot.LastErrorCode,
		&slot.LastErrorMessage,
		&slot.Version,
		&slot.CreatedAt,
		&slot.UpdatedAt,
	)
	if err != nil {
		return entity.Slot{}, err
	}
	slot.FleetScopeID = postgreslib.UUIDPtrFromPG(fleetScopeID)
	slot.ClusterID = postgreslib.UUIDPtrFromPG(clusterID)
	slot.AgentRunID = postgreslib.UUIDPtrFromPG(agentRunID)
	slot.ProjectID = postgreslib.UUIDPtrFromPG(projectID)
	slot.LeaseUntil = postgreslib.TimePtrFromPG(leaseUntil)
	if err := json.Unmarshal(repositoryIDsJSON, &slot.RepositoryIDs); err != nil {
		return entity.Slot{}, err
	}
	return slot, nil
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.Operation,
		&result.AggregateType,
		&result.AggregateID,
		&result.ResultPayload,
		&result.CreatedAt,
	)
	result.CommandID = postgreslib.UUIDPtrFromPG(commandID)
	return result, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	var event entity.OutboxEvent
	var publishedAt pgtype.Timestamptz
	var lockedUntil pgtype.Timestamptz
	var failedPermanentlyAt pgtype.Timestamptz
	err := row.Scan(
		&event.ID,
		&event.EventType,
		&event.SchemaVersion,
		&event.AggregateType,
		&event.AggregateID,
		&event.Payload,
		&event.OccurredAt,
		&publishedAt,
		&event.AttemptCount,
		&event.NextAttemptAt,
		&lockedUntil,
		&failedPermanentlyAt,
		&event.FailureKind,
		&event.LastError,
	)
	event.PublishedAt = postgreslib.TimePtrFromPG(publishedAt)
	event.LockedUntil = postgreslib.TimePtrFromPG(lockedUntil)
	event.FailedPermanentlyAt = postgreslib.TimePtrFromPG(failedPermanentlyAt)
	return event, err
}
