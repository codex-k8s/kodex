package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"math"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceRestoreSource resolves the owner-selected immutable receipt without granting object access.
	// queryWorkspaceRestoreSource разрешает выбранную owner immutable receipt без выдачи доступа к объектам.
	//go:embed sql/workspace_restore__source.sql
	queryWorkspaceRestoreSource string
	// queryWorkspaceRestoreAdvance creates a fresh generation only after a released checkpointed writer.
	// queryWorkspaceRestoreAdvance создаёт новое поколение только после released checkpointed writer.
	//go:embed sql/workspace_restore__advance.sql
	queryWorkspaceRestoreAdvance string
	// queryWorkspaceRestoreCreate persists one bounded restore intent for the newly allocated generation.
	// queryWorkspaceRestoreCreate сохраняет один bounded restore intent нового поколения.
	//go:embed sql/workspace_restore__create.sql
	queryWorkspaceRestoreCreate string
	// queryWorkspaceRestoreComplete accepts the exact source receipt and closes storage authority atomically.
	// queryWorkspaceRestoreComplete принимает exact source receipt и атомарно закрывает storage authority.
	//go:embed sql/workspace_restore__complete.sql
	queryWorkspaceRestoreComplete string
)

// workspaceArchiveTaskKind selects the closed data-custody operation from stored workspace state.
// workspaceArchiveTaskKind выбирает закрытую data-custody operation по сохранённому workspace state.
func workspaceArchiveTaskKind(state workspaceArchiveState) string {
	if state.state == "RESTORE_READY" || state.state == "RESTORING" {
		return "RESTORE"
	}
	return "SNAPSHOT"
}

// readWorkspaceRestoreSource pins a same-session accepted checkpoint; references alone never select a source.
// Pending restore requires a live immutable object; an already restored tree retains its historical commitment.
// readWorkspaceRestoreSource закрепляет принятый checkpoint той же session; ссылки сами по себе не выбирают источник.
// Pending restore требует доступный immutable object; восстановленное дерево сохраняет historical commitment.
func readWorkspaceRestoreSource(ctx context.Context, tx pgx.Tx, state workspaceArchiveState) (runtimecontract.WorkspaceCheckpointBinding, string, error) {
	var source runtimecontract.WorkspaceCheckpointBinding
	var archiveID string
	err := tx.QueryRow(ctx, queryWorkspaceRestoreSource, pgx.StrictNamedArgs{
		"organization_id": state.organizationID, "workspace_ref": state.ref, "workspace_generation": state.generation,
	}).Scan(&archiveID, &source.Scope.OrganizationRef, &source.Scope.ProjectRef, &source.Scope.SessionRef, &source.Scope.AgentRef,
		&source.WorkspaceRef, &source.Generation, &source.CheckpointRef, &source.TreeSHA256, &source.Bytes, &source.Entries,
		&source.Manifest.Key, &source.Manifest.Version, &source.Manifest.ETag, &source.Manifest.Digest, &source.Manifest.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, "", errs.ErrConflict
	}
	if err != nil {
		return source, "", errs.ErrUnavailable
	}
	if source.Validate() != nil || source.Scope != state.scope || source.WorkspaceRef != state.ref {
		return source, "", errs.ErrConflict
	}
	return source, archiveID, nil
}

// prepareWorkspaceRestoreTx allocates one new generation and its restore task under the execution owner's session lock.
// This is data custody, not a model lease; current execution admission must run again after RESTORED.
// prepareWorkspaceRestoreTx выделяет одно новое поколение и restore task под session lock владельца execution.
// Это data custody, не model lease; после RESTORED заново выполняется current execution admission.
func (repository *Repository) prepareWorkspaceRestoreTx(ctx context.Context, tx pgx.Tx, current scope, candidate claimableExecution, reference string, generation int64) error {
	state, err := readWorkspaceArchiveState(ctx, tx, current.organizationID, candidate.sessionID)
	if err != nil {
		return err
	}
	if state.state != "CHECKPOINT_READY" || state.ref != reference || state.generation != generation ||
		state.source == nil || generation >= math.MaxInt64 {
		return errs.ErrConflict
	}
	var next int64
	if err := tx.QueryRow(ctx, queryWorkspaceRestoreAdvance, pgx.StrictNamedArgs{
		"organization_id": state.organizationID, "workspace_id": state.id, "workspace_version": state.version,
		"workspace_generation": state.generation, "archive_id": state.sourceArchiveID,
	}).Scan(&next); err != nil || next != generation+1 {
		return errs.ErrConflict
	}
	state, err = readWorkspaceArchiveState(ctx, tx, current.organizationID, candidate.sessionID)
	if err != nil {
		return err
	}
	task, err := createWorkspaceRestore(ctx, tx, state)
	if err != nil {
		return err
	}
	if err := auditWorkspaceArchiveClaim(ctx, tx, current, state, task.ref, "READY"); err != nil {
		return err
	}
	return repository.emitSessionStorageRunChanged(ctx, tx, current, state.sessionID)
}

