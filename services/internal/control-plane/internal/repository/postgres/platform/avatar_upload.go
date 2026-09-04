package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type avatarUploadReservation struct {
	ref, artifactRef, objectKey, objectVersion, objectETag, state string
	version                                                       int64
}

type avatarCompensationTask struct {
	ref, objectKey, objectVersion, objectETag, digest string
	sizeBytes, version                                int64
}

func (repository *Repository) UploadAgentAvatar(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	input platformrepo.AgentAvatarUpload,
) (entity.Agent, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Agent{}, err
	}
	reservation, replay, err := repository.reserveAgentAvatarUpload(ctx, current, mutation, input)
	if err != nil || replay != nil {
		if replay != nil {
			return *replay, nil
		}
		return entity.Agent{}, err
	}
	if reservation.state != "RESERVED" && reservation.state != "MATERIALIZED" {
		return entity.Agent{}, errs.ErrConflict
	}
	receipt, err := repository.resolveOrMaterializeAvatarObject(ctx, reservation, input.ArtifactUpload)
	if err != nil {
		return entity.Agent{}, err
	}
	if err := repository.markAgentAvatarMaterialized(ctx, current, reservation, receipt); err != nil {
		cleanupErr := repository.compensateAgentAvatarObject(ctx, current, reservation.ref, receipt)
		return entity.Agent{}, errors.Join(err, cleanupErr)
	}
	item, err := repository.finalizeAgentAvatarUpload(ctx, principal, current, mutation, input, reservation.ref, receipt)
	if err != nil {
		cleanupErr := repository.compensateAgentAvatarObject(ctx, current, reservation.ref, receipt)
		return entity.Agent{}, errors.Join(err, cleanupErr)
	}
	return item, nil
}

func (repository *Repository) reserveAgentAvatarUpload(
	ctx context.Context,
	current scope,
	mutation value.Mutation,
	input platformrepo.AgentAvatarUpload,
) (avatarUploadReservation, *entity.Agent, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return avatarUploadReservation{}, nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID,
		mutation.Operation, mutation.IdempotencyKey); err != nil {
		return avatarUploadReservation{}, nil, errs.ErrUnavailable
	}
	var storedDigest string
	var stored []byte
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, mutation.Operation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if err == nil {
		if storedDigest != mutation.IntentDigest {
			return avatarUploadReservation{}, nil, errs.ErrIdempotencyReuse
		}
		var item entity.Agent
		if json.Unmarshal(stored, &item) != nil {
			return avatarUploadReservation{}, nil, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return avatarUploadReservation{}, nil, errs.ErrConflict
		}
		return avatarUploadReservation{}, &item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return avatarUploadReservation{}, nil, errs.ErrUnavailable
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, current, input.AgentRef)
	if err != nil {
		return avatarUploadReservation{}, nil, err
	}
	if agent.projectID == "" || agent.projectRef != input.ProjectRef {
		return avatarUploadReservation{}, nil, errs.ErrNotFound
	}
	if agent.agentVersion != input.ExpectedVersion {
		return avatarUploadReservation{}, nil, errs.ErrVersionMismatch
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: input.ProjectRef, ResourceKind: "AGENT", ResourceRef: input.AgentRef,
	})
	if err != nil {
		return avatarUploadReservation{}, nil, err
	}
	if err := repository.requireAccess(ctx, tx, current, "agent.avatar.manage", target); err != nil {
		return avatarUploadReservation{}, nil, errs.ErrNotFound
	}
	reservationRef, refErr := newRef("avres")
	artifactRef, artifactErr := newRef("art")
	if refErr != nil || artifactErr != nil {
		return avatarUploadReservation{}, nil, errs.ErrUnavailable
	}
	fileName := safeFileName(input.FileName)
	objectKey := artifactObjectKey(current.organizationRef, current.actorRef, input.ProjectRef, artifactRef, input.Digest)
	var reservation avatarUploadReservation
	err = tx.QueryRow(ctx, queryAvatarUploadReserve, pgx.StrictNamedArgs{
		"reservation_ref": reservationRef, "organization_id": current.organizationID,
		"project_id": agent.projectID, "agent_id": agent.id, "actor_id": current.actorID,
		"idempotency_key": mutation.IdempotencyKey, "intent_digest": mutation.IntentDigest,
		"expected_agent_version": input.ExpectedVersion, "artifact_ref": artifactRef,
		"file_name": fileName, "media_type": input.MediaType, "size_bytes": input.SizeBytes,
		"digest": input.Digest, "object_key": objectKey,
	}).Scan(&reservation.ref, &reservation.artifactRef, &reservation.objectKey,
		&reservation.objectVersion, &reservation.objectETag, &reservation.state, &reservation.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return avatarUploadReservation{}, nil, errs.ErrIdempotencyReuse
	}
	if err != nil {
		return avatarUploadReservation{}, nil, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return avatarUploadReservation{}, nil, errs.ErrConflict
	}
	return reservation, nil, nil
}

