package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"github.com/jackc/pgx/v5"
)

// UploadArtifact resolves scope and validates/seeks the upload, checks an authorized receipt
// before external work, scans and stores the object, then repeats receipt resolution under an
// idempotency lock in ReadCommitted. Fresh uploads retain filename/owner/run checks, persist
// artifact/content/audit/event/receipt and keep the prepared object only after commit; replay or
// failures clean that unused object. Existing scan-state inputs, ignored reference/marshal errors
// and commit mappings remain unchanged.
// UploadArtifact разрешает область, проверяет и перематывает upload, проверяет авторизованную
// receipt до внешней работы, сканирует и сохраняет объект, затем повторяет receipt resolution под
// idempotency lock в ReadCommitted. Новая загрузка сохраняет filename/owner/run checks, атомарно
// пишет artifact/content/audit/event/receipt, удерживает подготовленный объект лишь после commit;
// replay и отказы очищают неиспользованный объект. Прежние scan-state inputs, ignored
// reference/marshal errors и commit mappings не меняются.
func (repository *Repository) UploadArtifact(ctx context.Context, principal value.Principal, mutation value.Mutation, input platformrepo.ArtifactUpload) (entity.Artifact, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Artifact{}, err
	}
	if input.SizeBytes > maximumArtifactBytes {
		return entity.Artifact{}, errs.ErrInvalid
	}
	if input.Reader == nil || input.Digest == "" || input.MediaType == "" ||
		!contains([]string{"CLEAN", "QUARANTINED", "FAILED"}, input.ScanState) ||
		!contains([]string{"AVAILABLE", "UNAVAILABLE", "BLOCKED"}, input.PreviewState) {
		return entity.Artifact{}, errs.ErrInvalid
	}
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Artifact{}, errs.ErrInvalid
	}
	existing, err := repository.preflightArtifactUpload(ctx, scope, mutation, input)
	if err != nil {
		return entity.Artifact{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	ref, err := newRef("art")
	if err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	objectKey := artifactObjectKey(scope.organizationRef, scope.actorRef, input.ProjectRef, ref, input.Digest)
	scan, err := repository.scanArtifact(ctx, input.Reader, input.Digest, input.SizeBytes)
	if err != nil {
		return entity.Artifact{}, err
	}
	if scan.Infected {
		input.ScanState, input.PreviewState = "QUARANTINED", "BLOCKED"
	}
	scanJSON, err := artifactAntivirusJSON(scan)
	if err != nil {
		return entity.Artifact{}, err
	}
	objectReceipt, err := repository.putArtifactObject(ctx, objectKey, input)
	if err != nil {
		return entity.Artifact{}, err
	}
	keepObject := false
	defer func() {
		if !keepObject {
			repository.cleanupPreparedObjects(ctx, []objectstorage.Receipt{objectReceipt}, false)
		}
	}()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, scope.actorID,
		mutation.Operation, mutation.IdempotencyKey); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	if err := repository.requireArtifactUploadAccess(ctx, tx, scope, input.ProjectRef); err != nil {
		return entity.Artifact{}, err
	}
	replayed, err := readArtifactUploadReceipt(ctx, tx, scope, mutation)
	if err != nil {
		return entity.Artifact{}, err
	}
	if replayed != nil {
		if err := repository.authorizeArtifactUploadReceipt(ctx, tx, scope, input, replayed); err != nil {
			return entity.Artifact{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return entity.Artifact{}, errs.ErrConflict
		}
		return *replayed, nil
	}
	projectID := ""
	if input.ProjectRef != "" {
		projectID = mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
		if projectID == "" {
			return entity.Artifact{}, errs.ErrNotFound
		}
	} else if input.RunRef != "" {
		return entity.Artifact{}, errs.ErrInvalid
	}
	if err := repository.requireArtifactUploadAccess(ctx, tx, scope, input.ProjectRef); err != nil {
		return entity.Artifact{}, err
	}
	fileName := safeFileName(input.FileName)
	lockScopeID := projectID
	if lockScopeID == "" {
		lockScopeID = scope.actorID
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, lockScopeID,
		"artifact.upload.filename", fileName); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	var runID any
	var rootRunID, runRef, sessionRef string
	if input.RunRef != "" {
		var id string
		if err := tx.QueryRow(ctx, queryArtifactsUploadartifactSelectRunsOrganizationIdProjectIdRef, scope.organizationID, projectID, input.RunRef).Scan(&id, &rootRunID, &runRef, &sessionRef); err != nil {
			return entity.Artifact{}, errs.ErrNotFound
		}
		runID = id
	} else {
		runID = nil
	}
	receiptRef, _ := newRef("obj")
	var item entity.Artifact
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName, ref, scope.organizationID, projectID, runID, fileName, input.MediaType, input.SizeBytes, input.Digest, input.ScanState, receiptRef, input.PreviewState, scope.actorID).Scan(&item.Ref, &item.FileName, &item.MediaType, &item.SizeBytes, &item.Digest, &item.ScanState, &item.PreviewState, &item.Revision, &item.Version, &item.CreatedAt, &item.CurrentRevisionRef)
	if err != nil {
		return entity.Artifact{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertArtifactContentArtifactId,
		ref, objectReceipt.Key, objectReceipt.VersionID, objectReceipt.ETag,
		objectReceipt.Digest, objectReceipt.SizeBytes, scanJSON); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	item.ProjectRef = input.ProjectRef
	item.RunRef = input.RunRef
	item.SessionRef = sessionRef
	item.Source = "CONTROL_CENTER"
	item.LifecycleState = "ACTIVE"
	if input.ScanState == "CLEAN" {
		item.NextActions = []string{"DOWNLOAD", "BIND"}
	}
	auditRef, _ := newRef("aud")
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertAuditEventsRefProjectIdAction, auditRef, scope.organizationID, projectID, scope.actorID, ref, principal.CorrelationRef); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	if rootRunID != "" {
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, ref, "ARTIFACT_AVAILABLE", "", "", "", ref, "i18n:ARTIFACT_AVAILABLE", "", ""); err != nil {
			return entity.Artifact{}, err
		}
	} else if err := repository.emitPlatformEvent(ctx, tx, scope, "ARTIFACT_CHANGED", input.ProjectRef, ref, "i18n:ARTIFACT_AVAILABLE"); err != nil {
		return entity.Artifact{}, err
	}
	encoded, _ := json.Marshal(item)
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey, mutation.IntentDigest, encoded); err != nil {
		return entity.Artifact{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Artifact{}, errs.ErrConflict
	}
	keepObject = true
	_ = runRef
	return item, nil
}

