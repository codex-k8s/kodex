package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

type lockedSessionArchiveTask struct {
	id, ref, kind, state, fenceDigest, leaseRef         string
	organizationID, projectID, sessionID, archiveID     string
	inputDigest, objectKey, objectVersion, storageState string
	sourceRelativePath, sourceSHA256, currentArchiveID  string
	sessionRef                                          string
	generation, contentGeneration, storageGeneration    int64
	sourceSizeBytes                                     int64
	attempt, maximumAttempts                            int32
	leaseExpiresAt                                      time.Time
	activeTurn                                          bool
}

func (repository *Repository) changeSessionArchive(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SessionArchiveTaskInput)
	if !ok || input.Principal.CallerWorkload != "session-archive" ||
		input.Principal.Permission != sessionArchivePermission(input.Kind) {
		return commandOutcome{}, errs.ErrForbidden
	}
	locked, err := repository.lockSessionArchiveTask(ctx, tx, scope, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	var archiveRef, state string
	retryScheduled := false
	switch input.Kind {
	case command.CompleteSessionSnapshot:
		if locked.kind != "SNAPSHOT" || payload.FormatVersion != 1 || payload.ObjectKey != locked.objectKey ||
			len(payload.ObjectVersion) > 1024 || payload.ObjectETag == "" ||
			!validSessionArchiveDigest(payload.ObjectDigest) || payload.ObjectSizeBytes < 1 || payload.ObjectSizeBytes > 68<<20 ||
			payload.SourceSizeBytes != locked.sourceSizeBytes {
			return commandOutcome{}, errs.ErrInvalid
		}
		archiveRef, err = newRef("sar")
		if err != nil {
			return commandOutcome{}, err
		}
		err = tx.QueryRow(ctx, querySessionArchiveCompleteSnapshot, pgx.StrictNamedArgs{
			"archive_ref": archiveRef, "organization_id": locked.organizationID, "session_id": locked.sessionID,
			"task_id": locked.id, "content_generation": locked.contentGeneration, "format_version": payload.FormatVersion,
			"object_key": payload.ObjectKey, "object_version": payload.ObjectVersion, "object_etag": payload.ObjectETag,
			"object_digest": payload.ObjectDigest, "object_size_bytes": payload.ObjectSizeBytes,
			"active_turn": locked.activeTurn, "retention_seconds": int64(sessionArchiveRetention / time.Second),
			"maximum_attempts": sessionArchiveMaxAttempts,
		}).Scan(&archiveRef)
		state = "SUCCEEDED"
	case command.CompleteSessionRestore:
		if locked.kind != "RESTORE" || payload.FormatVersion != 1 || payload.ObjectKey == "" ||
			len(payload.ObjectVersion) > 1024 || payload.ObjectETag == "" ||
			!validSessionArchiveDigest(payload.ObjectDigest) || payload.ObjectSizeBytes < 1 ||
			payload.RestoredSourceSHA256 != locked.sourceSHA256 || payload.SourceSizeBytes != locked.sourceSizeBytes {
			return commandOutcome{}, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, querySessionArchiveCompleteRestore, pgx.StrictNamedArgs{
			"task_id": locked.id, "format_version": payload.FormatVersion, "object_key": payload.ObjectKey,
			"object_version": payload.ObjectVersion, "object_etag": payload.ObjectETag,
			"object_digest": payload.ObjectDigest, "object_size_bytes": payload.ObjectSizeBytes,
			"source_sha256": payload.RestoredSourceSHA256, "source_size_bytes": payload.SourceSizeBytes,
			"retention_seconds": int64(sessionArchiveRetention / time.Second),
		}).Scan(&archiveRef)
		state = "SUCCEEDED"
	case command.CompleteSessionPVCDeletion:
		expectedPVC, nameErr := runtimecontract.SessionPVCName(locked.sessionRef)
		if nameErr != nil || locked.kind != "DELETE_PVC" || payload.PVCName != expectedPVC {
			return commandOutcome{}, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, querySessionArchiveCompletePVCDeletion, pgx.StrictNamedArgs{
			"task_id": locked.id, "active_turn": locked.activeTurn, "maximum_attempts": sessionArchiveMaxAttempts,
		}).Scan(&archiveRef)
		state = "SUCCEEDED"
	case command.CompleteSessionObjectDeletion:
		if locked.kind != "DELETE_OBJECT" || payload.ObjectKey != locked.objectKey || payload.ObjectVersion != locked.objectVersion {
			return commandOutcome{}, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, querySessionArchiveCompleteObjectDeletion, pgx.StrictNamedArgs{
			"task_id": locked.id, "object_key": payload.ObjectKey, "object_version": payload.ObjectVersion,
		}).Scan(&archiveRef)
		state = "SUCCEEDED"
	case command.FailSessionArchiveTask:
		if !validSessionArchiveError(payload.SafeErrorCode) {
			return commandOutcome{}, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, querySessionArchiveFailTask, pgx.StrictNamedArgs{
			"task_id": locked.id, "safe_error_code": payload.SafeErrorCode,
			"maximum_attempts": sessionArchiveMaxAttempts,
		}).Scan(&archiveRef, &state, &retryScheduled)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{
		result:    command.Result{Runtime: map[string]any{"taskRef": locked.ref, "state": state, "archiveRef": archiveRef, "retryScheduled": retryScheduled}},
		projectID: locked.projectID, resourceKind: "SESSION_ARCHIVE_TASK", resourceRef: locked.ref,
		summary: sessionArchiveTaskSummary(state),
	}, nil
}

func sessionArchiveTaskSummary(state string) string {
	switch state {
	case "SUCCEEDED":
		return "i18n:SESSION_ARCHIVE_TASK_SUCCEEDED"
	case "READY":
		return "i18n:SESSION_ARCHIVE_TASK_READY"
	case "DEAD_LETTER":
		return "i18n:SESSION_ARCHIVE_TASK_DEAD_LETTER"
	default:
		return "i18n:SESSION_ARCHIVE_TASK_DEAD_LETTER"
	}
}

func (repository *Repository) lockSessionArchiveTask(ctx context.Context, tx pgx.Tx, scope scope, payload command.SessionArchiveTaskInput) (lockedSessionArchiveTask, error) {
	if payload.TaskRef == "" || payload.LeaseRef == "" || payload.Fence == "" || payload.Generation < 1 {
		return lockedSessionArchiveTask{}, errs.ErrInvalid
	}
	var task lockedSessionArchiveTask
	err := tx.QueryRow(ctx, querySessionArchiveLockTask, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "task_ref": payload.TaskRef,
	}).Scan(&task.id, &task.ref, &task.kind, &task.state, &task.generation, &task.fenceDigest,
		&task.leaseRef, &task.leaseExpiresAt, &task.attempt, &task.maximumAttempts,
		&task.organizationID, &task.projectID, &task.sessionID, &task.archiveID,
		&task.contentGeneration, &task.inputDigest, &task.objectKey, &task.objectVersion,
		&task.storageState, &task.storageGeneration, &task.sourceRelativePath,
		&task.sourceSHA256, &task.sourceSizeBytes, &task.currentArchiveID, &task.sessionRef,
		&task.activeTurn)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedSessionArchiveTask{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedSessionArchiveTask{}, errs.ErrUnavailable
	}
	fenceDigest := sha256.Sum256([]byte(payload.Fence))
	if task.state != "CLAIMED" || task.leaseRef != payload.LeaseRef || task.generation != payload.Generation ||
		task.fenceDigest != hex.EncodeToString(fenceDigest[:]) || !task.leaseExpiresAt.After(time.Now()) ||
		(task.kind != "DELETE_OBJECT" && task.contentGeneration != task.storageGeneration) {
		return lockedSessionArchiveTask{}, errs.ErrForbidden
	}
	return task, nil
}

