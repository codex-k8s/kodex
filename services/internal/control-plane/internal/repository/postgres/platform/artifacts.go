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

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
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
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
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
	var runID any
	var rootRunID, runRef string
	if input.RunRef != "" {
		var id string
		if err := tx.QueryRow(ctx, queryArtifactsUploadartifactSelectRunsOrganizationIdProjectIdRef, scope.organizationID, projectID, input.RunRef).Scan(&id, &rootRunID, &runRef); err != nil {
			return entity.Artifact{}, errs.ErrNotFound
		}
		runID = id
	} else {
		runID = nil
	}
	digest := sha256.Sum256(body)
	scanState, previewState := scanArtifactBody(input.MediaType, body)
	ref, _ := newRef("art")
	receiptRef, _ := newRef("obj")
	var item entity.Artifact
	err = tx.QueryRow(ctx, queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName, ref, scope.organizationID, projectID, runID, safeFileName(input.FileName), input.MediaType, input.SizeBytes, "sha256:"+hex.EncodeToString(digest[:]), scanState, receiptRef, previewState, scope.actorID).Scan(&item.Ref, &item.FileName, &item.MediaType, &item.SizeBytes, &item.Digest, &item.ScanState, &item.PreviewState, &item.Version, &item.CreatedAt)
	if err != nil {
		return entity.Artifact{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertArtifactContentArtifactId, ref, body); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	item.ProjectRef = input.ProjectRef
	item.RunRef = input.RunRef
	if scanState == "CLEAN" {
		item.NextActions = []string{"DOWNLOAD", "BIND"}
	}
	auditRef, _ := newRef("aud")
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertAuditEventsRefProjectIdAction, auditRef, scope.organizationID, projectID, scope.actorID, ref, principal.CorrelationRef); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	if rootRunID != "" {
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, ref, "ARTIFACT_AVAILABLE", "", "", "", ref, "Файл доступен", "", ""); err != nil {
			return entity.Artifact{}, err
		}
	} else if err := repository.emitPlatformEvent(ctx, tx, scope, "AGENT_CHANGED", input.ProjectRef, ref, "Файл доступен"); err != nil {
		return entity.Artifact{}, err
	}
	encoded, _ := json.Marshal(item)
	if _, err := tx.Exec(ctx, queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, scope.organizationID, scope.actorID, mutation.Operation, mutation.IdempotencyKey, mutation.IntentDigest, encoded); err != nil {
		return entity.Artifact{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Artifact{}, errs.ErrConflict
	}
	_ = runRef
	return item, nil
}

func scanArtifactBody(mediaType string, body []byte) (string, string) {
	allowed := strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" || mediaType == "application/pdf" || strings.HasPrefix(mediaType, "image/")
	if !allowed {
		return "PENDING", "UNAVAILABLE"
	}
	if bytes.HasPrefix(body, []byte("MZ")) || bytes.HasPrefix(body, []byte("\x7fELF")) || bytes.Contains(bytes.ToLower(body), []byte("<script")) {
		return "REJECTED", "BLOCKED"
	}
	return "CLEAN", "AVAILABLE"
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

func (repository *Repository) DownloadArtifact(ctx context.Context, principal value.Principal, ref string) (platformrepo.ArtifactDownload, error) {
	item, err := repository.GetArtifact(ctx, principal, ref)
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	if item.ScanState != "CLEAN" {
		return platformrepo.ArtifactDownload{}, errs.ErrForbidden
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.ArtifactDownload{}, err
	}
	var body []byte
	if err := repository.pool.QueryRow(ctx, queryArtifactsDownloadartifactSelectArtifactContentOrganizationIdRef, scope.organizationID, ref).Scan(&body); err != nil {
		return platformrepo.ArtifactDownload{}, errs.ErrNotFound
	}
	return platformrepo.ArtifactDownload{Artifact: item, Reader: io.NopCloser(bytes.NewReader(body))}, nil
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
	if payload.Enabled {
		if _, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingInsertArtifactBindingsArtifactIdTargetRef, artifactID, scope.organizationID, scope.actorID, projectID, payload.AgentRef); err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
	} else {
		_, _ = tx.Exec(ctx, queryArtifactsChangeartifactbindingDeleteArtifactBindingsArtifactIdTargetKindTargetRef, artifactID, payload.AgentRef)
	}
	if _, err := tx.Exec(ctx, queryArtifactsChangeartifactbindingUpdateArtifactsVersion, artifactID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item := entity.Artifact{Ref: payload.ArtifactRef, ProjectRef: projectRef, Version: version + 1}
	return commandOutcome{result: command.Result{Artifact: &item}, projectID: projectID, projectRef: projectRef, resourceKind: "ARTIFACT", resourceRef: payload.ArtifactRef, summary: "Привязка файла обновлена", platformEvent: "AGENT_CHANGED"}, nil
}

var _ = uuid.Nil
var _ = time.Second
