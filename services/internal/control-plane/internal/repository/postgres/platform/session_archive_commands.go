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

// lockedSessionArchiveTask holds owner-locked task/storage/session pins, nullable-lease-derived
// binding, input/object/source identity, attempt budget and current execution status. It is
// internal transaction state used by specialized completion checks, not caller-supplied authority.
// lockedSessionArchiveTask хранит owner-locked task/storage/session pins, binding из nullable
// lease fields, input/object/source identity, attempt budget и current execution status. Это
// internal transaction state для specialized completion checks, а не caller-supplied authority.
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

// changeSessionArchive checks typed worker input and operation permission before exact task
// locking, then dispatches snapshot/restore/PVC/object completion or closed-code failure with
// kind-specific descriptor guards. It maps no row to Conflict, emits the applicable storage wake
// and returns task outcome; owner receipt/audit/commit remain outside. It records worker evidence
// without independently performing or proving the external storage action.
// changeSessionArchive проверяет typed worker input и operation permission до exact task lock,
// затем dispatch snapshot/restore/PVC/object completion либо closed-code failure с kind-specific
// descriptor guards. No row даёт Conflict, выпускается применимый storage wake и возвращается task
// outcome; owner receipt/audit/commit остаются снаружи. Записывает worker evidence, самостоятельно
// не выполняя и не доказывая external storage action.
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
	if locked.kind != "DELETE_OBJECT" || (input.Kind == command.CompleteSessionObjectDeletion && locked.currentArchiveID != "" && locked.currentArchiveID == locked.archiveID) {
		if err := repository.emitSessionStorageRunChanged(ctx, tx, scope, locked.sessionID); err != nil {
			return commandOutcome{}, err
		}
	}
	return commandOutcome{
		result:    command.Result{Runtime: map[string]any{"taskRef": locked.ref, "state": state, "archiveRef": archiveRef, "retryScheduled": retryScheduled}},
		projectID: locked.projectID, resourceKind: "SESSION_ARCHIVE_TASK", resourceRef: locked.ref,
		summary: sessionArchiveTaskSummary(state),
	}, nil
}

// sessionArchiveTaskSummary maps successful, retry-ready and dead-letter task states to fixed i18n
// summaries; unknown states use the dead-letter presentation fallback. It changes no durable state
// and does not classify a new retry as safe.
// sessionArchiveTaskSummary отображает successful, retry-ready и dead-letter task states в fixed
// i18n summaries; unknown states используют dead-letter presentation fallback. Не меняет durable
// state и не объявляет новый retry безопасным.
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

// lockSessionArchiveTask validates the supplied binding, locks exact tenant task/storage and
// rejects non-CLAIMED or missing/partially revoked lease fields before dereference. Exact lease,
// generation, SHA-256 fence and future expiry must match; non-object-delete tasks also require
// current storage generation. It returns owner state without committing or checking external
// object/PVC identity.
// lockSessionArchiveTask проверяет supplied binding, блокирует exact tenant task/storage и
// отклоняет non-CLAIMED либо missing/partially revoked lease fields до dereference. Совпадают
// exact lease, generation, SHA-256 fence и future expiry; для non-object-delete tasks также
// требуется current storage generation. Возвращает owner state без commit либо проверки external
// object/PVC identity.
func (repository *Repository) lockSessionArchiveTask(ctx context.Context, tx pgx.Tx, scope scope, payload command.SessionArchiveTaskInput) (lockedSessionArchiveTask, error) {
	if payload.TaskRef == "" || payload.LeaseRef == "" || payload.Fence == "" || payload.Generation < 1 {
		return lockedSessionArchiveTask{}, errs.ErrInvalid
	}
	var task lockedSessionArchiveTask
	var storedFenceDigest, storedLeaseRef *string
	var storedLeaseExpiresAt *time.Time
	err := tx.QueryRow(ctx, querySessionArchiveLockTask, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "task_ref": payload.TaskRef,
	}).Scan(&task.id, &task.ref, &task.kind, &task.state, &task.generation, &storedFenceDigest,
		&storedLeaseRef, &storedLeaseExpiresAt, &task.attempt, &task.maximumAttempts,
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
	// READY и terminal задачи штатно не имеют lease. NULL либо частичный отзыв
	// authority закрыто отклоняется до использования сохранённого fence.
	if task.state != "CLAIMED" || storedFenceDigest == nil || storedLeaseRef == nil || storedLeaseExpiresAt == nil {
		return lockedSessionArchiveTask{}, errs.ErrForbidden
	}
	task.fenceDigest, task.leaseRef, task.leaseExpiresAt = *storedFenceDigest, *storedLeaseRef, *storedLeaseExpiresAt
	fenceDigest := sha256.Sum256([]byte(payload.Fence))
	if task.leaseRef != payload.LeaseRef || task.generation != payload.Generation ||
		task.fenceDigest != hex.EncodeToString(fenceDigest[:]) || !task.leaseExpiresAt.After(time.Now()) ||
		(task.kind != "DELETE_OBJECT" && task.contentGeneration != task.storageGeneration) {
		return lockedSessionArchiveTask{}, errs.ErrForbidden
	}
	return task, nil
}

// sessionArchivePermission recognizes only the five specialized
// snapshot/restore/PVC-delete/object-delete completion and task-failure command permissions.
// Unknown kinds map to empty; this classifier neither validates a principal nor authorizes a task.
// sessionArchivePermission распознаёт только пять specialized
// snapshot/restore/PVC-delete/object-delete completion и task-failure command permissions. Unknown
// kinds дают empty; classifier не проверяет principal и не авторизует task.
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

// validSessionArchiveDigest requires a sha256: prefix and exactly32 bytes of decodable hexadecimal
// text, accepting either hex letter case. It checks syntax only, without comparing source/object
// bytes or verifying provenance.
// validSessionArchiveDigest требует sha256: prefix и ровно32 bytes декодируемого hexadecimal text,
// принимая оба регистра hex letters. Проверяет только syntax, без сравнения source/object bytes
// либо проверки provenance.
func validSessionArchiveDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

// validSessionArchiveError accepts only the closed archive
// source/object/restore/PVC/Kubernetes/worker/timeout error vocabulary. It prevents arbitrary
// worker diagnostics entering this field but does not determine retryability or alter task
// lifecycle itself.
// validSessionArchiveError принимает только closed archive
// source/object/restore/PVC/Kubernetes/worker/timeout error vocabulary. Не допускает произвольные
// worker diagnostics в поле, но сам не определяет retryability и не меняет task lifecycle.
func validSessionArchiveError(value string) bool {
	switch value {
	case "SESSION_ARCHIVE_SOURCE_INVALID", "SESSION_ARCHIVE_OBJECT_WRITE_FAILED",
		"SESSION_ARCHIVE_OBJECT_READBACK_FAILED", "SESSION_ARCHIVE_OBJECT_DELETE_FAILED",
		"SESSION_ARCHIVE_RESTORE_INVALID", "SESSION_ARCHIVE_PVC_BUSY",
		"SESSION_ARCHIVE_PVC_MISSING", "SESSION_ARCHIVE_PVC_REPLACED",
		"SESSION_ARCHIVE_KUBERNETES_UNAVAILABLE", "SESSION_ARCHIVE_WORKER_FAILED",
		"SESSION_ARCHIVE_TIMEOUT":
		return true
	default:
		return false
	}
}