// createProjectFile accepts only a project-bound prepared file with no inline content and bounded
// size, validates its antivirus/object metadata and prepared-content ledger, then takes the
// filename lock and inserts artifact/content rows in the caller transaction. It adopts the ledger
// into the exact new revision and returns event metadata; outer execution owns
// audit/idempotency/commit and prepared-object retention.
// createProjectFile принимает лишь проектный подготовленный файл без inline content с ограниченным
// размером, проверяет antivirus/object metadata и prepared-content ledger, берёт filename lock и
// вставляет artifact/content в транзакции вызывающего. Принимает ledger в точную новую revision и
// возвращает event metadata; внешний execution владеет audit/idempotency/commit и удержанием
// подготовленного объекта.
func (repository *Repository) createProjectFile(ctx context.Context, tx pgx.Tx, current scope, raw any) (commandOutcome, error) {
	input, ok := raw.(command.ProjectFileInput)
	if !ok || input.ProjectRef == "" || input.FileName == "" || safeFileName(input.FileName) != input.FileName ||
		input.SizeBytes < 0 || input.SizeBytes > 1<<20 || input.Content != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	prepared, err := preparedArtifact(command.CompletedArtifact{
		FileName: input.FileName, MediaType: input.MediaType, SHA256: input.SHA256,
		SizeBytes: input.SizeBytes, Prepared: input.Prepared,
	})
	if err != nil || prepared.MediaType != input.MediaType {
		return commandOutcome{}, errs.ErrInvalid
	}
	scanJSON, err := artifactAntivirusJSON(prepared.Antivirus)
	if err != nil {
		return commandOutcome{}, err
	}
	if input.PreparedLedgerID == "" || input.ContentRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, current.organizationID, input.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err := repository.requireArtifactUploadAccess(ctx, tx, current, input.ProjectRef); err != nil {
		return commandOutcome{}, err
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, projectID,
		"artifact.upload.filename", input.FileName); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	receiptRef, err := newRef("obj")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var item entity.Artifact
	if err := tx.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName,
		prepared.Ref, current.organizationID, projectID, nil, input.FileName, prepared.MediaType,
		prepared.SizeBytes, prepared.Digest, prepared.ScanState, receiptRef, prepared.PreviewState, current.actorID,
	).Scan(&item.Ref, &item.FileName, &item.MediaType, &item.SizeBytes, &item.Digest, &item.ScanState,
		&item.PreviewState, &item.Revision, &item.Version, &item.CreatedAt, &item.CurrentRevisionRef); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertArtifactContentArtifactId,
		prepared.Ref, prepared.ObjectKey, prepared.ObjectVersion, prepared.ObjectETag,
		prepared.Digest, prepared.SizeBytes, scanJSON); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.ProjectRef = input.ProjectRef
	item.Source = "CONTROL_CENTER"
	item.LifecycleState = "ACTIVE"
	item.NextActions = []string{"DOWNLOAD", "BIND"}
	var revisionID string
	if err := tx.QueryRow(ctx, queryArtifactRevisionCurrentID, current.organizationID, item.Ref, item.CurrentRevisionRef).Scan(&revisionID); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if err := repository.adoptPreparedContentTx(ctx, tx, current, input.PreparedLedgerID, revisionID); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		result: command.Result{Artifact: &item}, projectID: projectID, projectRef: input.ProjectRef,
		resourceKind: "ARTIFACT", resourceRef: item.Ref, summary: "i18n:ARTIFACT_AVAILABLE",
		platformEvent: "ARTIFACT_CHANGED",
	}, nil
}

// putArtifactObject rewinds the seekable upload, streams it to object storage and verifies the
// source reader again against declared size/digest after Put. Failed verification cleans the new
// object receipt; storage errors use the domain mapper and no database transaction is created.
// putArtifactObject перематывает seekable upload, передаёт поток object storage и после Put
// повторно сверяет исходный reader с заявленными size/digest. Отказ проверки очищает новый object
// receipt; storage errors проходят доменное отображение, транзакция БД не создаётся.
func (repository *Repository) putArtifactObject(
	ctx context.Context,
	objectKey string,
	input platformrepo.ArtifactUpload,
) (objectstorage.Receipt, error) {
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return objectstorage.Receipt{}, errs.ErrInvalid
	}
	receipt, err := repository.objects.Put(ctx, objectstorage.PutInput{
		Key: objectKey, MediaType: input.MediaType, Digest: input.Digest,
		SizeBytes: input.SizeBytes, Body: input.Reader,
	})
	if err != nil {
		return objectstorage.Receipt{}, mapObjectStorageError(err)
	}
	if err := verifyArtifactReader(input.Reader, input.SizeBytes, input.Digest); err != nil {
		repository.cleanupPreparedObjects(ctx, []objectstorage.Receipt{receipt}, false)
		return objectstorage.Receipt{}, err
	}
	return receipt, nil
}

// verifyArtifactReader rewinds, hashes at most maximumArtifactBytes plus one and attempts to
// rewind again before checking the read result. Seek/read failures are Unavailable, exact size or
// sha256-prefixed digest mismatch Conflict; this verifies the seekable source rather than reading
// the stored object.
// verifyArtifactReader перематывает reader, хеширует не более maximumArtifactBytes плюс один и
// повторно перематывает до проверки результата чтения. Seek/read failures дают Unavailable,
// несовпадение точных size либо sha256-prefixed digest — Conflict; проверяет seekable source, не
// чтение сохранённого объекта.
func verifyArtifactReader(reader platformrepo.ArtifactReader, sizeBytes int64, digest string) error {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return errs.ErrUnavailable
	}
	contentDigest := sha256.New()
	read, err := io.Copy(contentDigest, io.LimitReader(reader, maximumArtifactBytes+1))
	if _, seekErr := reader.Seek(0, io.SeekStart); seekErr != nil {
		return errs.ErrUnavailable
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if read != sizeBytes || "sha256:"+hex.EncodeToString(contentDigest.Sum(nil)) != digest {
		return errs.ErrConflict
	}
	return nil
}

