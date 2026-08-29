package platform

import (
	"bytes"
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
	body, err := io.ReadAll(io.LimitReader(input.Reader, maximumArtifactBytes+1))
	if err != nil || int64(len(body)) != input.SizeBytes {
		return entity.Artifact{}, errs.ErrInvalid
	}
	digest := sha256.Sum256(body)
	if input.Digest != "sha256:"+hex.EncodeToString(digest[:]) || input.MediaType == "" ||
		!contains([]string{"CLEAN", "QUARANTINED", "FAILED"}, input.ScanState) ||
		!contains([]string{"AVAILABLE", "UNAVAILABLE", "BLOCKED"}, input.PreviewState) {
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
	objectKey := artifactObjectKey(scope.organizationRef, input.ProjectRef, ref, input.Digest)
	objectReceipt, err := repository.objects.Put(ctx, objectstorage.PutInput{
		Key: objectKey, MediaType: input.MediaType, Digest: input.Digest,
		SizeBytes: input.SizeBytes, Body: bytes.NewReader(body),
	})
	if err != nil {
		return entity.Artifact{}, mapObjectStorageError(err)
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
	projectID := mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
	if projectID == "" {
		return entity.Artifact{}, errs.ErrNotFound
	}
	if err := requireProjectPermission(ctx, tx, scope, projectID, "MANAGE_ARTIFACTS"); err != nil {
		return entity.Artifact{}, err
	}
	fileName := safeFileName(input.FileName)
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, projectID,
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
	projectID := mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
	if projectID == "" {
		return nil, errs.ErrNotFound
	}
	if err := requireProjectPermission(ctx, tx, scope, projectID, "MANAGE_ARTIFACTS"); err != nil {
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

func artifactObjectKey(organizationRef, projectRef, artifactRef, digest string) string {
	return strings.Join([]string{
		"organizations", organizationRef, "projects", projectRef, "artifacts", artifactRef,
		strings.TrimPrefix(digest, "sha256:"),
	}, "/")
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
