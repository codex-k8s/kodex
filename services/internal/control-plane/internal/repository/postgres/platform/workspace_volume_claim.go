package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceVolumeCandidates returns bounded references without taking locks ahead of the root owner.
	// queryWorkspaceVolumeCandidates возвращает bounded references без блокировок раньше root owner.
	//go:embed sql/workspace_volume__candidates.sql
	queryWorkspaceVolumeCandidates string
	// queryWorkspaceVolumeLockOrigin serializes a candidate with terminal owner commands and skips busy roots.
	// queryWorkspaceVolumeLockOrigin сериализует candidate с terminal owner commands, пропуская busy roots.
	//go:embed sql/workspace_volume__lock_origin.sql
	queryWorkspaceVolumeLockOrigin string
	// queryWorkspaceVolumeLockSession retains the existing root-before-session lock order.
	// queryWorkspaceVolumeLockSession сохраняет существующий root-before-session lock order.
	//go:embed sql/workspace_volume__lock_session.sql
	queryWorkspaceVolumeLockSession string
	// queryWorkspaceVolumeLockCustody locks physical custody before its logical task.
	// queryWorkspaceVolumeLockCustody блокирует physical custody до logical task.
	//go:embed sql/workspace_volume__lock_custody.sql
	queryWorkspaceVolumeLockCustody string
	// queryWorkspaceVolumeTask reads the exact trusted organization and current incarnation under the task lock.
	// queryWorkspaceVolumeTask читает exact trusted organization/current incarnation под task lock.
	//go:embed sql/workspace_volume__task.sql
	queryWorkspaceVolumeTask string
	// queryWorkspaceVolumeCloseGrants closes all previous credits before a replacement attempt or terminal transition.
	// queryWorkspaceVolumeCloseGrants закрывает previous credits до replacement attempt либо terminal transition.
	//go:embed sql/workspace_volume__close_grants.sql
	queryWorkspaceVolumeCloseGrants string
	// queryWorkspaceVolumeClaim increments the bounded attempt without rewriting its logical intent.
	// queryWorkspaceVolumeClaim увеличивает bounded attempt без изменения logical intent.
	//go:embed sql/workspace_volume__claim.sql
	queryWorkspaceVolumeClaim string
	// queryWorkspaceVolumeGrant persists the exact effective input, secret-fence digest and server-clock expiry.
	// queryWorkspaceVolumeGrant сохраняет exact effective input, secret-fence digest и server-clock expiry.
	//go:embed sql/workspace_volume__grant.sql
	queryWorkspaceVolumeGrant string
	// queryWorkspaceVolumeTerminal closes an open task without mutating terminal history.
	// queryWorkspaceVolumeTerminal закрывает open task без изменения terminal history.
	//go:embed sql/workspace_volume__terminal.sql
	queryWorkspaceVolumeTerminal string
	// queryWorkspaceVolumeBlock retains unconfirmed physical data while denying future private writes.
	// queryWorkspaceVolumeBlock сохраняет unconfirmed physical data, запрещая future private writes.
	//go:embed sql/workspace_volume__block.sql
	queryWorkspaceVolumeBlock string
)

// workspaceVolumeTask is the owner-locked logical origin and physical incarnation, independent of any model revision.
// workspaceVolumeTask — owner-locked logical origin/physical incarnation, отдельные от model revision.
type workspaceVolumeTask struct {
	id, ref, kind, state, nodeID, rootRunID, runID, actorID, volumeID, volumeState string
	workspaceRef, projectID, sessionID, workspaceState                             string
	attempt                                                                        int32
	volumeGeneration, currentVolumeGeneration                                      int64
	noWriter, available, activeGrant                                               bool
	input                                                                          runtimecontract.WorkspaceVolumeInput
}