// readArtifactUploadReceipt locks and reads the unexpired receipt for the exact
// organization/actor/operation/idempotency key in the caller transaction. Missing or wrapped
// NoRows is a nil miss, other scan failures are Unavailable, a changed intent is IdempotencyReuse
// before JSON decoding and malformed JSON is Conflict. A decoded artifact is only a replay
// candidate, including historical null/unknown-field JSON compatibility; callers still require
// fresh upload/file/revision eligibility and own commit.
// readArtifactUploadReceipt блокирует и читает неистёкшую receipt точных
// organization/actor/operation/idempotency key в транзакции вызывающего. Missing либо wrapped
// NoRows даёт nil miss, прочие scan failures — Unavailable, иной intent — IdempotencyReuse до
// декодирования JSON, неверный JSON — Conflict. Декодированный artifact — лишь replay candidate с
// прежней совместимостью null/unknown-field JSON; вызывающие всё ещё требуют свежие
// upload/file/revision eligibility и владеют commit.
func readArtifactUploadReceipt(ctx context.Context, tx pgx.Tx, current scope, mutation value.Mutation) (*entity.Artifact, error) {
	var storedDigest string
	var stored []byte
	err := tx.QueryRow(ctx, queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, mutation.Operation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if storedDigest != mutation.IntentDigest {
		return nil, errs.ErrIdempotencyReuse
	}
	var item entity.Artifact
	if json.Unmarshal(stored, &item) != nil {
		return nil, errs.ErrConflict
	}
	return &item, nil
}

// preflightArtifactUpload checks current upload access and a recorded receipt in ReadCommitted
// before scanning or object Put. A matching receipt still undergoes current file/revision
// eligibility and commit; a miss validates project/run scope and commits an empty preflight. It
// supplies no idempotency guarantee for the later write, which repeats these checks under its
// lock.
// preflightArtifactUpload проверяет текущий upload access и сохранённую receipt в ReadCommitted до
// сканирования и object Put. Совпавшая receipt проходит текущую file/revision eligibility и
// commit; отсутствие проверяет project/run scope и фиксирует пустой preflight. Не гарантирует
// идемпотентность поздней записи: она повторяет проверки под своим lock.
func (repository *Repository) preflightArtifactUpload(
	ctx context.Context,
	scope scope,
	mutation value.Mutation,
	input platformrepo.ArtifactUpload,
) (*entity.Artifact, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireArtifactUploadAccess(ctx, tx, scope, input.ProjectRef); err != nil {
		return nil, err
	}
	replayed, err := readArtifactUploadReceipt(ctx, tx, scope, mutation)
	if err != nil {
		return nil, err
	}
	if replayed != nil {
		if err := repository.authorizeArtifactUploadReceipt(ctx, tx, scope, input, replayed); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return replayed, nil
	}
	projectID := ""
	if input.ProjectRef != "" {
		projectID = mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
		if projectID == "" {
			return nil, errs.ErrNotFound
		}
	} else if input.RunRef != "" {
		return nil, errs.ErrInvalid
	}
	if err := repository.requireArtifactUploadAccess(ctx, tx, scope, input.ProjectRef); err != nil {
		return nil, err
	}
	if input.RunRef != "" {
		var runID, rootRunID, runRef, sessionRef string
		if err := tx.QueryRow(ctx, queryArtifactsUploadartifactSelectRunsOrganizationIdProjectIdRef,
			scope.organizationID, projectID, input.RunRef).Scan(&runID, &rootRunID, &runRef, &sessionRef); err != nil {
			return nil, errs.ErrNotFound
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return nil, nil
}

// requireArtifactUploadAccess hides organization-level upload from a project-constrained
// principal, resolves an exact project target when present and requires artifact.upload on that
// target or the organization. Access-check errors are hidden as NotFound; the helper does not
// create an artifact or grant.
// requireArtifactUploadAccess скрывает upload уровня организации от project-constrained principal,
// при наличии проекта разрешает точную цель и требует artifact.upload на ней либо организации.
// Access-check errors скрываются как NotFound; helper не создаёт artifact и grant.
func (repository *Repository) requireArtifactUploadAccess(ctx context.Context, tx pgx.Tx, current scope, projectRef string) error {
	if current.authorityProjectID != "" && projectRef == "" {
		return errs.ErrNotFound
	}
	var target any = organizationTarget(current.organizationRef)
	if projectRef != "" {
		resolved, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
			ProjectRef: projectRef, ResourceKind: "PROJECT", ResourceRef: projectRef,
		})
		if err != nil {
			return err
		}
		target = resolved
	}
	if err := repository.requireAccess(ctx, tx, current, "artifact.upload", target); err != nil {
		return errs.ErrNotFound
	}
	return nil
}

// authorizeArtifactUploadReceipt requires recorded artifact/project/run identity to match the
// upload, resolves current file context, reads the recorded revision and refreshes
// scan/eligibility projection. Historical receipt contents do not supply authority; the caller
// commits and discards partial item mutations on failure.
// authorizeArtifactUploadReceipt требует совпадения записанных artifact/project/run identity с
// upload, разрешает текущий file context, читает записанную revision, обновляет scan/eligibility
// projection. Историческое содержимое receipt не даёт полномочий; вызывающий выполняет commit и
// отбрасывает частичные изменения объекта при ошибке.
func (repository *Repository) authorizeArtifactUploadReceipt(ctx context.Context, tx pgx.Tx, current scope, input platformrepo.ArtifactUpload, item *entity.Artifact) error {
	if item.Ref == "" || item.ProjectRef != input.ProjectRef || item.RunRef != input.RunRef {
		return errs.ErrConflict
	}
	if _, err := repository.resolveAssistantContext(ctx, tx, current, entity.AssistantContextDescriptor{EntityKind: "FILE", EntityRef: item.Ref}, ""); err != nil {
		return err
	}
	revision, err := artifactRevisionRow(tx.QueryRow(ctx, queryArtifactRevisionGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_ref": item.Ref, "revision_ref": item.CurrentRevisionRef,
	}))
	if err != nil {
		return err
	}
	item.ScanState = revision.ScanState
	return projectArtifactEligibility(ctx, tx, current, item)
}

// artifactObjectKey builds an organization/project artifact path or falls back to the actor
// subject path for unassigned uploads, removing the sha256 prefix from the digest suffix. It
// formats already resolved identifiers without escaping or independently validating them.
// artifactObjectKey строит путь artifact организации/проекта либо использует actor subject path
// для upload без проекта, убирая sha256 prefix из digest suffix. Форматирует уже разрешённые
// identifiers без экранирования и самостоятельной проверки.
func artifactObjectKey(organizationRef, actorRef, projectRef, artifactRef, digest string) string {
	scopeKind, scopeRef := "projects", projectRef
	if projectRef == "" {
		scopeKind, scopeRef = "subjects", actorRef
	}
	return strings.Join([]string{"organizations", organizationRef, scopeKind, scopeRef, "artifacts", artifactRef,
		strings.TrimPrefix(digest, "sha256:")}, "/")
}

// mapObjectStorageError maps known invalid/not-found/conflict storage sentinels through errors.Is
// to domain errors and maps every other error, including nil if misused, to Unavailable. Callers
// invoke it only for failed storage operations.
// mapObjectStorageError отображает известные invalid/not-found/conflict storage sentinels через
// errors.Is в доменные ошибки, остальные ошибки, включая nil при неверном вызове, в Unavailable.
// Вызывающие применяют его лишь при отказах storage.
func mapObjectStorageError(err error) error {
	switch {
	case errors.Is(err, objectstorage.ErrInvalid):
		return errs.ErrInvalid
	case errors.Is(err, objectstorage.ErrNotFound):
		return errs.ErrNotFound
	case errors.Is(err, objectstorage.ErrConflict):
		return errs.ErrConflict
	default:
		return errs.ErrUnavailable
	}
}

