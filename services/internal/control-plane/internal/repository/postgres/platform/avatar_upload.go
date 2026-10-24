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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// avatarUploadReservation stores the private server reservation/artifact/object locators,
// materialized version/ETag, lifecycle state and OCC version. It is metadata for the upload owner
// protocol, not a caller grant or proof that an artifact was adopted.
// avatarUploadReservation хранит закрытые серверные ссылки reservation, artifact и object,
// materialized version и ETag, состояние и OCC version. Это metadata протокола владельца загрузки,
// не клиентское разрешение и не доказательство принятия artifact.
type avatarUploadReservation struct {
	ref, artifactRef, objectKey, objectVersion, objectETag, state string
	version                                                       int64
}

// avatarCompensationTask pins a claimed reservation to object key/version/ETag, digest/size and
// reservation version for exact deletion and ACK. An empty descriptor may require authoritative
// Head resolution before use; the struct alone grants no cleanup authority.
// avatarCompensationTask связывает захваченную reservation с key, version, ETag, digest, size и
// версией reservation для точных удаления и ACK. Пустой дескриптор может требовать разрешения
// через Head; структура сама не выдаёт cleanup authority.
type avatarCompensationTask struct {
	ref, objectKey, objectVersion, objectETag, digest string
	sizeBytes, version                                int64
}

// UploadAgentAvatar resolves scope and reserves or replays an authorized avatar upload, scans
// content before object materialization, records the exact receipt and atomically finalizes owner
// state. A materialization-mark or finalization failure triggers separately bounded durable
// compensation and joins cleanup errors. Existing replay bypasses new object work; no committed
// artifact is adopted merely from a storage receipt.
// UploadAgentAvatar разрешает scope, резервирует либо повторяет авторизованную загрузку, сканирует
// содержимое до materialization, сохраняет точную квитанцию и атомарно завершает состояние
// владельца. Ошибка mark либо finalize запускает отдельную ограниченную durable compensation с
// объединением ошибок. Replay не создаёт object work; одна storage receipt не означает принятие
// artifact.
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
	scan, err := repository.scanArtifact(ctx, input.Reader, input.Digest, input.SizeBytes)
	if err != nil {
		return entity.Agent{}, err
	}
	if scan.Infected {
		return entity.Agent{}, errs.ErrInvalid
	}
	receipt, err := repository.resolveOrMaterializeAvatarObject(ctx, reservation, input.ArtifactUpload)
	if err != nil {
		return entity.Agent{}, err
	}
	if err := repository.markAgentAvatarMaterialized(ctx, current, reservation, receipt); err != nil {
		cleanupErr := repository.compensateAgentAvatarObject(ctx, current, reservation.ref, receipt)
		return entity.Agent{}, errors.Join(err, cleanupErr)
	}
	item, err := repository.finalizeAgentAvatarUpload(ctx, principal, current, mutation, input, reservation.ref, receipt, scan)
	if err != nil {
		cleanupErr := repository.compensateAgentAvatarObject(ctx, current, reservation.ref, receipt)
		return entity.Agent{}, errors.Join(err, cleanupErr)
	}
	return item, nil
}

