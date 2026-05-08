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
	var activeWorkspaceMaterializationID pgtype.UUID
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
		&activeWorkspaceMaterializationID,
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
	slot.ActiveWorkspaceMaterializationID = postgreslib.UUIDPtrFromPG(activeWorkspaceMaterializationID)
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
		&result.Actor.Type,
		&result.Actor.ID,
		&result.Operation,
		&result.AggregateType,
		&result.AggregateID,
		&result.ResultPayload,
		&result.CreatedAt,
	)
	result.CommandID = postgreslib.UUIDPtrFromPG(commandID)
	return result, err
}

func scanWorkspaceMaterialization(row postgreslib.RowScanner) (entity.WorkspaceMaterialization, error) {
	var materialization entity.WorkspaceMaterialization
	var sourcesJSON []byte
	var startedAt pgtype.Timestamptz
	var finishedAt pgtype.Timestamptz
	err := row.Scan(
		&materialization.ID,
		&materialization.SlotID,
		&materialization.Status,
		&materialization.PolicyDigest,
		&sourcesJSON,
		&materialization.Fingerprint,
		&startedAt,
		&finishedAt,
		&materialization.LastErrorCode,
		&materialization.LastErrorMessage,
		&materialization.Version,
		&materialization.CreatedAt,
		&materialization.UpdatedAt,
	)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if err := json.Unmarshal(sourcesJSON, &materialization.Sources); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	materialization.StartedAt = postgreslib.TimePtrFromPG(startedAt)
	materialization.FinishedAt = postgreslib.TimePtrFromPG(finishedAt)
	return materialization, nil
}

func scanJob(row postgreslib.RowScanner) (entity.Job, error) {
	var job entity.Job
	var leaseUntil pgtype.Timestamptz
	var slotID pgtype.UUID
	var agentRunID pgtype.UUID
	var projectID pgtype.UUID
	var repositoryID pgtype.UUID
	var releaseLineID pgtype.UUID
	var packageInstallationID pgtype.UUID
	var fleetScopeID pgtype.UUID
	var clusterID pgtype.UUID
	var requestedBy pgtype.UUID
	var startedAt pgtype.Timestamptz
	var finishedAt pgtype.Timestamptz
	err := row.Scan(
		&job.ID,
		&job.CommandID,
		&job.JobType,
		&job.Status,
		&job.Priority,
		&job.JobInputJSON,
		&job.LeaseOwner,
		&job.LeaseTokenHash,
		&leaseUntil,
		&job.ClaimAttempt,
		&slotID,
		&agentRunID,
		&projectID,
		&repositoryID,
		&releaseLineID,
		&packageInstallationID,
		&fleetScopeID,
		&clusterID,
		&requestedBy,
		&job.CreatedAt,
		&startedAt,
		&finishedAt,
		&job.NextAction,
		&job.LastErrorCode,
		&job.LastErrorMessage,
		&job.ShortLogTail,
		&job.FullLogRef,
		&job.UpdatedAt,
		&job.Version,
	)
	if err != nil {
		return entity.Job{}, err
	}
	job.LeaseUntil = postgreslib.TimePtrFromPG(leaseUntil)
	job.SlotID = postgreslib.UUIDPtrFromPG(slotID)
	job.AgentRunID = postgreslib.UUIDPtrFromPG(agentRunID)
	job.ProjectID = postgreslib.UUIDPtrFromPG(projectID)
	job.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	job.ReleaseLineID = postgreslib.UUIDPtrFromPG(releaseLineID)
	job.PackageInstallationID = postgreslib.UUIDPtrFromPG(packageInstallationID)
	job.FleetScopeID = postgreslib.UUIDPtrFromPG(fleetScopeID)
	job.ClusterID = postgreslib.UUIDPtrFromPG(clusterID)
	job.RequestedBy = postgreslib.UUIDPtrFromPG(requestedBy)
	job.StartedAt = postgreslib.TimePtrFromPG(startedAt)
	job.FinishedAt = postgreslib.TimePtrFromPG(finishedAt)
	return job, nil
}

func scanJobStep(row postgreslib.RowScanner) (entity.JobStep, error) {
	var step entity.JobStep
	var startedAt pgtype.Timestamptz
	var finishedAt pgtype.Timestamptz
	err := row.Scan(
		&step.ID,
		&step.JobID,
		&step.StepKey,
		&step.Status,
		&startedAt,
		&finishedAt,
		&step.ShortLogTail,
		&step.ExternalRef,
		&step.ErrorCode,
		&step.ErrorMessage,
		&step.Version,
		&step.CreatedAt,
		&step.UpdatedAt,
	)
	if err != nil {
		return entity.JobStep{}, err
	}
	step.StartedAt = postgreslib.TimePtrFromPG(startedAt)
	step.FinishedAt = postgreslib.TimePtrFromPG(finishedAt)
	return step, nil
}

func scanRuntimeArtifactRef(row postgreslib.RowScanner) (entity.RuntimeArtifactRef, error) {
	var ref entity.RuntimeArtifactRef
	var jobID pgtype.UUID
	var slotID pgtype.UUID
	err := row.Scan(
		&ref.ID,
		&jobID,
		&slotID,
		&ref.ArtifactType,
		&ref.ExternalRef,
		&ref.Digest,
		&ref.MetadataJSON,
		&ref.CreatedAt,
	)
	if err != nil {
		return entity.RuntimeArtifactRef{}, err
	}
	ref.JobID = postgreslib.UUIDPtrFromPG(jobID)
	ref.SlotID = postgreslib.UUIDPtrFromPG(slotID)
	return ref, nil
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