// artifactPurgeReceipt records exact artifact/project identity and ordered revision/object-version
// inventory for a two-phase purge; the terminal persisted receipt is reduced to artifact reference
// and PURGED state. It is replay metadata rather than current purge authority.
// artifactPurgeReceipt фиксирует точные artifact/project identity и упорядоченный
// revision/object-version inventory двухфазного purge; terminal persisted receipt сокращается до
// artifact ref и PURGED state. Это replay metadata, не текущие полномочия purge.
type artifactPurgeReceipt struct {
	ArtifactRef    string                `json:"artifactRef"`
	ArtifactID     string                `json:"artifactId"`
	ProjectID      string                `json:"projectId"`
	ProjectRef     string                `json:"projectRef"`
	Objects        []artifactPurgeObject `json:"objects"`
	LifecycleState string                `json:"lifecycleState"`
}

// artifactPurgeObject pins one revision content object key/version in the prepared purge inventory
// so external deletion and final owner inventory comparison refer to the same stored objects.
// artifactPurgeObject связывает ключ/версию content object одной revision с подготовленным purge
// inventory для удаления и итоговой owner inventory сверки одних и тех же объектов.
type artifactPurgeObject struct {
	RevisionID    string `json:"revisionId"`
	ObjectKey     string `json:"objectKey"`
	ObjectVersion string `json:"objectVersion"`
}

// PurgeArtifact prepares an authorized impact-pinned purge receipt, returns immediately for a
// terminal replay, otherwise deletes each exact object version and requires Head to confirm
// absence before finalizing owner state. Partial external deletion leaves PURGE_PENDING for retry;
// storage errors do not falsely mark the artifact PURGED.
// PurgeArtifact готовит авторизованную impact-pinned purge receipt, сразу возвращает terminal
// replay, иначе удаляет точную версию каждого объекта и требует подтверждения отсутствия через
// Head до завершения owner state. Частичное внешнее удаление оставляет PURGE_PENDING для повтора;
// storage errors не объявляют artifact PURGED.
func (repository *Repository) PurgeArtifact(ctx context.Context, principal value.Principal, mutation value.Mutation, artifactRef, impactDigest string) (string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return "", err
	}
	receipt, err := repository.prepareArtifactPurge(ctx, scope, mutation, artifactRef, impactDigest)
	if err != nil {
		return "", err
	}
	if receipt.LifecycleState == "PURGED" {
		return receipt.LifecycleState, nil
	}
	for _, object := range receipt.Objects {
		if err := repository.deleteArtifactObjectAndVerify(ctx, object.ObjectKey, object.ObjectVersion); err != nil {
			return "", err
		}
	}
	return repository.finalizeArtifactPurge(ctx, scope, mutation, receipt)
}

// deleteArtifactObjectAndVerify deletes the caller-resolved key/version and, even after NotFound,
// requires Head on the same selector to confirm authoritative absence. Existing objects cause
// Conflict; other failures use the existing domain mapping and stop without retry. It preserves
// the supplied context and optional empty version selector, creates no transaction/claim and
// performs no owner ACK, authorization or terminal transition.
// deleteArtifactObjectAndVerify удаляет разрешённые вызывающим key и version и даже после NotFound
// требует Head того же selector для подтверждения отсутствия. Существующий объект даёт Conflict;
// остальные ошибки сохраняют доменное отображение и останавливают путь без повтора. Сохраняет
// context и допустимый пустой version selector, не создаёт transaction и claim, не выполняет owner
// ACK, авторизацию и terminal transition.
func (repository *Repository) deleteArtifactObjectAndVerify(ctx context.Context, objectKey, objectVersion string) error {
	if err := repository.objects.Delete(ctx, objectKey, objectVersion); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		return mapObjectStorageError(err)
	}
	if _, err := repository.objects.Head(ctx, objectKey, objectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		if err == nil {
			return errs.ErrConflict
		}
		return mapObjectStorageError(err)
	}
	return nil
}

// prepareArtifactPurge locks actor idempotency and resolves fresh artifact.purge authority in
// ReadCommitted before receipt replay. A fresh purge locks exact content inventory, validates
// expected version, DELETED state and matching permitted impact, then atomically marks
// PURGE_PENDING and stores the replay inventory. Commit failures are Conflict and no external
// objects are deleted here.
// prepareArtifactPurge блокирует actor idempotency и разрешает свежее artifact.purge authority в
// ReadCommitted до receipt replay. Новый purge блокирует точный content inventory, проверяет
// expected version, DELETED и совпавший permitted impact, атомарно назначает PURGE_PENDING и
// сохраняет replay inventory. Отказ commit даёт Conflict; внешние объекты здесь не удаляются.
func (repository *Repository) prepareArtifactPurge(ctx context.Context, scope scope, mutation value.Mutation, artifactRef, impactDigest string) (artifactPurgeReceipt, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return artifactPurgeReceipt{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey); err != nil {
		return artifactPurgeReceipt{}, errs.ErrUnavailable
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, scope, "artifact.purge", "ARTIFACT", artifactRef, "")
	if err != nil {
		return artifactPurgeReceipt{}, err
	}
	if err := repository.requireAccess(ctx, tx, scope, "artifact.purge", target); err != nil {
		return artifactPurgeReceipt{}, errs.ErrNotFound
	}
	var storedDigest string
	var stored []byte
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if err == nil {
		if storedDigest != mutation.IntentDigest {
			return artifactPurgeReceipt{}, errs.ErrIdempotencyReuse
		}
		var receipt artifactPurgeReceipt
		if json.Unmarshal(stored, &receipt) != nil || receipt.ArtifactRef != artifactRef ||
			(receipt.LifecycleState != "PURGED" && len(receipt.Objects) == 0) {
			return artifactPurgeReceipt{}, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return artifactPurgeReceipt{}, errs.ErrConflict
		}
		return receipt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return artifactPurgeReceipt{}, errs.ErrUnavailable
	}
	var receipt artifactPurgeReceipt
	var version int64
	var objects []byte
	if err := tx.QueryRow(ctx, queryArtifactsPurgeSelectArtifactContentForUpdate, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"artifact_ref":    artifactRef,
	}).Scan(&receipt.ArtifactID, &receipt.ProjectID, &receipt.ProjectRef, &version, &receipt.LifecycleState, &objects); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return artifactPurgeReceipt{}, errs.ErrNotFound
		}
		return artifactPurgeReceipt{}, errs.ErrUnavailable
	}
	if json.Unmarshal(objects, &receipt.Objects) != nil || len(receipt.Objects) == 0 {
		return artifactPurgeReceipt{}, errs.ErrConflict
	}
	receipt.ArtifactRef = artifactRef
	if mutation.ExpectedVersion == nil || version != *mutation.ExpectedVersion {
		return artifactPurgeReceipt{}, errs.ErrVersionMismatch
	}
	if receipt.LifecycleState != "DELETED" {
		return artifactPurgeReceipt{}, errs.ErrConflict
	}
	impact, impactState, err := repository.artifactImpactTx(ctx, tx, scope, artifactRef, "PURGE")
	if err != nil {
		return artifactPurgeReceipt{}, err
	}
	if impactState != receipt.LifecycleState || impact.Digest != impactDigest || !impact.Permitted {
		return artifactPurgeReceipt{}, errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, queryArtifactsPurgeMarkPending, pgx.StrictNamedArgs{
		"artifact_id":      receipt.ArtifactID,
		"expected_version": version,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return artifactPurgeReceipt{}, errs.ErrConflict
	}
	receipt.LifecycleState = "PURGE_PENDING"
	encoded, _ := json.Marshal(receipt)
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest,
		scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey, mutation.IntentDigest, encoded); err != nil {
		return artifactPurgeReceipt{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return artifactPurgeReceipt{}, errs.ErrConflict
	}
	return receipt, nil
}