func (repository *Repository) resolveOrMaterializeAvatarObject(
	ctx context.Context,
	reservation avatarUploadReservation,
	input platformrepo.ArtifactUpload,
) (objectstorage.Receipt, error) {
	if reservation.state == "MATERIALIZED" {
		receipt, err := repository.objects.Head(ctx, reservation.objectKey, reservation.objectVersion)
		if err != nil {
			return objectstorage.Receipt{}, mapObjectStorageError(err)
		}
		if receipt.ETag != reservation.objectETag || receipt.Digest != input.Digest || receipt.SizeBytes != input.SizeBytes {
			return objectstorage.Receipt{}, errs.ErrConflict
		}
		return receipt, nil
	}
	if receipt, err := repository.objects.Head(ctx, reservation.objectKey, ""); err == nil {
		if receipt.Digest != input.Digest || receipt.SizeBytes != input.SizeBytes || receipt.ETag == "" {
			return objectstorage.Receipt{}, errs.ErrConflict
		}
		return receipt, nil
	} else if !errors.Is(err, objectstorage.ErrNotFound) {
		return objectstorage.Receipt{}, mapObjectStorageError(err)
	}
	return repository.putArtifactObject(ctx, reservation.objectKey, input)
}

func (repository *Repository) markAgentAvatarMaterialized(
	ctx context.Context,
	current scope,
	reservation avatarUploadReservation,
	receipt objectstorage.Receipt,
) error {
	if receipt.ETag == "" || receipt.Key != reservation.objectKey {
		return errs.ErrConflict
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	err = tx.QueryRow(ctx, queryAvatarUploadMarkMaterialized, pgx.StrictNamedArgs{
		"reservation_ref": reservation.ref, "organization_id": current.organizationID,
		"object_key": receipt.Key, "object_version": receipt.VersionID, "object_etag": receipt.ETag,
		"digest": receipt.Digest, "size_bytes": receipt.SizeBytes,
	}).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrConflict
	}
	return nil
}

