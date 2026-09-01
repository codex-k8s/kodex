package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const (
	sessionArchiveIdleAfter     = 15 * time.Minute
	sessionArchiveLeaseDuration = 2 * time.Minute
	sessionArchiveRetention     = 30 * 24 * time.Hour
	sessionArchiveMaxAttempts   = 5
)

type sessionArchiveCandidate struct {
	taskID, taskRef, kind, inputDigest, objectKey, objectVersion string
	organizationRef, projectRef, sessionRef, providerAccountRef  string
	runtimeRevisionRef, runtimeRevisionDigest, codexSessionID    string
	sourceRelativePath, sourceSHA256                             string
	archiveRef, archiveObjectKey, archiveObjectVersion           string
	archiveObjectETag, archiveObjectDigest                       string
	archiveSourceRelativePath, archiveSourceSHA256               string
	contentGeneration, runtimeRevisionVersion, sourceSizeBytes   int64
	archiveFormatVersion                                         int32
	archiveObjectSizeBytes, archiveSourceSizeBytes               int64
	attempt                                                      int32
}

func (repository *Repository) ClaimSessionArchiveTasks(ctx context.Context, principal value.Principal, workloadInstance string, limit int32) ([]map[string]any, error) {
	if principal.CallerWorkload != "session-archive" || principal.Permission != "platform.session-archive.tasks.claim" ||
		strings.TrimSpace(workloadInstance) == "" || limit < 1 || limit > 16 {
		return nil, errs.ErrForbidden
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, querySessionArchiveMaterializeTasks, pgx.StrictNamedArgs{
		"idle_seconds": int64(sessionArchiveIdleAfter / time.Second), "retention_seconds": int64(sessionArchiveRetention / time.Second),
		"maximum_attempts": sessionArchiveMaxAttempts,
	}); err != nil {
		return nil, fmt.Errorf("materialize session archive tasks: %w", errs.ErrUnavailable)
	}
	rows, err := tx.Query(ctx, querySessionArchiveSelectClaimableTasks, pgx.StrictNamedArgs{"organization_id": scope.organizationID, "limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	candidates := make([]sessionArchiveCandidate, 0, limit)
	for rows.Next() {
		var candidate sessionArchiveCandidate
		if err := rows.Scan(
			&candidate.taskID, &candidate.taskRef, &candidate.kind, &candidate.contentGeneration,
			&candidate.inputDigest, &candidate.objectKey, &candidate.objectVersion, &candidate.attempt,
			&candidate.organizationRef, &candidate.projectRef, &candidate.sessionRef, &candidate.providerAccountRef,
			&candidate.runtimeRevisionRef, &candidate.runtimeRevisionVersion, &candidate.runtimeRevisionDigest,
			&candidate.codexSessionID, &candidate.sourceRelativePath, &candidate.sourceSHA256, &candidate.sourceSizeBytes,
			&candidate.archiveRef, &candidate.archiveFormatVersion, &candidate.archiveObjectKey,
			&candidate.archiveObjectVersion, &candidate.archiveObjectETag, &candidate.archiveObjectDigest,
			&candidate.archiveObjectSizeBytes, &candidate.archiveSourceRelativePath,
			&candidate.archiveSourceSHA256, &candidate.archiveSourceSizeBytes,
		); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, errs.ErrUnavailable
	}
	rows.Close()

	items := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		leaseRef, err := newRef("lea")
		if err != nil {
			return nil, err
		}
		fence, err := newRef("fnc")
		if err != nil {
			return nil, err
		}
		objectKey := candidate.objectKey
		if candidate.kind == "SNAPSHOT" {
			project := candidate.projectRef
			if project == "" {
				project = "_system"
			}
			objectKey = fmt.Sprintf("session-archive/v1/%s/%s/%s/g%d/%s-a%d.tar", candidate.organizationRef,
				project, candidate.sessionRef, candidate.contentGeneration, candidate.taskRef, candidate.attempt+1)
		}
		inputDigest, err := digestSessionArchiveInput(candidate, objectKey)
		if err != nil {
			return nil, errs.ErrConflict
		}
		fenceDigest := sha256.Sum256([]byte(fence))
		expiresAt := time.Now().UTC().Add(sessionArchiveLeaseDuration)
		var attempt int32
		var generation int64
		if err := tx.QueryRow(ctx, querySessionArchiveClaimTask, pgx.StrictNamedArgs{
			"task_id": candidate.taskID, "object_key": objectKey, "input_digest": inputDigest,
			"workload_instance": workloadInstance, "lease_ref": leaseRef,
			"fence_digest": hex.EncodeToString(fenceDigest[:]), "lease_expires_at": expiresAt,
		}).Scan(&attempt, &generation); err != nil {
			return nil, errs.ErrConflict
		}
		pvcName, err := runtimecontract.SessionPVCName(candidate.sessionRef)
		if err != nil {
			return nil, errs.ErrConflict
		}
		archive := map[string]any{}
		if candidate.archiveRef != "" {
			archive = map[string]any{"archiveRef": candidate.archiveRef, "formatVersion": candidate.archiveFormatVersion,
				"objectKey": candidate.archiveObjectKey, "objectVersion": candidate.archiveObjectVersion,
				"objectETag": candidate.archiveObjectETag, "objectDigest": candidate.archiveObjectDigest,
				"objectSizeBytes": candidate.archiveObjectSizeBytes, "sourceRelativePath": candidate.archiveSourceRelativePath,
				"sourceSHA256": candidate.archiveSourceSHA256, "sourceSizeBytes": candidate.archiveSourceSizeBytes}
		}
		items = append(items, map[string]any{
			"taskRef": candidate.taskRef, "kind": candidate.kind, "organizationRef": candidate.organizationRef,
			"projectRef": candidate.projectRef, "sessionRef": candidate.sessionRef,
			"providerAccountRef": candidate.providerAccountRef, "runtimeRevisionRef": candidate.runtimeRevisionRef,
			"runtimeRevisionVersion": candidate.runtimeRevisionVersion, "runtimeRevisionDigest": candidate.runtimeRevisionDigest,
			"codexSessionID": candidate.codexSessionID, "contentGeneration": candidate.contentGeneration,
			"sourceRelativePath": candidate.sourceRelativePath, "sourceSHA256": candidate.sourceSHA256,
			"sourceSizeBytes": candidate.sourceSizeBytes, "objectKey": objectKey, "objectVersion": candidate.objectVersion,
			"pvcName": pvcName, "archive": archive, "inputDigest": inputDigest, "attempt": attempt,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return items, nil
}

func digestSessionArchiveInput(candidate sessionArchiveCandidate, objectKey string) (string, error) {
	raw, err := json.Marshal([]any{candidate.taskRef, candidate.kind, candidate.organizationRef,
		candidate.projectRef, candidate.sessionRef, candidate.providerAccountRef,
		candidate.runtimeRevisionRef, candidate.runtimeRevisionVersion, candidate.runtimeRevisionDigest,
		candidate.codexSessionID, candidate.contentGeneration, candidate.sourceRelativePath,
		candidate.sourceSHA256, candidate.sourceSizeBytes, objectKey, candidate.objectVersion,
		candidate.archiveRef, candidate.archiveObjectDigest, candidate.archiveObjectSizeBytes})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (repository *Repository) RenewSessionArchiveTask(ctx context.Context, principal value.Principal, input command.SessionArchiveTaskInput) (map[string]any, error) {
	if principal.CallerWorkload != "session-archive" || principal.Permission != "platform.session-archive.tasks.renew" ||
		input.TaskRef == "" || input.LeaseRef == "" || input.Fence == "" || input.Generation < 1 {
		return nil, errs.ErrForbidden
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(input.Fence))
	expiresAt := time.Now().UTC().Add(sessionArchiveLeaseDuration)
	if err := repository.pool.QueryRow(ctx, querySessionArchiveRenewTask, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "task_ref": input.TaskRef, "lease_ref": input.LeaseRef,
		"fence_digest": hex.EncodeToString(digest[:]), "generation": input.Generation,
		"lease_expires_at": expiresAt,
	}).Scan(&expiresAt); errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrForbidden
	} else if err != nil {
		return nil, errs.ErrUnavailable
	}
	return map[string]any{"leaseRef": input.LeaseRef, "fence": input.Fence, "generation": input.Generation, "expiresAt": expiresAt}, nil
}