// finalizeArtifactPurge rechecks current purge access, exact artifact/project identity,
// PURGE_PENDING state and byte-equal ordered inventory after external deletion. It deletes
// content/revisions, finalizes lifecycle, replaces the idempotency receipt with a safe terminal
// payload and writes audit/invalidation in one transaction. It preserves historical read-error
// conflation and commit/error mappings; object absence is verified by the outer method.
// finalizeArtifactPurge повторно проверяет текущий purge access, точные artifact/project identity,
// PURGE_PENDING и побайтно равный упорядоченный inventory после внешнего удаления. Удаляет
// content/revisions, завершает lifecycle, заменяет idempotency receipt безопасным terminal
// payload, пишет audit/invalidation в одной транзакции. Сохраняет прежнее объединение ошибок
// чтения и commit/error mappings; отсутствие объектов проверяет внешний метод.
func (repository *Repository) finalizeArtifactPurge(ctx context.Context, scope scope, mutation value.Mutation, receipt artifactPurgeReceipt) (string, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey); err != nil {
		return "", errs.ErrUnavailable
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, scope, "artifact.purge", "ARTIFACT", receipt.ArtifactRef, "")
	if err != nil {
		return "", err
	}
	if err := repository.requireAccess(ctx, tx, scope, "artifact.purge", target); err != nil {
		return "", errs.ErrNotFound
	}
	var artifactID, projectID, projectRef, lifecycle string
	var version int64
	var inventory []byte
	if err := tx.QueryRow(ctx, queryArtifactsPurgeSelectArtifactContentForUpdate, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "artifact_ref": receipt.ArtifactRef,
	}).Scan(&artifactID, &projectID, &projectRef, &version, &lifecycle, &inventory); err != nil {
		return "", errs.ErrNotFound
	}
	var objects []artifactPurgeObject
	if json.Unmarshal(inventory, &objects) != nil || artifactID != receipt.ArtifactID || projectID != receipt.ProjectID || projectRef != receipt.ProjectRef || lifecycle != "PURGE_PENDING" {
		return "", errs.ErrConflict
	}
	actual, _ := json.Marshal(objects)
	expected, _ := json.Marshal(receipt.Objects)
	if string(actual) != string(expected) {
		return "", errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryArtifactsPurgeDeleteContent, pgx.StrictNamedArgs{"artifact_id": receipt.ArtifactID}); err != nil {
		return "", errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryArtifactsPurgeFinalize, pgx.StrictNamedArgs{"artifact_id": receipt.ArtifactID})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		var lifecycleState string
		if err := tx.QueryRow(ctx, queryArtifactsLifecycleSelectForUpdate, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"artifact_ref":    receipt.ArtifactRef,
		}).Scan(new(string), new(string), new(string), new(int64), &lifecycleState); err != nil || lifecycleState != "PURGED" {
			return "", errs.ErrConflict
		}
	}
	if _, err := tx.Exec(ctx, queryArtifactsPurgeDeleteRevisions, pgx.StrictNamedArgs{"artifact_id": receipt.ArtifactID}); err != nil {
		return "", errs.ErrConflict
	}
	receipt.LifecycleState = "PURGED"
	storedReceipt := artifactPurgeReceipt{
		ArtifactRef:    receipt.ArtifactRef,
		LifecycleState: receipt.LifecycleState,
	}
	encoded, _ := json.Marshal(storedReceipt)
	updated, err := tx.Exec(ctx, queryArtifactsPurgeUpdateIdempotencyReceipt, pgx.StrictNamedArgs{
		"organization_id":  scope.organizationID,
		"actor_id":         scope.actorID,
		"operation":        mutation.Operation,
		"idempotency_key":  mutation.IdempotencyKey,
		"intent_digest":    mutation.IntentDigest,
		"response_payload": encoded,
	})
	if err != nil || updated.RowsAffected() != 1 {
		return "", errs.ErrConflict
	}
	auditRef, _ := newRef("aud")
	if _, err := tx.Exec(ctx, queryArtifactsDownloadartifactInsertAuditEvent, pgx.StrictNamedArgs{
		"audit_ref":       auditRef,
		"organization_id": scope.organizationID,
		"project_id":      receipt.ProjectID,
		"subject_id":      scope.actorID,
		"action":          "artifact.purge",
		"artifact_ref":    receipt.ArtifactRef,
		"safe_summary":    "i18n:ARTIFACT_PURGED",
		"correlation_ref": scope.correlationRef,
	}); err != nil {
		return "", errs.ErrUnavailable
	}
	if err := repository.emitPlatformEvent(ctx, tx, scope, "ARTIFACT_CHANGED", receipt.ProjectRef, receipt.ArtifactRef, "i18n:ARTIFACT_PURGED"); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", errs.ErrConflict
	}
	return receipt.LifecycleState, nil
}

// safeFileName trims surrounding whitespace, replaces both slash forms with underscores,
// substitutes artifact for an empty result and truncates to255 runes. It is a display/storage
// filename normalizer, not a full path or control-character validator.
// safeFileName убирает крайние пробелы, заменяет оба вида slash на underscores, подставляет
// artifact для пустого результата и обрезает до255 runes. Нормализует display/storage filename, не
// является полной проверкой пути и control characters.
func safeFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	if name == "" {
		return "artifact"
	}
	runes := []rune(name)
	if len(runes) > 255 {
		return string(runes[:255])
	}
	return name
}

