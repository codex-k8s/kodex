package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceArchiveTaskSession resolves the persisted discriminator within the trusted organization.
	// queryWorkspaceArchiveTaskSession разрешает stored discriminator внутри trusted organization.
	//go:embed sql/workspace_archive__task_session.sql
	queryWorkspaceArchiveTaskSession string
	// queryWorkspaceArchiveLockSession serializes V2 commands with execution and both archive profiles.
	// queryWorkspaceArchiveLockSession сериализует V2 commands с execution и обоими archive profiles.
	//go:embed sql/workspace_archive__lock_session.sql
	queryWorkspaceArchiveLockSession string
	// queryWorkspaceArchiveRenew extends only the exact current granted V2 snapshot attempt.
	// queryWorkspaceArchiveRenew продлевает только exact current granted V2 snapshot attempt.
	//go:embed sql/workspace_archive__renew.sql
	queryWorkspaceArchiveRenew string
	// queryWorkspaceArchiveComplete atomically accepts a private checkpoint and closes its task without deleting PVC.
	// queryWorkspaceArchiveComplete атомарно принимает private checkpoint и закрывает task без удаления PVC.
	//go:embed sql/workspace_archive__complete.sql
	queryWorkspaceArchiveComplete string
	// queryWorkspaceArchiveFail retains custody and gives later attempts reconciliation-only input.
	// queryWorkspaceArchiveFail сохраняет custody и оставляет следующим attempts только reconciliation input.
	//go:embed sql/workspace_archive__fail.sql
	queryWorkspaceArchiveFail string
)

// lockClaimedWorkspaceArchive rejects stale grants before consuming any completion metadata.
// An absent V2 discriminator alone permits the caller to try the independent legacy path.
// lockClaimedWorkspaceArchive отклоняет stale grants до чтения completion metadata.
// Только отсутствие V2 discriminator допускает отдельный legacy path.
func lockClaimedWorkspaceArchive(ctx context.Context, tx pgx.Tx, current scope, payload command.SessionArchiveTaskInput) (workspaceArchiveState, workspaceArchiveTask, runtimecontract.WorkspaceArchiveWork, bool, error) {
	var state workspaceArchiveState
	var task workspaceArchiveTask
	var work runtimecontract.WorkspaceArchiveWork
	var sessionID string
	err := tx.QueryRow(ctx, queryWorkspaceArchiveTaskSession, pgx.StrictNamedArgs{"organization_id": current.organizationID, "task_ref": payload.TaskRef}).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, task, work, false, nil
	}
	if err != nil {
		return state, task, work, true, errs.ErrUnavailable
	}
	if err := lockSessionContentGeneration(ctx, tx, current.organizationID, sessionID); err != nil {
		return state, task, work, true, err
	}
	state, err = readWorkspaceArchiveState(ctx, tx, current.organizationID, sessionID)
	if err != nil {
		return state, task, work, true, err
	}
	task, err = readWorkspaceArchiveTask(ctx, tx, state)
	if err != nil {
		return state, task, work, true, errs.ErrUnavailable
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if task.ref != payload.TaskRef || task.state != "CLAIMED" || task.leaseRef == nil || task.fenceDigest == nil || task.expiresAt == nil ||
		payload.LeaseRef == "" || payload.Fence == "" || payload.Generation < 1 || task.generation != payload.Generation ||
		*task.leaseRef != payload.LeaseRef || *task.fenceDigest != hex.EncodeToString(digest[:]) || !task.expiresAt.After(time.Now()) {
		return state, task, work, true, errs.ErrForbidden
	}
	work, err = workspaceArchiveWork(state, task, task.attempt)
	if err != nil {
		return state, task, work, true, err
	}
	expected, err := workspaceArchiveDigest(work)
	if err != nil || expected != task.digest {
		return state, task, work, true, errs.ErrConflict
	}
	return state, task, work, true, nil
}

// lockSessionContentGeneration gives all history/checkpoint writers the same lock before a fresh SQL snapshot.
// lockSessionContentGeneration даёт history/checkpoint writers один lock до fresh SQL snapshot.
func lockSessionContentGeneration(ctx context.Context, tx pgx.Tx, organizationID, sessionID string) error {
	var locked string
	if err := tx.QueryRow(ctx, queryWorkspaceArchiveLockSession, pgx.StrictNamedArgs{"organization_id": organizationID, "session_id": sessionID}).Scan(&locked); err != nil || locked != sessionID {
		return errs.ErrConflict
	}
	return nil
}