func (repository *Repository) finalizeAgentAvatarUpload(
	ctx context.Context,
	principal value.Principal,
	current scope,
	mutation value.Mutation,
	input platformrepo.AgentAvatarUpload,
	reservationRef string,
	receipt objectstorage.Receipt,
) (entity.Agent, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID,
		mutation.Operation, mutation.IdempotencyKey); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	var projectID, agentID, artifactRef, fileName, mediaType, digest, objectKey, objectVersion, objectETag, state string
	var sizeBytes, expectedAgentVersion, reservationVersion int64
	err = tx.QueryRow(ctx, queryAvatarUploadLockReservation, pgx.StrictNamedArgs{
		"reservation_ref": reservationRef, "organization_id": current.organizationID,
		"actor_id": current.actorID, "idempotency_key": mutation.IdempotencyKey,
		"intent_digest": mutation.IntentDigest,
	}).Scan(&projectID, &agentID, &artifactRef, &fileName, &mediaType, &sizeBytes, &digest,
		&objectKey, &objectVersion, &objectETag, &state, &expectedAgentVersion, &reservationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrConflict
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	if state != "MATERIALIZED" || projectID == "" || expectedAgentVersion != input.ExpectedVersion ||
		objectKey != receipt.Key || objectVersion != receipt.VersionID || objectETag != receipt.ETag ||
		digest != receipt.Digest || sizeBytes != receipt.SizeBytes {
		return entity.Agent{}, errs.ErrConflict
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, current, input.AgentRef)
	if err != nil {
		return entity.Agent{}, err
	}
	if agent.id != agentID || agent.projectID != projectID || agent.projectRef != input.ProjectRef {
		return entity.Agent{}, errs.ErrNotFound
	}
	if agent.agentVersion != expectedAgentVersion {
		return entity.Agent{}, errs.ErrVersionMismatch
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: input.ProjectRef, ResourceKind: "AGENT", ResourceRef: input.AgentRef,
	})
	if err != nil {
		return entity.Agent{}, err
	}
	if err := repository.requireAccess(ctx, tx, current, "agent.avatar.manage", target); err != nil {
		return entity.Agent{}, errs.ErrNotFound
	}
	receiptRef, _ := newRef("obj")
	var artifact entity.Artifact
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName,
		artifactRef, current.organizationID, projectID, "", fileName, mediaType, sizeBytes, digest,
		"CLEAN", receiptRef, "AVAILABLE", current.actorID).Scan(
		&artifact.Ref, &artifact.FileName, &artifact.MediaType, &artifact.SizeBytes, &artifact.Digest,
		&artifact.ScanState, &artifact.PreviewState, &artifact.Revision, &artifact.Version, &artifact.CreatedAt)
	if err != nil {
		return entity.Agent{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertArtifactContentArtifactId,
		artifactRef, objectKey, objectVersion, objectETag, digest, sizeBytes); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	var item entity.Agent
	err = tx.QueryRow(ctx, queryCommandsChangeAgentAvatarURL, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": input.AgentRef,
		"expected_version": expectedAgentVersion, "avatar_url": avatarArtifactContentURL(artifactRef),
	}).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription,
		&item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	item.Avatar, err = repository.syncAgentAvatar(ctx, tx, current, item.Ref, artifactRef)
	if err != nil {
		return entity.Agent{}, err
	}
	if err := tx.QueryRow(ctx, queryCommandsChangeagentSelectAgentsRef, item.Ref).Scan(
		&item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName,
		&item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model,
		&item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs,
		&item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision,
	); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	setAgentAvatarReadback(&item)
	item.NextActions = agentActions(item, true, true)
	var finalizedVersion int64
	err = tx.QueryRow(ctx, queryAvatarUploadMarkFinalized, pgx.StrictNamedArgs{
		"reservation_ref": reservationRef, "expected_reservation_version": reservationVersion,
		"artifact_ref": artifactRef, "object_key": objectKey, "object_version": objectVersion,
		"object_etag": objectETag,
	}).Scan(&finalizedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrConflict
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	auditRef, _ := newRef("aud")
	if _, err := tx.Exec(ctx, queryAvatarUploadInsertAudit, pgx.StrictNamedArgs{
		"audit_ref": auditRef, "organization_id": current.organizationID, "project_id": projectID,
		"actor_id": current.actorID, "agent_ref": item.Ref, "correlation_ref": principal.CorrelationRef,
	}); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "AGENT_CHANGED", item.ProjectRef,
		item.Ref, "i18n:AGENT_AVATAR_UPDATED", item.Version, item.State); err != nil {
		return entity.Agent{}, err
	}
	encoded, _ := json.Marshal(item)
	if _, err := tx.Exec(ctx, queryAvatarUploadInsertIdempotencyReceipt, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"idempotency_key": mutation.IdempotencyKey, "intent_digest": mutation.IntentDigest,
		"response_payload": encoded,
	}); err != nil {
		return entity.Agent{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Agent{}, errs.ErrConflict
	}
	return item, nil
}

