package controlplane

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/jackc/pgx/v5"
)

var _ domainrepo.SchedulePromptPreparationTransaction = (*transaction)(nil)

func (wrapped *transaction) GetSchedulePromptPreparation(
	ctx context.Context,
	keyHash string,
) (domainrepo.SchedulePromptPreparation, error) {
	return wrapped.getSchedulePromptPreparationForUpdate(ctx, keyHash)
}

func (wrapped *transaction) ReserveSchedulePromptPreparation(
	ctx context.Context,
	requested domainrepo.SchedulePromptPreparation,
) (domainrepo.SchedulePromptPreparation, bool, error) {
	tag, err := wrapped.tx.Exec(ctx, sqlSchedulePromptPreparationReserve, pgx.StrictNamedArgs{
		"organization_id": requested.OrganizationID, "project_id": requested.ProjectID,
		"owner_actor_id": requested.OwnerActorID, "key_hash": requested.KeyHash,
		"request_sha256": requested.RequestSHA256, "semantic_sha256": requested.SemanticSHA256,
		"action": requested.Action, "target_id": requested.TargetID,
		"expected_version": requested.ExpectedVersion, "object_key": requested.ObjectKey,
		"lease_expires_at": requested.LeaseExpiresAt, "created_at": requested.CreatedAt,
	})
	if err != nil {
		return domainrepo.SchedulePromptPreparation{}, false, mapError(err)
	}
	current, err := wrapped.getSchedulePromptPreparationForUpdate(ctx, requested.KeyHash)
	if err != nil {
		return domainrepo.SchedulePromptPreparation{}, false, err
	}
	if current.RequestSHA256 != requested.RequestSHA256 ||
		current.SemanticSHA256 != requested.SemanticSHA256 ||
		current.Action != requested.Action || current.TargetID != requested.TargetID ||
		current.ExpectedVersion != requested.ExpectedVersion ||
		current.ObjectKey != requested.ObjectKey || current.OwnerActorID != requested.OwnerActorID {
		return domainrepo.SchedulePromptPreparation{}, false, errs.ErrStateConflict
	}
	if tag.RowsAffected() == 1 {
		return current, true, nil
	}
	switch current.State {
	case "READY", "CONSUMED":
		return current, false, nil
	case "WRITING":
		if current.LeaseExpiresAt.After(requested.CreatedAt) {
			return current, false, errs.ErrUnavailable
		}
	case "AMBIGUOUS":
	default:
		return domainrepo.SchedulePromptPreparation{}, false, errs.ErrStateConflict
	}
	tag, err = wrapped.tx.Exec(ctx, sqlSchedulePromptPreparationClaim, pgx.StrictNamedArgs{
		"organization_id": requested.OrganizationID, "project_id": requested.ProjectID,
		"owner_actor_id": requested.OwnerActorID, "key_hash": requested.KeyHash,
		"expected_generation": current.Generation, "lease_expires_at": requested.LeaseExpiresAt,
		"updated_at": requested.CreatedAt,
	})
	if err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return domainrepo.SchedulePromptPreparation{}, false, mapError(err)
		}
		return domainrepo.SchedulePromptPreparation{}, false, errs.ErrStateConflict
	}
	current.Generation++
	current.State = "WRITING"
	current.LeaseExpiresAt = requested.LeaseExpiresAt
	current.ObjectReference, current.ObjectVersionID = "", ""
	current.ObjectSHA256, current.ObjectMediaType, current.ObjectSize = "", "", 0
	current.UpdatedAt = requested.CreatedAt
	return current, true, nil
}

func (wrapped *transaction) getSchedulePromptPreparationForUpdate(
	ctx context.Context,
	keyHash string,
) (domainrepo.SchedulePromptPreparation, error) {
	var result domainrepo.SchedulePromptPreparation
	var lease *time.Time
	err := wrapped.tx.QueryRow(ctx, sqlSchedulePromptPreparationGetForUpdate, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"owner_actor_id": wrapped.actorID, "key_hash": keyHash,
	}).Scan(&result.OrganizationID, &result.ProjectID, &result.OwnerActorID,
		&result.KeyHash, &result.RequestSHA256, &result.SemanticSHA256,
		&result.Action, &result.TargetID, &result.ExpectedVersion, &result.ObjectKey,
		&result.State, &result.Generation, &lease, &result.ObjectReference,
		&result.ObjectVersionID, &result.ObjectSHA256, &result.ObjectSize,
		&result.ObjectMediaType, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return domainrepo.SchedulePromptPreparation{}, mapError(err)
	}
	if lease != nil {
		result.LeaseExpiresAt = lease.UTC()
	}
	return result, nil
}

func (wrapped *transaction) CompleteSchedulePromptPreparation(
	ctx context.Context,
	preparation domainrepo.SchedulePromptPreparation,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlSchedulePromptPreparationComplete, pgx.StrictNamedArgs{
		"organization_id": preparation.OrganizationID, "project_id": preparation.ProjectID,
		"owner_actor_id": preparation.OwnerActorID, "key_hash": preparation.KeyHash,
		"request_sha256": preparation.RequestSHA256, "generation": preparation.Generation,
		"object_reference": preparation.ObjectReference, "object_version_id": preparation.ObjectVersionID,
		"object_sha256": preparation.ObjectSHA256, "object_size": preparation.ObjectSize,
		"object_media_type": preparation.ObjectMediaType, "updated_at": preparation.UpdatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) MarkSchedulePromptPreparationAmbiguous(
	ctx context.Context,
	preparation domainrepo.SchedulePromptPreparation,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlSchedulePromptPreparationAmbiguous, pgx.StrictNamedArgs{
		"organization_id": preparation.OrganizationID, "project_id": preparation.ProjectID,
		"owner_actor_id": preparation.OwnerActorID, "key_hash": preparation.KeyHash,
		"request_sha256": preparation.RequestSHA256, "generation": preparation.Generation,
		"updated_at": preparation.UpdatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) ConsumeSchedulePromptPreparation(
	ctx context.Context,
	keyHash, scheduleID string,
	generation uint64,
	expectedSHA256 string,
	scheduleVersion uint64,
	now time.Time,
) error {
	current, err := wrapped.getSchedulePromptPreparationForUpdate(ctx, keyHash)
	if err != nil {
		return err
	}
	if current.ObjectSHA256 != expectedSHA256 || current.Generation != generation ||
		(current.State != "READY" && current.State != "CONSUMED") {
		return errs.ErrStateConflict
	}
	tag, err := wrapped.tx.Exec(ctx, sqlSchedulePromptPreparationConsume, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"owner_actor_id": wrapped.actorID, "key_hash": keyHash, "generation": generation,
		"schedule_id": scheduleID, "schedule_version": scheduleVersion, "updated_at": now,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}