// createWorkspaceRestore freezes the exact accepted checkpoint and source policy without creating execution or delete credit.
// createWorkspaceRestore фиксирует exact accepted checkpoint и source policy без выдачи execution либо delete credit.
func createWorkspaceRestore(ctx context.Context, tx pgx.Tx, state workspaceArchiveState) (workspaceArchiveTask, error) {
	var task workspaceArchiveTask
	if state.state != "RESTORE_READY" || state.source == nil || state.source.Generation >= state.generation {
		return task, errs.ErrConflict
	}
	var highWatermark int64
	if err := tx.QueryRow(ctx, queryWorkspaceArchiveGeneration, pgx.StrictNamedArgs{"session_id": state.sessionID}).Scan(&highWatermark); err != nil {
		return task, errs.ErrUnavailable
	}
	if highWatermark >= math.MaxInt64 {
		return task, errs.ErrConflict
	}
	var pvcUID string
	var incarnation int64
	var preparationRef, readyRaw string
	if err := tx.QueryRow(ctx, queryWorkspaceRestorePVC, pgx.StrictNamedArgs{
		"organization_id": state.organizationID, "workspace_id": state.id,
	}).Scan(&pvcUID, &incarnation, &preparationRef, &readyRaw); err != nil || pvcUID == "" {
		return task, errs.ErrConflict
	}
	ready, err := runtimecontract.DecodeWorkspaceVolumeReady([]byte(readyRaw))
	if err != nil || ready.Preparation.Scope != state.scope || ready.Preparation.WorkspaceRef != state.ref ||
		ready.Preparation.PVCUID != pvcUID || ready.Preparation.VolumeGeneration != incarnation || ready.Preparation.TaskRef != preparationRef {
		return task, errs.ErrConflict
	}
	body := runtimecontract.WorkspaceArchiveTask{
		Target: runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
			Scope: state.scope, WorkspaceRef: state.ref, Generation: state.generation, VolumeReady: &ready},
		CheckpointRef: state.source.CheckpointRef, PVCUID: pvcUID, Policy: state.policy, Checkpoint: state.source,
	}
	task.ref, err = newRef("sat")
	if err != nil {
		return task, err
	}
	task.kind, task.state, task.contentGeneration, task.workspaceGeneration = "RESTORE", "READY", highWatermark+1, state.generation
	task.maximumAttempts, task.objectKey, task.objectVersion = sessionArchiveMaxAttempts, state.source.Manifest.Key, state.source.Manifest.Version
	task.body, err = json.Marshal(body)
	if err != nil {
		return task, errs.ErrConflict
	}
	task.digest = state.revisionDigest
	work, err := workspaceArchiveWork(state, task, 1)
	if err != nil {
		return task, err
	}
	task.digest, err = workspaceArchiveDigest(work)
	if err != nil {
		return task, err
	}
	err = tx.QueryRow(ctx, queryWorkspaceRestoreCreate, pgx.StrictNamedArgs{
		"task_ref": task.ref, "organization_id": state.organizationID, "project_id": state.projectID, "session_id": state.sessionID,
		"archive_id": state.sourceArchiveID, "workspace_id": state.id, "workspace_generation": state.generation,
		"workspace_input": string(task.body), "content_generation": task.contentGeneration, "input_digest": task.digest,
		"object_key": task.objectKey, "object_version": task.objectVersion,
	}).Scan(&task.id)
	if err != nil {
		return task, errs.ErrUnavailable
	}
	return task, nil
}

// queryWorkspaceRestorePVC requires the retained positive volume incarnation before issuing any restore write.
// queryWorkspaceRestorePVC требует retained positive volume incarnation до выдачи restore write.
//
//go:embed sql/workspace_restore__pvc.sql
var queryWorkspaceRestorePVC string
