package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) prepareCommandObjects(ctx context.Context, scope scope, input *command.Command) ([]objectstorage.Receipt, error) {
	if input == nil || input.Kind != command.CompleteExecution {
		return nil, nil
	}
	payload, ok := input.Payload.(command.CompleteExecutionInput)
	if !ok || len(payload.Artifacts) == 0 {
		return nil, nil
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.authorizeCommand(ctx, tx, scope, *input); err != nil {
		return nil, err
	}
	var storedDigest string
	var storedPayload []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).
		Scan(&storedDigest, &storedPayload)
	if err == nil {
		if storedDigest != input.Mutation.IntentDigest {
			return nil, errs.ErrIdempotencyReuse
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrUnavailable
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{
		LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation,
	}, true)
	if err != nil {
		return nil, err
	}
	var allowed bool
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectAgentCapability,
		scope.organizationID, runtimecontract.ArtifactCapability, lease["nodeID"]).Scan(&allowed); err != nil {
		return nil, errs.ErrUnavailable
	}
	if !allowed {
		return nil, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}

	prepared := make([]objectstorage.Receipt, 0, len(payload.Artifacts))
	var totalBytes int64
	for index := range payload.Artifacts {
		artifact := &payload.Artifacts[index]
		if len(payload.Artifacts) > 16 || artifact.Prepared != nil || artifact.FileName == "" ||
			safeFileName(artifact.FileName) != artifact.FileName || artifact.SizeBytes != int64(len(artifact.Content)) ||
			artifact.SizeBytes < 0 || artifact.SizeBytes > 1<<20 {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		totalBytes += artifact.SizeBytes
		if totalBytes > maximumArtifactBytes {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		digest := sha256.Sum256(artifact.Content)
		digestHex := hex.EncodeToString(digest[:])
		if !strings.EqualFold(strings.TrimSpace(artifact.SHA256), digestHex) {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		verdict := artifactpolicy.Inspect(artifact.FileName, artifact.MediaType, artifact.Content)
		if verdict.ScanState != artifactpolicy.ScanClean {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		ref, refErr := newRef("art")
		if refErr != nil {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrUnavailable
		}
		digestValue := "sha256:" + digestHex
		key := artifactObjectKey(scope.organizationRef, stringMap(lease, "projectRef"), ref, digestValue)
		receipt, putErr := repository.objects.Put(ctx, objectstorage.PutInput{
			Key: key, MediaType: verdict.MediaType, Digest: digestValue,
			SizeBytes: artifact.SizeBytes, Body: bytes.NewReader(artifact.Content),
		})
		if putErr != nil {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, mapObjectStorageError(putErr)
		}
		prepared = append(prepared, receipt)
		artifact.Prepared = &command.PreparedArtifact{
			Ref: ref, ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID,
			ObjectETag: receipt.ETag, MediaType: verdict.MediaType,
			Digest: receipt.Digest, SizeBytes: receipt.SizeBytes,
			ScanState: verdict.ScanState, PreviewState: verdict.PreviewState,
		}
		artifact.Content = nil
	}
	input.Payload = payload
	return prepared, nil
}

func (repository *Repository) cleanupPreparedObjects(ctx context.Context, prepared []objectstorage.Receipt, keep bool) {
	if keep {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, receipt := range prepared {
		_ = repository.objects.Delete(cleanupContext, receipt.Key, receipt.VersionID)
	}
}

func resultContainsPreparedObjects(result command.Result, prepared []objectstorage.Receipt) bool {
	if len(prepared) == 0 {
		return true
	}
	if len(result.CreatedRefs) < len(prepared) {
		return false
	}
	// Prepared keys include a server-assigned artifact ref. The result must expose
	// every one of those refs before cleanup is suppressed.
	for _, receipt := range prepared {
		parts := strings.Split(receipt.Key, "/")
		if len(parts) < 2 {
			return false
		}
		ref := parts[len(parts)-2]
		found := false
		for _, created := range result.CreatedRefs {
			if created == ref {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func preparedArtifact(artifact command.CompletedArtifact) (command.PreparedArtifact, error) {
	if artifact.Prepared == nil || artifact.Content != nil ||
		artifact.Prepared.Ref == "" || !objectstorage.ValidKey(artifact.Prepared.ObjectKey) ||
		!objectstorage.ValidDigest(artifact.Prepared.Digest) || artifact.Prepared.SizeBytes != artifact.SizeBytes ||
		artifact.Prepared.Digest != "sha256:"+strings.ToLower(strings.TrimSpace(artifact.SHA256)) ||
		artifact.Prepared.MediaType == "" || artifact.Prepared.ScanState != artifactpolicy.ScanClean {
		return command.PreparedArtifact{}, errs.ErrInvalid
	}
	return *artifact.Prepared, nil
}

func mapPreparedObjectError(err error) error {
	if errors.Is(err, objectstorage.ErrConflict) {
		return errs.ErrConflict
	}
	return mapObjectStorageError(err)
}