// lockWorkspaceVolumeTask acquires session, custody and task after the caller's root lock and rejects a changed logical lineage.
// lockWorkspaceVolumeTask блокирует session/custody/task после caller root lock и отклоняет changed logical lineage.
func lockWorkspaceVolumeTask(ctx context.Context, tx pgx.Tx, current scope, taskRef, rootID string) (workspaceVolumeTask, error) {
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "task_ref": taskRef}
	for _, query := range []string{queryWorkspaceVolumeLockSession, queryWorkspaceVolumeLockCustody} {
		var id string
		if err := tx.QueryRow(ctx, query, args).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return workspaceVolumeTask{}, errs.ErrNotFound
		} else if err != nil {
			return workspaceVolumeTask{}, errs.ErrUnavailable
		}
	}
	var task workspaceVolumeTask
	var raw []byte
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeTask, args).Scan(&task.id, &task.ref, &task.kind, &task.state, &task.attempt,
		&raw, &task.nodeID, &task.rootRunID, &task.runID, &task.actorID, &task.volumeID, &task.volumeState, &task.volumeGeneration, &task.currentVolumeGeneration,
		&task.workspaceRef, &task.projectID, &task.sessionID, &task.workspaceState, &task.noWriter, &task.available, &task.activeGrant); errors.Is(err, pgx.ErrNoRows) {
		return workspaceVolumeTask{}, errs.ErrNotFound
	} else if err != nil {
		return workspaceVolumeTask{}, errs.ErrUnavailable
	}
	if task.rootRunID != rootID || runtimecontract.DecodeWorkspaceStorageTaskJSON(raw, &task.input) != nil ||
		task.input.WorkspaceRef != task.workspaceRef || task.input.VolumeGeneration != task.volumeGeneration {
		return workspaceVolumeTask{}, errs.ErrConflict
	}
	return task, nil
}

// closeWorkspaceVolumeTaskTx atomically revokes every active attempt and retains uncertain custody without changing business-run history.
// closeWorkspaceVolumeTaskTx атомарно отзывает active attempts и сохраняет uncertain custody без изменения business-run history.
func (repository *Repository) closeWorkspaceVolumeTaskTx(ctx context.Context, tx pgx.Tx, current scope, task workspaceVolumeTask, state, reason string) error {
	if _, err := tx.Exec(ctx, queryWorkspaceVolumeCloseGrants, pgx.StrictNamedArgs{"task_id": task.id}); err != nil {
		return errs.ErrUnavailable
	}
	var terminal string
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeTerminal, pgx.StrictNamedArgs{
		"task_id": task.id, "state": state, "safe_error_code": reason}).Scan(&terminal); err != nil {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryWorkspaceVolumeBlock, pgx.StrictNamedArgs{
		"volume_id": task.volumeID, "generation": task.volumeGeneration, "reason": reason}); err != nil {
		return errs.ErrUnavailable
	}
	if err := auditWorkspaceVolumeTask(ctx, tx, current, task.projectID, task.ref, "Подготовка рабочей папки приостановлена"); err != nil {
		return err
	}
	return repository.emitSessionStorageRunChanged(ctx, tx, current, task.sessionID)
}