func sessionArchivePermission(kind command.Kind) string {
	switch kind {
	case command.CompleteSessionSnapshot:
		return "platform.session-archive.snapshot.complete"
	case command.CompleteSessionRestore:
		return "platform.session-archive.restore.complete"
	case command.CompleteSessionPVCDeletion:
		return "platform.session-archive.pvc-delete.complete"
	case command.CompleteSessionObjectDeletion:
		return "platform.session-archive.object-delete.complete"
	case command.FailSessionArchiveTask:
		return "platform.session-archive.tasks.fail"
	default:
		return ""
	}
}

func validSessionArchiveDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validSessionArchiveError(value string) bool {
	switch value {
	case "SESSION_ARCHIVE_SOURCE_INVALID", "SESSION_ARCHIVE_OBJECT_WRITE_FAILED",
		"SESSION_ARCHIVE_OBJECT_READBACK_FAILED", "SESSION_ARCHIVE_OBJECT_DELETE_FAILED",
		"SESSION_ARCHIVE_RESTORE_INVALID", "SESSION_ARCHIVE_PVC_BUSY",
		"SESSION_ARCHIVE_KUBERNETES_UNAVAILABLE", "SESSION_ARCHIVE_WORKER_FAILED",
		"SESSION_ARCHIVE_TIMEOUT":
		return true
	default:
		return false
	}
}