// DownloadArtifact delegates a current-revision download/preview to the shared purpose-checked
// retry path using an empty revision selector; no reader is returned before the owner read
// commits.
// DownloadArtifact делегирует download/preview текущей revision общему purpose-checked retry пути
// с пустым revision selector; reader не возвращается до commit owner read.
func (repository *Repository) DownloadArtifact(ctx context.Context, principal value.Principal, ref, purpose string) (platformrepo.ArtifactDownload, error) {
	return repository.downloadArtifactRevision(ctx, principal, ref, "", purpose)
}

// DownloadArtifactRevision requires a nonempty exact revision selector and delegates to the shared
// authorized content-read path; purpose validation and transaction/reader ownership remain there.
// DownloadArtifactRevision требует непустой exact revision selector и делегирует общему
// авторизованному content-read пути; purpose validation и владение транзакцией/reader остаются
// там.
func (repository *Repository) DownloadArtifactRevision(ctx context.Context, principal value.Principal, ref, revisionRef, purpose string) (platformrepo.ArtifactDownload, error) {
	if revisionRef == "" {
		return platformrepo.ArtifactDownload{}, errs.ErrInvalid
	}
	return repository.downloadArtifactRevision(ctx, principal, ref, revisionRef, purpose)
}

// downloadArtifactRevision accepts only DOWNLOAD or PREVIEW and retries the complete specialized
// content-read attempt through the existing bounded artifact retry helper. It preserves the
// original caller context for a successfully returned stream rather than retrying an already
// exposed reader.
// downloadArtifactRevision допускает лишь DOWNLOAD либо PREVIEW и повторяет полную
// специализированную content-read attempt через существующий ограниченный retry helper. Сохраняет
// исходный caller context успешного потока, не повторяет уже выданный reader.
func (repository *Repository) downloadArtifactRevision(ctx context.Context, principal value.Principal, ref, revisionRef, purpose string) (platformrepo.ArtifactDownload, error) {
	if purpose != "DOWNLOAD" && purpose != "PREVIEW" {
		return platformrepo.ArtifactDownload{}, errs.ErrInvalid
	}
	return retryArtifactContentRead(ctx, func() (platformrepo.ArtifactDownload, error) {
		return repository.downloadArtifactRevisionOnce(ctx, principal, ref, revisionRef, purpose)
	})
}