// claimWorkspaceVolumeTasksTx grants only fresh owner-authorized attempts; ambiguous responses replay this command's durable receipt.
// Each replacement is reconciliation-only and never repeats a PVC Create or root initializer.
// claimWorkspaceVolumeTasksTx выдаёт только fresh owner-authorized attempts; неизвестный ответ повторяет durable receipt команды.
// Каждая replacement attempt допускает только reconciliation без повтора PVC Create либо root initializer.
func (repository *Repository) claimWorkspaceVolumeTasksTx(ctx context.Context, tx pgx.Tx, current scope, input command.WorkspaceVolumeClaimInput) (commandOutcome, error) {
	if input.Limit < 1 || input.Limit > 16 || strings.TrimSpace(input.WorkloadInstance) == "" || len(input.WorkloadInstance) > 256 {
		return commandOutcome{}, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryWorkspaceVolumeCandidates, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "limit": input.Limit * 4})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Ref, RootID string }])
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	items := make([]map[string]any, 0, input.Limit)
	changed := false
	for _, candidate := range candidates {
		if len(items) >= int(input.Limit) {
			break
		}
		var rootID string
		if err := tx.QueryRow(ctx, queryWorkspaceVolumeLockOrigin, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "root_run_id": candidate.RootID}).Scan(&rootID); errors.Is(err, pgx.ErrNoRows) {
			continue
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		task, err := lockWorkspaceVolumeTask(ctx, tx, current, candidate.Ref, rootID)
		if err != nil {
			return commandOutcome{}, err
		}
		if task.state != "READY" && task.state != "CLAIMED" || !task.available || task.activeGrant {
			continue
		}
		if task.volumeGeneration != task.currentVolumeGeneration {
			return commandOutcome{}, errs.ErrConflict
		}
		deadline, allowed, err := repository.authorizeWorkspaceVolumeGrantTx(ctx, tx, current, task)
		if err != nil {
			return commandOutcome{}, err
		}
		if !allowed {
			if err := repository.closeWorkspaceVolumeTaskTx(ctx, tx, current, task, "CANCELLED", "ORIGIN_CLOSED"); err != nil {
				return commandOutcome{}, err
			}
			changed = true
			continue
		}
		if task.attempt >= sessionArchiveMaxAttempts {
			reason := "PREPARE_UNCONFIRMED"
			if task.kind == runtimecontract.WorkspaceVolumeCreate {
				reason = "CREATE_UNCONFIRMED"
			}
			if err := repository.closeWorkspaceVolumeTaskTx(ctx, tx, current, task, "DEAD_LETTER", reason); err != nil {
				return commandOutcome{}, err
			}
			changed = true
			continue
		}
		work := runtimecontract.WorkspaceVolumeWork{TaskRef: task.ref, Kind: task.kind, Attempt: task.attempt + 1,
			Volume: task.input, ReconcileOnly: task.input.ReadOnly || task.attempt > 0}
		digest, err := work.Digest()
		if err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		snapshot, err := json.Marshal(work)
		if err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		leaseRef, err := newRef("lea")
		if err != nil {
			return commandOutcome{}, err
		}
		fence, err := newRef("fnc")
		if err != nil {
			return commandOutcome{}, err
		}
		if _, err := tx.Exec(ctx, queryWorkspaceVolumeCloseGrants, pgx.StrictNamedArgs{"task_id": task.id}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var attempt int32
		if err := tx.QueryRow(ctx, queryWorkspaceVolumeClaim, pgx.StrictNamedArgs{
			"task_id": task.id, "previous_attempt": task.attempt}).Scan(&attempt); err != nil || attempt != work.Attempt {
			return commandOutcome{}, errs.ErrConflict
		}
		fenceDigest := sha256.Sum256([]byte(fence))
		var expires time.Time
		if err := tx.QueryRow(ctx, queryWorkspaceVolumeGrant, pgx.StrictNamedArgs{
			"task_id": task.id, "attempt": attempt, "lease_ref": leaseRef, "fence_digest": hex.EncodeToString(fenceDigest[:]),
			"workload_instance": input.WorkloadInstance, "input_snapshot": string(snapshot), "input_digest": digest, "deadline_at": deadline}).Scan(&expires); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		work.InputDigest = digest
		items = append(items, map[string]any{"work": work, "leaseRef": leaseRef, "fence": fence,
			"generation": int64(attempt), "expiresAt": expires})
		if err := auditWorkspaceVolumeTask(ctx, tx, current, task.projectID, task.ref, "Подготавливается рабочая папка"); err != nil {
			return commandOutcome{}, err
		}
		if err := repository.emitSessionStorageRunChanged(ctx, tx, current, task.sessionID); err != nil {
			return commandOutcome{}, err
		}
	}
	return commandOutcome{result: command.Result{RuntimeItems: items}, runtimeGraphChanged: changed,
		resourceKind: "ORGANIZATION", resourceRef: current.organizationRef, summary: "Обработана очередь подготовки рабочих папок"}, nil
}
