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
	"github.com/jackc/pgx/v5"
)

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
	var storedDigest string
	var stored []byte
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation, scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if err == nil {
		if storedDigest != mutation.IntentDigest {
			return entity.Artifact{}, errs.ErrIdempotencyReuse
		}
		var item entity.Artifact
		if json.Unmarshal(stored, &item) != nil {
			return entity.Artifact{}, errs.ErrConflict
		}
		_ = tx.Commit(ctx)
		return item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return entity.Artifact{}, errs.ErrUnavailable
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
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName, ref, scope.organizationID, projectID, runID, fileName, input.MediaType, input.SizeBytes, input.Digest, input.ScanState, receiptRef, input.PreviewState, scope.actorID).Scan(&item.Ref, &item.FileName, &item.MediaType, &item.SizeBytes, &item.Digest, &item.ScanState, &item.PreviewState, &item.Revision, &item.Version, &item.CreatedAt)
	if err != nil {
		return entity.Artifact{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertArtifactContentArtifactId,
		ref, objectReceipt.Key, objectReceipt.VersionID, objectReceipt.ETag,
		objectReceipt.Digest, objectReceipt.SizeBytes); err != nil {
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
	var storedDigest string
	var stored []byte
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if err == nil {
		if storedDigest != mutation.IntentDigest {
			return nil, errs.ErrIdempotencyReuse
		}
		var item entity.Artifact
		if json.Unmarshal(stored, &item) != nil {
			return nil, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return &item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrUnavailable
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

func (repository *Repository) requireArtifactUploadAccess(ctx context.Context, tx pgx.Tx, current scope, projectRef string) error {
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

func artifactObjectKey(organizationRef, actorRef, projectRef, artifactRef, digest string) string {
	scopeKind, scopeRef := "projects", projectRef
	if projectRef == "" {
		scopeKind, scopeRef = "subjects", actorRef
	}
	return strings.Join([]string{"organizations", organizationRef, scopeKind, scopeRef, "artifacts", artifactRef,
		strings.TrimPrefix(digest, "sha256:")}, "/")
}

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

type artifactPurgeReceipt struct {
	ArtifactRef    string `json:"artifactRef"`
	ArtifactID     string `json:"artifactId"`
	ProjectID      string `json:"projectId"`
	ProjectRef     string `json:"projectRef"`
	ObjectKey      string `json:"objectKey"`
	ObjectVersion  string `json:"objectVersion"`
	LifecycleState string `json:"lifecycleState"`
}

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
	if err := repository.objects.Delete(ctx, receipt.ObjectKey, receipt.ObjectVersion); err != nil && !errors.Is(err, objectstorage.ErrNotFound) {
		return "", mapObjectStorageError(err)
	}
	if _, err := repository.objects.Head(ctx, receipt.ObjectKey, receipt.ObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		if err == nil {
			return "", errs.ErrConflict
		}
		return "", mapObjectStorageError(err)
	}
	return repository.finalizeArtifactPurge(ctx, scope, mutation, receipt)
}

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
		if json.Unmarshal(stored, &receipt) != nil || receipt.ArtifactRef != artifactRef {
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
	if err := tx.QueryRow(ctx, queryArtifactsPurgeSelectArtifactContentForUpdate, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"artifact_ref":    artifactRef,
	}).Scan(&receipt.ArtifactID, &receipt.ProjectID, &receipt.ProjectRef, &version, &receipt.LifecycleState, &receipt.ObjectKey, &receipt.ObjectVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return artifactPurgeReceipt{}, errs.ErrNotFound
		}
		return artifactPurgeReceipt{}, errs.ErrUnavailable
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

func (repository *Repository) finalizeArtifactPurge(ctx context.Context, scope scope, mutation value.Mutation, receipt artifactPurgeReceipt) (string, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey); err != nil {
		return "", errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryArtifactsPurgeDeleteBindings, pgx.StrictNamedArgs{"artifact_id": receipt.ArtifactID}); err != nil {
		return "", errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryArtifactsPurgeDeleteDownloadGrants, pgx.StrictNamedArgs{"artifact_id": receipt.ArtifactID}); err != nil {
		return "", errs.ErrUnavailable
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

func (repository *Repository) DownloadArtifact(ctx context.Context, principal value.Principal, ref, purpose string) (platformrepo.ArtifactDownload, error) {
	if purpose != "DOWNLOAD" && purpose != "PREVIEW" {
		return platformrepo.ArtifactDownload{}, errs.ErrInvalid
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var artifactID, projectID, scanState string
	var artifactVersion int64
	err = tx.QueryRow(ctx, queryArtifactsDownloadartifactSelectArtifactForGrant, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"artifact_ref":    ref,
		"platform_role":   scope.role,
		"subject_id":      scope.actorID,
	}).Scan(&artifactID, &projectID, &artifactVersion, &scanState)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ArtifactDownload{}, errs.ErrNotFound
	}
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	if scanState != "CLEAN" {
		return platformrepo.ArtifactDownload{}, errs.ErrForbidden
	}
	item, err := scanArtifact(tx.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, ref, scope.role, scope.actorID))
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	if purpose == "PREVIEW" && item.PreviewState != "AVAILABLE" {
		return platformrepo.ArtifactDownload{}, errs.ErrForbidden
	}

	grantRef, err := newRef("adg")
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	var grantID, storedGrantRef string
	err = tx.QueryRow(ctx, queryArtifactsDownloadartifactInsertDownloadGrant, pgx.StrictNamedArgs{
		"grant_ref":        grantRef,
		"organization_id":  scope.organizationID,
		"project_id":       projectID,
		"artifact_id":      artifactID,
		"artifact_version": artifactVersion,
		"subject_id":       scope.actorID,
		"purpose":          purpose,
	}).Scan(&grantID, &storedGrantRef)
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	var consumedAt time.Time
	err = tx.QueryRow(ctx, queryArtifactsDownloadartifactConsumeDownloadGrant, pgx.StrictNamedArgs{
		"grant_id":         grantID,
		"organization_id":  scope.organizationID,
		"project_id":       projectID,
		"artifact_id":      artifactID,
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
	if err := tx.QueryRow(ctx, queryArtifactsDownloadartifactSelectArtifactContent, pgx.StrictNamedArgs{
		"artifact_id":      artifactID,
		"organization_id":  scope.organizationID,
		"artifact_version": artifactVersion,
	}).Scan(&objectKey, &objectVersion, &objectETag, &objectDigest, &objectSize); errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ArtifactDownload{}, errs.ErrNotFound
	} else if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
	if objectDigest != item.Digest || objectSize != item.SizeBytes || objectKey == "" {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	object, err := repository.objects.Get(ctx, objectKey, objectVersion)
	if err != nil {
		return platformrepo.ArtifactDownload{}, mapObjectStorageError(err)
	}
	keepBody := false
	defer func() {
		if !keepBody {
			_ = object.Body.Close()
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
	auditRef, err := newRef("aud")
	if err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}
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
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrConflict
	}
	keepBody = true
	return platformrepo.ArtifactDownload{Artifact: item, Reader: object.Body, GrantRef: grantRef}, nil
}

func (repository *Repository) changeArtifactBinding(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ArtifactBindingInput)
	if !ok || payload.ArtifactRef == "" || payload.AgentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var artifactID, projectID, projectRef string
	var version int64
	if err := tx.QueryRow(ctx, queryArtifactsChangeartifactbindingSelectArtifactsOrganizationIdRefScanState, scope.organizationID, payload.ArtifactRef).Scan(&artifactID, &projectID, &projectRef, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var agentID string
	var canManageArtifacts bool
	if err := tx.QueryRow(ctx, queryArtifactsChangeartifactbindingSelectAgentsOrganizationIdProjectIdRef, scope.organizationID, projectID, payload.AgentRef, runtimecontract.ArtifactCapability).Scan(&agentID, &canManageArtifacts); err != nil {
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

func (repository *Repository) artifactImpactTx(ctx context.Context, tx pgx.Tx, current scope, artifactRef, action string) (entity.ArtifactImpact, string, error) {
	var artifactID, lifecycleState string
	var activeRunsJSON []byte
	var version, bindingCount, attachmentCount, activeRuntimeCount int64
	err := tx.QueryRow(ctx, queryArtifactsImpact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"artifact_ref":    artifactRef,
	}).Scan(&artifactID, &version, &lifecycleState, &bindingCount, &attachmentCount, &activeRuntimeCount, &activeRunsJSON)
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
	if action == "DELETE" {
		if lifecycleState != "ACTIVE" {
			blockers = append(blockers, "ARTIFACT_NOT_ACTIVE")
		}
	} else {
		if lifecycleState != "DELETED" {
			blockers = append(blockers, "ARTIFACT_NOT_DELETED")
		}
		if bindingCount > 0 {
			blockers = append(blockers, "ARTIFACT_HAS_BINDINGS")
		}
		if attachmentCount > 0 {
			blockers = append(blockers, "ARTIFACT_HAS_IMMUTABLE_ATTACHMENTS")
		}
		if activeRuntimeCount > 0 {
			blockers = append(blockers, "ACTIVE_RUN_USES_ARTIFACT")
		}
	}
	digestPayload, _ := json.Marshal(struct {
		ArtifactRef, Action, LifecycleState                                string
		ArtifactVersion, BindingCount, AttachmentCount, ActiveRuntimeCount int64
		Blockers                                                           []string
		ActiveRuns                                                         []entity.ArtifactImpactRun
		ActiveRunsTruncated                                                bool
	}{artifactRef, action, lifecycleState, version, bindingCount, attachmentCount, activeRuntimeCount, blockers, activeRuns, activeRunsTruncated})
	digestValue := sha256.Sum256(digestPayload)
	return entity.ArtifactImpact{
		ArtifactRef: artifactRef, Action: action, Digest: hex.EncodeToString(digestValue[:]),
		ArtifactVersion: version, BindingCount: bindingCount, AttachmentCount: attachmentCount,
		ActiveRuntimeCount: activeRuntimeCount, ActiveRuns: activeRuns, ActiveRunsTruncated: activeRunsTruncated,
		Blockers: blockers, Permitted: len(blockers) == 0,
	}, lifecycleState, nil
}