// downloadArtifactRevisionOnce uses an observed Serializable owner read to lock an eligible clean
// exact revision, project current access and create/consume a purpose/version/subject-bound
// download grant. It verifies stored content pins and object metadata, writes audit and commits
// before transferring the reader. Failed attempts close the body and finish bounded transaction
// cleanup with safe stage diagnostics; it checks object descriptor metadata without rehashing the
// streamed bytes.
// downloadArtifactRevisionOnce использует наблюдаемый Serializable owner read для блокировки
// доступной clean exact revision, проекции текущего доступа и создания/потребления
// purpose/version/subject-bound download grant. Проверяет stored content pins и object metadata,
// пишет audit, выполняет commit до передачи reader. Отказ закрывает body и завершает bounded
// transaction cleanup с безопасными stage diagnostics; проверяет object descriptor metadata без
// повторного хеширования байтов потока.
func (repository *Repository) downloadArtifactRevisionOnce(ctx context.Context, principal value.Principal, ref, revisionRef, purpose string) (_ platformrepo.ArtifactDownload, resultError error) {
	scopeDiagnostic := artifactread.NewDiagnostic(artifactread.StageScope, artifactread.ClassDomain, "")
	scope, err := repository.resolveScopeObserved(ctx, principal, func(cause error) {
		scopeDiagnostic = artifactReadDiagnostic(artifactread.StageScope, cause)
	})
	if err != nil {
		return platformrepo.ArtifactDownload{}, artifactread.WithFailure(err, scopeDiagnostic)
	}
	rawTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.ArtifactDownload{}, artifactread.WithFailure(errs.ErrUnavailable, artifactReadDiagnostic(artifactread.StageBegin, err))
	}
	tx := &artifactContentReadTx{Tx: rawTx}
	defer tx.finish(ctx, &resultError)

	var artifactID, projectID, scanState, revisionID, selectedRevisionRef string
	var artifactVersion int64
	tx.stage = artifactread.StageSelect
	err = tx.QueryRow(ctx, queryArtifactsDownloadartifactSelectArtifactForGrant, pgx.StrictNamedArgs{
		"organization_id":   scope.organizationID,
		"artifact_ref":      ref,
		"revision_ref":      revisionRef,
		"authority_project": scope.authorityProjectID,
		"subject_id":        scope.actorID,
	}).Scan(&artifactID, &projectID, &artifactVersion, &scanState, &revisionID, &selectedRevisionRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ArtifactDownload{}, errs.ErrNotFound
	}
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	if scanState != "CLEAN" {
		return platformrepo.ArtifactDownload{}, errs.ErrForbidden
	}
	tx.stage = artifactread.StageMetadata
	item, err := scanArtifact(tx.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, ref, scope.role, scope.actorID))
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	tx.stage = artifactread.StageEligibility
	if err := projectArtifactEligibility(ctx, tx, scope, &item); err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	tx.stage = artifactread.StageRevision
	selected, err := artifactRevisionRow(tx.QueryRow(ctx, queryArtifactRevisionGet, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "artifact_ref": ref, "revision_ref": selectedRevisionRef}))
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	item.CurrentRevisionRef = selected.Ref
	item.FileName, item.MediaType, item.Digest = selected.FileName, selected.MediaType, selected.Digest
	item.SizeBytes, item.Revision, item.ScanState, item.Source = selected.SizeBytes, selected.Revision, selected.ScanState, selected.Source
	item.PreviewState = "UNAVAILABLE"
	if selected.PreviewAvailable {
		item.PreviewState = "AVAILABLE"
	}
	if purpose == "PREVIEW" && item.PreviewState != "AVAILABLE" {
		return platformrepo.ArtifactDownload{}, errs.ErrForbidden
	}

	tx.stage = artifactread.StageGrantRef
	grantRef, err := newRef("adg")
	if err != nil {
		tx.observeFailure(err)
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	var grantID, storedGrantRef string
	tx.stage = artifactread.StageGrantCreate
	err = tx.QueryRow(ctx, queryArtifactsDownloadartifactInsertDownloadGrant, pgx.StrictNamedArgs{
		"grant_ref":        grantRef,
		"organization_id":  scope.organizationID,
		"project_id":       projectID,
		"artifact_id":      artifactID,
		"revision_id":      revisionID,
		"artifact_version": artifactVersion,
		"subject_id":       scope.actorID,
		"purpose":          purpose,
	}).Scan(&grantID, &storedGrantRef)
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	var consumedAt time.Time
	tx.stage = artifactread.StageGrantConsume
	err = tx.QueryRow(ctx, queryArtifactsDownloadartifactConsumeDownloadGrant, pgx.StrictNamedArgs{
		"grant_id":         grantID,
		"organization_id":  scope.organizationID,
		"project_id":       projectID,
		"artifact_id":      artifactID,
		"revision_id":      revisionID,
		"artifact_version": artifactVersion,
		"subject_id":       scope.actorID,
		"purpose":          purpose,
	}).Scan(&consumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	var objectKey, objectVersion, objectETag, objectDigest string
	var objectSize int64
	tx.stage = artifactread.StageContent
	if err := tx.QueryRow(ctx, queryArtifactsDownloadartifactSelectArtifactContent, pgx.StrictNamedArgs{
		"artifact_id":     artifactID,
		"revision_id":     revisionID,
		"organization_id": scope.organizationID,
	}).Scan(&objectKey, &objectVersion, &objectETag, &objectDigest, &objectSize); errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ArtifactDownload{}, errs.ErrNotFound
	} else if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	if objectDigest != item.Digest || objectSize != item.SizeBytes || objectKey == "" {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	tx.stage = artifactread.StageStorage
	object, err := repository.objects.Get(ctx, objectKey, objectVersion)
	if err != nil {
		tx.observeFailure(err)
		return platformrepo.ArtifactDownload{}, mapObjectStorageError(err)
	}
	keepBody := false
	defer func() {
		if !keepBody {
			tx.bodyCleanupFailed = object.Body.Close() != nil
		}
	}()
	if object.Digest != objectDigest || object.SizeBytes != objectSize ||
		(objectVersion != "" && object.VersionID != objectVersion) ||
		(objectETag != "" && object.ETag != objectETag) {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	action, safeSummary := "artifact.download", "i18n:ARTIFACT_DOWNLOADED"
	if purpose == "PREVIEW" {
		action, safeSummary = "artifact.preview", "i18n:ARTIFACT_PREVIEWED"
	}
	tx.stage = artifactread.StageAuditRef
	auditRef, err := newRef("aud")
	if err != nil {
		tx.observeFailure(err)
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	tx.stage = artifactread.StageAudit
	if _, err := tx.Exec(ctx, queryArtifactsDownloadartifactInsertAuditEvent, pgx.StrictNamedArgs{
		"audit_ref":       auditRef,
		"organization_id": scope.organizationID,
		"project_id":      projectID,
		"subject_id":      scope.actorID,
		"action":          action,
		"artifact_ref":    ref,
		"safe_summary":    safeSummary,
		"correlation_ref": scope.correlationRef,
	}); err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	if consumedAt.IsZero() || storedGrantRef != grantRef {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	tx.stage = artifactread.StageCommit
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	keepBody = true
	return platformrepo.ArtifactDownload{Artifact: item, Reader: object.Body, GrantRef: grantRef}, nil
}

// changeArtifactBinding requires a typed artifact/agent payload and expected version, reads the
// exact artifact and eligible same-project agent, and requires artifact capability only when
// enabling. It inserts/deletes the binding, advances artifact/agent versions only when changed and
// returns current artifact readback/event metadata in the caller transaction. Authorization and
// commit stay in outer execution.
// changeArtifactBinding требует typed artifact/agent payload и expected version, читает точный
// artifact и допустимого агента того же проекта, требует artifact capability лишь при включении.
// Вставляет/удаляет binding, увеличивает artifact/agent versions лишь при изменении, возвращает
// текущий artifact readback/event metadata в транзакции вызывающего. Authorization и commit
// остаются во внешнем execution.
func (repository *Repository) changeArtifactBinding(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ArtifactBindingInput)
	if !ok || payload.ArtifactRef == "" || payload.AgentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var artifactID, projectID, projectRef string
	var version int64
	if err := tx.QueryRow(ctx, queryArtifactsChangeartifactbindingSelectArtifactsOrganizationIdRefScanState, scope.organizationID, payload.ArtifactRef, payload.Enabled).Scan(&artifactID, &projectID, &projectRef, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var agentID string
	var canManageArtifacts bool
	if err := tx.QueryRow(ctx, queryArtifactsChangeartifactbindingSelectAgentsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "project_id": projectID, "agent_ref": payload.AgentRef,
		"capability": runtimecontract.ArtifactCapability, "artifact_id": artifactID, "enabled": payload.Enabled,
	}).Scan(&agentID, &canManageArtifacts); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if payload.Enabled && !canManageArtifacts {
		return commandOutcome{}, errs.ErrConflict
	}
	changed := false
	if payload.Enabled {
		tag, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingInsertArtifactBindingsArtifactIdTargetRef, artifactID, scope.organizationID, scope.actorID, projectID, payload.AgentRef)
		if err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		changed = tag.RowsAffected() == 1
	} else {
		tag, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingDeleteArtifactBindingsArtifactIdTargetKindTargetRef, artifactID, payload.AgentRef)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		changed = tag.RowsAffected() == 1
	}
	if changed {
		if _, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingUpdateArtifactsVersion, artifactID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingUpdateAgentsVersion, agentID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	item, err := scanArtifact(tx.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, payload.ArtifactRef, scope.role, scope.actorID))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Artifact: &item}, projectID: projectID, projectRef: projectRef, resourceKind: "ARTIFACT", resourceRef: payload.ArtifactRef, summary: "i18n:ARTIFACT_BINDING_UPDATED", platformEvent: "ARTIFACT_CHANGED"}, nil
}

// changeArtifactLifecycle locks the exact artifact, checks expected version and ACTIVE→DELETED or
// DELETED→ACTIVE state, requiring fresh matching permitted impact only for deletion. It performs a
// one-row versioned transition and returns artifact/event metadata without deleting storage or
// committing the caller transaction.
// changeArtifactLifecycle блокирует точный artifact, проверяет expected version и ACTIVE→DELETED
// либо DELETED→ACTIVE, требует свежий совпавший permitted impact лишь при удалении. Выполняет
// версионный переход одной строки, возвращает artifact/event metadata без удаления storage и
// commit транзакции вызывающего.
func (repository *Repository) changeArtifactLifecycle(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ArtifactLifecycleInput)
	if !ok || strings.TrimSpace(payload.ArtifactRef) == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var artifactID, projectID, projectRef, lifecycleState string
	var version int64
	if err := tx.QueryRow(ctx, queryArtifactsLifecycleSelectForUpdate, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"artifact_ref":    payload.ArtifactRef,
	}).Scan(&artifactID, &projectID, &projectRef, &version, &lifecycleState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		return commandOutcome{}, errs.ErrUnavailable
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	queryText := queryArtifactsLifecycleSoftDelete
	expectedState := "ACTIVE"
	summary := "i18n:ARTIFACT_DELETED"
	if input.Kind == command.RestoreArtifact {
		queryText = queryArtifactsLifecycleRestore
		expectedState = "DELETED"
		summary = "i18n:ARTIFACT_RESTORED"
	}
	if lifecycleState != expectedState {
		return commandOutcome{}, errs.ErrConflict
	}
	if input.Kind == command.DeleteArtifact {
		impact, impactState, err := repository.artifactImpactTx(ctx, tx, scope, payload.ArtifactRef, "DELETE")
		if err != nil {
			return commandOutcome{}, err
		}
		if impactState != lifecycleState || impact.Digest != payload.ImpactDigest || !impact.Permitted {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	tag, err := tx.Exec(ctx, queryText, pgx.StrictNamedArgs{
		"artifact_id":      artifactID,
		"expected_version": version,
	})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	item, err := scanArtifact(tx.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, payload.ArtifactRef, scope.role, scope.actorID))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		result:        command.Result{Artifact: &item},
		projectID:     projectID,
		projectRef:    projectRef,
		resourceKind:  "ARTIFACT",
		resourceRef:   payload.ArtifactRef,
		summary:       summary,
		platformEvent: "ARTIFACT_CHANGED",
	}, nil
}