func (repository *Repository) compensateAgentAvatarObject(
	ctx context.Context,
	current scope,
	reservationRef string,
	receipt objectstorage.Receipt,
) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := repository.pool.BeginTx(cleanupContext, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(cleanupContext) }()
	var task avatarCompensationTask
	err = tx.QueryRow(cleanupContext, queryAvatarUploadClaimCompensation, pgx.StrictNamedArgs{
		"reservation_ref": reservationRef, "organization_id": current.organizationID,
		"object_key": receipt.Key, "object_version": receipt.VersionID, "object_etag": receipt.ETag,
		"digest": receipt.Digest, "size_bytes": receipt.SizeBytes,
	}).Scan(&task.ref, &task.objectKey, &task.objectVersion, &task.objectETag,
		&task.digest, &task.sizeBytes, &task.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if err := tx.Commit(cleanupContext); err != nil {
		return errs.ErrConflict
	}
	if err := repository.objects.Delete(cleanupContext, task.objectKey, task.objectVersion); err != nil &&
		!errors.Is(err, objectstorage.ErrNotFound) {
		return mapObjectStorageError(err)
	}
	if err := repository.verifyAgentAvatarObjectDeleted(cleanupContext, task); err != nil {
		return err
	}
	return repository.completeAgentAvatarCompensation(cleanupContext, task)
}

func (repository *Repository) CleanupExpiredAgentAvatarUploads(ctx context.Context, limit int32) error {
	if limit < 1 || limit > 100 {
		return errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryAvatarUploadClaimExpired, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return fmt.Errorf("claim expired agent avatar uploads: %w", errs.ErrUnavailable)
	}
	var tasks []avatarCompensationTask
	for rows.Next() {
		var task avatarCompensationTask
		if err := rows.Scan(&task.ref, &task.objectKey, &task.objectVersion, &task.objectETag,
			&task.digest, &task.sizeBytes, &task.version); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errs.ErrUnavailable
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrConflict
	}
	var cleanupErrors []error
	for _, task := range tasks {
		if task.objectETag == "" {
			resolved, resolveErr := repository.resolveExpiredAgentAvatarDescriptor(ctx, task)
			if resolveErr != nil {
				cleanupErrors = append(cleanupErrors, resolveErr)
				continue
			}
			task = resolved
		}
		if err := repository.objects.Delete(ctx, task.objectKey, task.objectVersion); err != nil &&
			!errors.Is(err, objectstorage.ErrNotFound) {
			cleanupErrors = append(cleanupErrors, mapObjectStorageError(err))
			continue
		}
		if err := repository.verifyAgentAvatarObjectDeleted(ctx, task); err != nil {
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
		if err := repository.completeAgentAvatarCompensation(ctx, task); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

func (repository *Repository) resolveExpiredAgentAvatarDescriptor(
	ctx context.Context,
	task avatarCompensationTask,
) (avatarCompensationTask, error) {
	receipt, err := repository.objects.Head(ctx, task.objectKey, "")
	if errors.Is(err, objectstorage.ErrNotFound) {
		return task, nil
	}
	if err != nil {
		return avatarCompensationTask{}, mapObjectStorageError(err)
	}
	if receipt.Key != task.objectKey || receipt.ETag == "" || receipt.Digest != task.digest || receipt.SizeBytes != task.sizeBytes {
		return avatarCompensationTask{}, errs.ErrConflict
	}
	err = repository.pool.QueryRow(ctx, queryAvatarUploadRecordCompensationDescriptor, pgx.StrictNamedArgs{
		"reservation_ref": task.ref, "expected_version": task.version, "object_key": receipt.Key,
		"object_version": receipt.VersionID, "object_etag": receipt.ETag, "digest": receipt.Digest,
		"size_bytes": receipt.SizeBytes,
	}).Scan(&task.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return avatarCompensationTask{}, errs.ErrConflict
	}
	if err != nil {
		return avatarCompensationTask{}, errs.ErrUnavailable
	}
	task.objectVersion, task.objectETag = receipt.VersionID, receipt.ETag
	return task, nil
}

func (repository *Repository) verifyAgentAvatarObjectDeleted(ctx context.Context, task avatarCompensationTask) error {
	if _, err := repository.objects.Head(ctx, task.objectKey, task.objectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		if err == nil {
			return errs.ErrConflict
		}
		return mapObjectStorageError(err)
	}
	return nil
}

func (repository *Repository) completeAgentAvatarCompensation(ctx context.Context, task avatarCompensationTask) error {
	tag, err := repository.pool.Exec(ctx, queryAvatarUploadCompleteCompensation, pgx.StrictNamedArgs{
		"reservation_ref": task.ref, "expected_version": task.version, "object_key": task.objectKey,
		"object_version": task.objectVersion, "object_etag": task.objectETag, "digest": task.digest,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