// changeWorkspaceArchive accepts only a stored V2 task; a request discriminator cannot select its owner.
// Its outcome participates in the outer command receipt/audit transaction and preserves terminal business state.
// changeWorkspaceArchive принимает только stored V2 task; request discriminator не выбирает owner.
// Outcome входит в outer command receipt/audit transaction и сохраняет terminal business state.
func (repository *Repository) changeWorkspaceArchive(ctx context.Context, tx pgx.Tx, current scope, input command.Command, payload command.SessionArchiveTaskInput) (commandOutcome, bool, error) {
	state, task, work, found, err := lockClaimedWorkspaceArchive(ctx, tx, current, payload)
	if err != nil || !found {
		return commandOutcome{}, found, err
	}
	var archiveRef, taskState string
	retry := false
	switch input.Kind {
	case command.CompleteSessionSnapshot:
		if payload.FormatVersion != 2 || task.kind != "SNAPSHOT" || payload.ObjectKey != work.TargetObjectKey ||
			payload.RestoredSourceSHA256 != "" || payload.PVCName != "" || payload.SourceSizeBytes > work.Workspace.Policy.MaximumWritableBytes ||
			payload.SourceEntryCount > work.Workspace.Policy.MaximumFileCount || payload.SourceSizeBytes > 0 && payload.SourceEntryCount == 0 {
			return commandOutcome{}, true, errs.ErrInvalid
		}
		checkpoint := runtimecontract.WorkspaceCheckpointBinding{Scope: work.Workspace.Target.Scope, WorkspaceRef: work.Workspace.Target.WorkspaceRef,
			Generation: work.Workspace.Target.Generation, CheckpointRef: work.Workspace.CheckpointRef,
			TreeSHA256: payload.SnapshotSourceSHA256, Bytes: payload.SourceSizeBytes, Entries: payload.SourceEntryCount,
			Manifest: runtimecontract.WorkspaceObjectCommitment{Key: payload.ObjectKey, Version: payload.ObjectVersion,
				ETag: payload.ObjectETag, Digest: payload.ObjectDigest, SizeBytes: payload.ObjectSizeBytes}}
		if checkpoint.Validate() != nil {
			return commandOutcome{}, true, errs.ErrInvalid
		}
		archiveRef, err = newRef("sar")
		if err != nil {
			return commandOutcome{}, true, err
		}
		err = tx.QueryRow(ctx, queryWorkspaceArchiveComplete, pgx.StrictNamedArgs{"archive_ref": archiveRef, "organization_id": state.organizationID,
			"workspace_id": state.id, "workspace_version": state.version, "task_id": task.id, "task_generation": task.generation,
			"lease_ref": payload.LeaseRef, "fence_digest": *task.fenceDigest, "tree_sha256": checkpoint.TreeSHA256,
			"source_bytes": checkpoint.Bytes, "source_entries": checkpoint.Entries, "object_key": checkpoint.Manifest.Key,
			"object_version": checkpoint.Manifest.Version, "object_etag": checkpoint.Manifest.ETag, "object_digest": checkpoint.Manifest.Digest,
			"object_bytes": checkpoint.Manifest.SizeBytes}).Scan(&archiveRef)
		taskState = "SUCCEEDED"
	case command.CompleteSessionRestore:
		if task.kind != "RESTORE" || state.source == nil || payload.FormatVersion != 2 || payload.PVCName != "" ||
			payload.SnapshotSourceSHA256 != "" || payload.SourceEntryCount != 0 ||
			payload.ObjectKey != state.source.Manifest.Key || payload.ObjectVersion != state.source.Manifest.Version ||
			payload.ObjectETag != state.source.Manifest.ETag || payload.ObjectDigest != state.source.Manifest.Digest ||
			payload.ObjectSizeBytes != state.source.Manifest.SizeBytes || payload.RestoredSourceSHA256 != state.source.TreeSHA256 ||
			payload.SourceSizeBytes != state.source.Bytes {
			return commandOutcome{}, true, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, queryWorkspaceRestoreComplete, pgx.StrictNamedArgs{
			"organization_id": state.organizationID, "workspace_id": state.id, "workspace_version": state.version,
			"archive_id": state.sourceArchiveID, "task_id": task.id, "task_generation": task.generation,
			"lease_ref": payload.LeaseRef, "fence_digest": *task.fenceDigest,
		}).Scan(&archiveRef)
		taskState = "SUCCEEDED"
	case command.FailSessionArchiveTask:
		if !validSessionArchiveError(payload.SafeErrorCode) {
			return commandOutcome{}, true, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, queryWorkspaceArchiveFail, pgx.StrictNamedArgs{"task_id": task.id, "generation": task.generation,
			"lease_ref": payload.LeaseRef, "fence_digest": *task.fenceDigest, "safe_error_code": payload.SafeErrorCode}).Scan(&taskState, &retry)
	default:
		return commandOutcome{}, true, errs.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, true, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	if err := repository.emitSessionStorageRunChanged(ctx, tx, current, state.sessionID); err != nil {
		return commandOutcome{}, true, err
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"taskRef": task.ref, "state": taskState,
		"archiveRef": archiveRef, "retryScheduled": retry}}, projectID: state.projectID, projectRef: state.scope.ProjectRef,
		resourceKind: "SESSION_ARCHIVE_TASK", resourceRef: task.ref, summary: sessionArchiveTaskSummary(taskState)}, true, nil
}

// renewWorkspaceArchive renews the stored V2 lease under the same session lock as claim and completion.
// It reports legacy absence distinctly; any lookup or custody failure remains closed.
// renewWorkspaceArchive продлевает stored V2 lease под тем же session lock, что claim и completion.
// Отсутствие legacy discriminator выделено отдельно; любая ошибка lookup либо custody закрыта.
func (repository *Repository) renewWorkspaceArchive(ctx context.Context, current scope, payload command.SessionArchiveTaskInput) (result map[string]any, found bool, resultErr error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, true, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	state, task, _, found, err := lockClaimedWorkspaceArchive(ctx, tx, current, payload)
	if err != nil || !found {
		return nil, found, err
	}
	expires := time.Now().UTC().Add(sessionArchiveLeaseDuration)
	if err := tx.QueryRow(ctx, queryWorkspaceArchiveRenew, pgx.StrictNamedArgs{"task_id": task.id, "workspace_id": state.id,
		"workspace_generation": state.generation, "lease_ref": payload.LeaseRef, "fence_digest": *task.fenceDigest,
		"generation": payload.Generation, "lease_expires_at": expires}).Scan(&expires); err != nil {
		return nil, true, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, true, errs.ErrUnavailable
	}
	return map[string]any{"leaseRef": payload.LeaseRef, "fence": payload.Fence, "generation": payload.Generation, "expiresAt": expires}, true, nil
}