// GetArtifactImpact accepts DELETE/PURGE, resolves the corresponding exact command target and
// access in ReadCommitted, then computes impact and commits before returning. Access denials are
// hidden as NotFound; the response is a proposal pin rather than permission to execute later
// without rechecking.
// GetArtifactImpact принимает DELETE/PURGE, разрешает соответствующую точную command target и
// доступ в ReadCommitted, вычисляет impact и выполняет commit до ответа. Отказ доступа скрывает
// как NotFound; ответ служит proposal pin, не разрешением позднего исполнения без повторной
// проверки.
func (repository *Repository) GetArtifactImpact(ctx context.Context, principal value.Principal, artifactRef, action string) (entity.ArtifactImpact, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ArtifactImpact{}, err
	}
	if action != "DELETE" && action != "PURGE" {
		return entity.ArtifactImpact{}, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return entity.ArtifactImpact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	permission := "artifact.delete"
	if action == "PURGE" {
		permission = "artifact.purge"
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, permission, "ARTIFACT", artifactRef, "")
	if err != nil {
		return entity.ArtifactImpact{}, err
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return entity.ArtifactImpact{}, errs.ErrNotFound
	}
	impact, _, err := repository.artifactImpactTx(ctx, tx, current, artifactRef, action)
	if err != nil {
		return entity.ArtifactImpact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ArtifactImpact{}, errs.ErrConflict
	}
	return impact, nil
}

// artifactImpactTx reads version/lifecycle and binding/attachment/runtime/skill counts, validates
// a21-row runtime lookahead and returns at most20 active run summaries with truncation metadata.
// Skill usage always blocks; DELETE requires ACTIVE while PURGE requires DELETED with no active
// bindings/runtime. The digest pins the serialized impact view; callers own action validation,
// authorization and transaction consistency.
// artifactImpactTx читает version/lifecycle и binding/attachment/runtime/skill counts, проверяет
// runtime lookahead из21 строки и возвращает не более20 active run summaries с truncation
// metadata. Skill usage всегда блокирует; DELETE требует ACTIVE, PURGE — DELETED без active
// bindings/runtime. Digest фиксирует сериализованный impact view; action validation, authorization
// и согласованность транзакции принадлежат вызывающим.
func (repository *Repository) artifactImpactTx(ctx context.Context, tx pgx.Tx, current scope, artifactRef, action string) (entity.ArtifactImpact, string, error) {
	var artifactID, lifecycleState string
	var activeRunsJSON []byte
	var version, bindingCount, attachmentCount, activeRuntimeCount, skillRevisionCount, activeBindingCount int64
	err := tx.QueryRow(ctx, queryArtifactsImpact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"artifact_ref":    artifactRef,
	}).Scan(&artifactID, &version, &lifecycleState, &bindingCount, &attachmentCount, &activeRuntimeCount, &activeRunsJSON, &skillRevisionCount, &activeBindingCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ArtifactImpact{}, "", errs.ErrNotFound
	}
	if err != nil {
		return entity.ArtifactImpact{}, "", errs.ErrUnavailable
	}
	activeRuns := make([]entity.ArtifactImpactRun, 0, 21)
	if json.Unmarshal(activeRunsJSON, &activeRuns) != nil ||
		len(activeRuns) > 21 || activeRuntimeCount < int64(len(activeRuns)) ||
		activeRuntimeCount <= 20 && activeRuntimeCount != int64(len(activeRuns)) ||
		activeRuntimeCount > 20 && len(activeRuns) != 21 {
		return entity.ArtifactImpact{}, "", errs.ErrUnavailable
	}
	activeRunsTruncated := len(activeRuns) > 20
	if activeRunsTruncated {
		activeRuns = activeRuns[:20]
	}
	blockers := make([]string, 0, 3)
	if skillRevisionCount > 0 {
		blockers = append(blockers, "ARTIFACT_USED_BY_SKILL")
	}
	if action == "DELETE" {
		if lifecycleState != "ACTIVE" {
			blockers = append(blockers, "ARTIFACT_NOT_ACTIVE")
		}
	} else {
		if lifecycleState != "DELETED" {
			blockers = append(blockers, "ARTIFACT_NOT_DELETED")
		}
		if activeBindingCount > 0 {
			blockers = append(blockers, "ARTIFACT_HAS_BINDINGS")
		}
		if activeRuntimeCount > 0 {
			blockers = append(blockers, "ACTIVE_RUN_USES_ARTIFACT")
		}
	}
	digestPayload, _ := json.Marshal(struct {
		ArtifactRef, Action, LifecycleState                                                                        string
		ArtifactVersion, BindingCount, AttachmentCount, ActiveRuntimeCount, SkillRevisionCount, ActiveBindingCount int64
		Blockers                                                                                                   []string
		ActiveRuns                                                                                                 []entity.ArtifactImpactRun
		ActiveRunsTruncated                                                                                        bool
	}{artifactRef, action, lifecycleState, version, bindingCount, attachmentCount, activeRuntimeCount, skillRevisionCount, activeBindingCount, blockers, activeRuns, activeRunsTruncated})
	digestValue := sha256.Sum256(digestPayload)
	return entity.ArtifactImpact{
		ArtifactRef: artifactRef, Action: action, Digest: hex.EncodeToString(digestValue[:]),
		ArtifactVersion: version, BindingCount: bindingCount, AttachmentCount: attachmentCount,
		ActiveRuntimeCount: activeRuntimeCount, ActiveRuns: activeRuns, ActiveRunsTruncated: activeRunsTruncated,
		Blockers: blockers, Permitted: len(blockers) == 0,
	}, lifecycleState, nil
}