// reserveAgentAvatarUpload uses ReadCommitted and an actor/operation/key advisory lock, locks the
// exact agent/project and checks avatar.manage before receipt replay. Replay requires matching
// intent/resource and fresh agent.view/action filtering; fresh work also checks agent version and
// persists a server-generated reservation/object key. Commit errors are Conflict; caller
// input/operation validation remains upstream and deferred request-context rollback errors are
// ignored.
// reserveAgentAvatarUpload использует ReadCommitted и advisory lock субъекта, операции и ключа,
// блокирует точный agent/project и проверяет avatar.manage до replay. Повтор требует совпадения
// intent и ресурса, свежих agent.view и action filtering; новая работа также проверяет версию
// агента и сохраняет серверные reservation и object key. Ошибки commit — Conflict; вход и
// operation валидируются выше, ошибки request-context rollback игнорируются.
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
	agent, err := repository.lockRuntimeAgent(ctx, tx, current, input.AgentRef)
	if err != nil {
		return avatarUploadReservation{}, nil, err
	}
	if agent.projectID == "" || agent.projectRef != input.ProjectRef {
		return avatarUploadReservation{}, nil, errs.ErrNotFound
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
		if item.Ref != input.AgentRef || item.ProjectRef != input.ProjectRef {
			return avatarUploadReservation{}, nil, errs.ErrConflict
		}
		if err := repository.requireAccess(ctx, tx, current, "agent.view", target); err != nil {
			return avatarUploadReservation{}, nil, errs.ErrNotFound
		}
		result := command.Result{Agent: &item}
		if err := repository.applyResultActionPermissions(ctx, tx, current, &result, item.ProjectRef); err != nil {
			return avatarUploadReservation{}, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return avatarUploadReservation{}, nil, errs.ErrConflict
		}
		return avatarUploadReservation{}, &item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return avatarUploadReservation{}, nil, errs.ErrUnavailable
	}
	if agent.agentVersion != input.ExpectedVersion {
		return avatarUploadReservation{}, nil, errs.ErrVersionMismatch
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

// resolveOrMaterializeAvatarObject reuses MATERIALIZED exact-version metadata only when
// ETag/digest/size match, otherwise checks the reserved key before creating content. An already
// existing object must match digest/size and have an ETag; errors other than NotFound do not
// authorize another Put. It returns a storage receipt without adopting owner state.
// resolveOrMaterializeAvatarObject повторно использует MATERIALIZED exact-version metadata только
// при совпадении ETag, digest и size; иначе проверяет зарезервированный ключ до записи. Имеющийся
// объект требует совпадения digest и size и непустого ETag; ошибки кроме NotFound не разрешают ещё
// один Put. Квитанция не принимает состояние владельца.
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

// markAgentAvatarMaterialized requires receipt key/ETag, writes its exact version/digest/size
// through guarded reservation SQL in ReadCommitted and commits the new materialized marker.
// Missing guarded row or commit failure is Conflict; other SQL failures are Unavailable. No
// agent/avatar publication occurs here.
// markAgentAvatarMaterialized требует receipt key и ETag, пишет точные version, digest и size
// защищённым SQL reservation в ReadCommitted и фиксирует marker materialized. Отсутствие строки
// либо commit failure — Conflict, прочие SQL failures — Unavailable. Публикации avatar агента
// здесь нет.
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

// finalizeAgentAvatarUpload checks clean scan/receipt agreement, then serializably locks actor
// idempotency, exact reservation and agent with fresh project/avatar authority and OCC.
// Artifact/content creation, avatar binding/readback, reservation FINALIZED, audit/event/cards and
// receipt commit together. Any failure returns an empty agent for outer compensation; ignored
// reference/JSON helper errors and existing commit mappings remain unchanged. No storage Delete
// occurs inside the owner transaction.
// finalizeAgentAvatarUpload проверяет clean scan и receipt, затем serializable блокирует
// idempotency субъекта, точную reservation и агента со свежими project/avatar authority и OCC.
// Artifact, content, avatar binding и readback, FINALIZED, audit, event, cards и receipt
// фиксируются вместе. При ошибке агент пуст для внешней компенсации; игнорируемые ошибки reference
// и JSON и commit mappings сохранены. Storage Delete внутри owner TX отсутствует.
func (repository *Repository) finalizeAgentAvatarUpload(
	ctx context.Context,
	principal value.Principal,
	current scope,
	mutation value.Mutation,
	input platformrepo.AgentAvatarUpload,
	reservationRef string,
	receipt objectstorage.Receipt,
	scan value.ArtifactScan,
) (entity.Agent, error) {
	scanJSON, err := artifactAntivirusJSON(scan)
	if err != nil || scan.Infected || scan.Digest != receipt.Digest || scan.SizeBytes != receipt.SizeBytes {
		return entity.Agent{}, errs.ErrConflict
	}
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
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: input.ProjectRef, ResourceKind: "AGENT", ResourceRef: input.AgentRef,
	})
	if err != nil {
		return entity.Agent{}, err
	}
	if err := repository.requireAccess(ctx, tx, current, "agent.avatar.manage", target); err != nil {
		return entity.Agent{}, errs.ErrNotFound
	}
	if agent.agentVersion != expectedAgentVersion {
		return entity.Agent{}, errs.ErrVersionMismatch
	}
	receiptRef, _ := newRef("obj")
	var artifact entity.Artifact
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName,
		artifactRef, current.organizationID, projectID, "", fileName, mediaType, sizeBytes, digest,
		"CLEAN", receiptRef, "AVAILABLE", current.actorID).Scan(
		&artifact.Ref, &artifact.FileName, &artifact.MediaType, &artifact.SizeBytes, &artifact.Digest,
		&artifact.ScanState, &artifact.PreviewState, &artifact.Revision, &artifact.Version, &artifact.CreatedAt, &artifact.CurrentRevisionRef)
	if err != nil {
		return entity.Agent{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertArtifactContentArtifactId,
		artifactRef, objectKey, objectVersion, objectETag, digest, sizeBytes, scanJSON); err != nil {
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
	if err := projectAgentCards(ctx, tx, current, []*entity.Agent{&item}); err != nil {
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

// compensateAgentAvatarObject uses a five-second WithoutCancel context to commit an exact
// unadopted reservation compensation claim before any external delete. The shared delete/readback
// helper must confirm absence before the exact version/descriptor ACK; claim, storage or ACK
// failures remain visible for later durable recovery. It does not delete an artifact already
// adopted by the owner.
// compensateAgentAvatarObject использует пятисекундный WithoutCancel context и фиксирует точный
// claim компенсации непринятой reservation до внешнего удаления. Общий delete/readback helper
// подтверждает отсутствие до точного version/descriptor ACK; ошибки claim, storage и ACK
// сохраняются для durable recovery. Принятый владельцем artifact не удаляется.
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
	if err := repository.deleteArtifactObjectAndVerify(cleanupContext, task.objectKey, task.objectVersion); err != nil {
		return err
	}
	return repository.completeAgentAvatarCompensation(cleanupContext, task)
}

// CleanupExpiredAgentAvatarUploads validates a 1-to-100 batch, claims expired or abandoned
// compensation rows with adopted-content exclusion, closes rows and commits before storage
// operations. Missing descriptors are resolved with guarded Head/OCC; each task then requires
// delete plus authoritative absence before ACK. Failures accumulate while other tasks continue;
// caller context, no implicit retry and exact descriptor constraints are preserved.
// CleanupExpiredAgentAvatarUploads проверяет batch от 1 до 100, захватывает истёкшие и брошенные
// compensation rows с исключением принятого content, закрывает rows и фиксирует claim до storage.
// Отсутствующие дескрипторы разрешаются защищённым Head и OCC; каждый task требует delete и
// подтверждённого отсутствия до ACK. Ошибки накапливаются, остальные tasks продолжаются; context,
// отсутствие неявного повтора и exact descriptors сохраняются.
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
		if err := repository.deleteArtifactObjectAndVerify(ctx, task.objectKey, task.objectVersion); err != nil {
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
		if err := repository.completeAgentAvatarCompensation(ctx, task); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

// resolveExpiredAgentAvatarDescriptor reads the reserved key latest metadata when no stored
// descriptor exists and requires exact key/digest/size plus a nonempty ETag before guarded OCC
// persistence. NotFound retains the original task for absence cleanup; read errors, descriptor
// mismatch and lost owner version fail closed. It returns copied version/ETag metadata, never a
// new grant.
// resolveExpiredAgentAvatarDescriptor читает latest metadata зарезервированного ключа при
// отсутствии дескриптора, требуя точные key, digest, size и непустой ETag до защищённого OCC
// сохранения. NotFound сохраняет task для cleanup отсутствия; ошибки чтения, mismatch и потеря
// owner version вызывают отказ. Возвращает version и ETag, не новое разрешение.
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

// completeAgentAvatarCompensation acknowledges only the exact COMPENSATING reservation version and
// object descriptor with no adopted artifact content, requiring exactly one affected row. SQL
// error is Unavailable and a stale/missing row Conflict; callers must first prove object absence,
// since this update performs no external deletion.
// completeAgentAvatarCompensation подтверждает только точные версию COMPENSATING reservation и
// object descriptor без принятого artifact content, требуя одну изменённую строку. Ошибка SQL —
// Unavailable, устаревшая либо отсутствующая строка — Conflict; вызывающий заранее доказывает
// отсутствие объекта, update не удаляет storage.
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
