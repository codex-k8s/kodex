package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceVolumeObserve retains a positive exact-attempt fact and closes only that attempt's grant.
	// queryWorkspaceVolumeObserve сохраняет positive exact-attempt fact и закрывает только grant этой attempt.
	//go:embed sql/workspace_volume__observe.sql
	queryWorkspaceVolumeObserve string
	// queryWorkspaceVolumeAcceptCreate sets only the first positive UID; historical Create replay cannot clear a later physical block.
	// queryWorkspaceVolumeAcceptCreate сохраняет только первый positive UID; historical Create replay не снимает поздний physical block.
	//go:embed sql/workspace_volume__accept_create.sql
	queryWorkspaceVolumeAcceptCreate string
	// queryWorkspaceVolumeAcceptPrepare separates retained root observation from fresh admission to READY.
	// queryWorkspaceVolumeAcceptPrepare отделяет retained root observation от fresh admission в READY.
	//go:embed sql/workspace_volume__accept_prepare.sql
	queryWorkspaceVolumeAcceptPrepare string
)

// workspaceVolumeObservationSummary keeps the user-facing observation audit text identical across owner receipts.
// workspaceVolumeObservationSummary сохраняет одинаковый пользовательский текст observation audit в owner receipts.
const workspaceVolumeObservationSummary = "Сохранён результат подготовки рабочей папки"

// currentWorkspaceVolumeAttempt checks only the persisted live tuple; fresh actor/clock eligibility remains a separate owner read.
// currentWorkspaceVolumeAttempt проверяет только persisted live tuple; fresh actor/clock eligibility проверяется отдельным owner read.
func currentWorkspaceVolumeAttempt(task workspaceVolumeTask, attempt workspaceVolumeAttempt, input command.WorkspaceVolumeTaskInput) bool {
	return task.state == "CLAIMED" && int64(task.attempt) == input.Generation &&
		task.volumeGeneration == task.currentVolumeGeneration && attempt.grantState == "ACTIVE" &&
		attempt.unexpired && !attempt.reported
}

// completeWorkspaceVolumeTaskTx records past physical facts without reopening terminal tasks or closing a different live replacement attempt.
// Only a current fresh preparation may grant READY; first positive Create identity can be retained independently of business execution.
// completeWorkspaceVolumeTaskTx сохраняет past physical facts без открытия terminal tasks либо закрытия другой live replacement attempt.
// Только current fresh preparation допускает READY; первый positive Create identity сохраняется отдельно от business execution.
func (repository *Repository) completeWorkspaceVolumeTaskTx(ctx context.Context, tx pgx.Tx, current scope, input command.WorkspaceVolumeTaskInput) (commandOutcome, error) {
	if input.SafeErrorCode != "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	task, err := lockWorkspaceVolumeReportTaskTx(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	attempt, err := loadWorkspaceVolumeAttemptTx(ctx, tx, current, task, input)
	if err != nil {
		return commandOutcome{}, err
	}
	if runtimecontract.ValidateWorkspaceVolumeObservation(attempt.work, input.ObservedPVCUID, input.JobUID, input.PodUID, input.Ready) != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	ready := ""
	if input.Ready != nil {
		raw, err := runtimecontract.EncodeWorkspaceVolumeReady(*input.Ready)
		if err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		ready = string(raw)
	}
	if attempt.reported && (attempt.pvcUID != input.ObservedPVCUID || attempt.jobUID != input.JobUID ||
		attempt.podUID != input.PodUID || attempt.ready != ready) {
		return commandOutcome{}, errs.ErrConflict
	}
	fresh := currentWorkspaceVolumeAttempt(task, attempt, input)
	if fresh {
		deadline, allowed, err := repository.authorizeWorkspaceVolumeGrantTx(ctx, tx, current, task)
		if err != nil {
			return commandOutcome{}, err
		}
		fresh = allowed && (deadline == nil || !attempt.expiresAt.After(*deadline))
		if !fresh {
			if err := repository.closeWorkspaceVolumeTaskTx(ctx, tx, current, task, "CANCELLED", "ORIGIN_CLOSED"); err != nil {
				return commandOutcome{}, err
			}
			task.state = "CANCELLED"
		}
	}
	if !attempt.reported {
		args, err := workspaceVolumeFenceArgs(current, input)
		if err != nil {
			return commandOutcome{}, err
		}
		delete(args, "organization_id")
		delete(args, "task_ref")
		args["task_id"], args["pvc_uid"], args["job_uid"], args["pod_uid"], args["ready_record"] =
			task.id, input.ObservedPVCUID, input.JobUID, input.PodUID, ready
		var recorded bool
		if err := tx.QueryRow(ctx, queryWorkspaceVolumeObserve, args).Scan(&recorded); err != nil || !recorded {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if (task.state == "READY" || task.state == "CLAIMED") &&
		(int64(task.attempt) == input.Generation || attempt.work.Kind == runtimecontract.WorkspaceVolumeCreate) {
		if attempt.work.Kind == runtimecontract.WorkspaceVolumeCreate {
			if _, err := tx.Exec(ctx, queryWorkspaceVolumeCloseGrants, pgx.StrictNamedArgs{"task_id": task.id}); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
		if err := tx.QueryRow(ctx, queryWorkspaceVolumeTerminal, pgx.StrictNamedArgs{
			"task_id": task.id, "state": "SUCCEEDED", "safe_error_code": ""}).Scan(&task.state); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	custody, err := scanWorkspaceVolumeCustody(tx.QueryRow(ctx, queryWorkspaceVolumeGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "workspace_ref": task.workspaceRef}))
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	matched := custody.generation == task.volumeGeneration
	if matched {
		if attempt.work.Kind == runtimecontract.WorkspaceVolumeCreate {
			if custody.pvcUID != "" && custody.pvcUID != input.ObservedPVCUID {
				return commandOutcome{}, errs.ErrConflict
			}
			if custody.pvcUID == "" {
				if err := tx.QueryRow(ctx, queryWorkspaceVolumeAcceptCreate, pgx.StrictNamedArgs{
					"volume_id": custody.id, "generation": custody.generation,
					"create_task_ref": attempt.work.Volume.CreateTaskRef, "pvc_uid": input.ObservedPVCUID}).Scan(&custody.state); err != nil {
					return commandOutcome{}, errs.ErrConflict
				}
			}
		} else {
			if custody.pvcUID != input.ObservedPVCUID || custody.preparationTaskRef != attempt.work.Volume.PreparationTaskRef ||
				custody.readyRecord != "" && custody.readyRecord != ready {
				return commandOutcome{}, errs.ErrConflict
			}
			if custody.readyRecord == "" || fresh && custody.state != "READY" {
				if err := tx.QueryRow(ctx, queryWorkspaceVolumeAcceptPrepare, pgx.StrictNamedArgs{
					"volume_id": custody.id, "generation": custody.generation, "preparation_task_ref": custody.preparationTaskRef,
					"pvc_uid": input.ObservedPVCUID, "ready_record": ready, "make_ready": fresh}).Scan(&custody.state); err != nil {
					return commandOutcome{}, errs.ErrConflict
				}
			}
		}
	}
	if err := auditWorkspaceVolumeTask(ctx, tx, current, task.projectID, task.ref, workspaceVolumeObservationSummary); err != nil {
		return commandOutcome{}, err
	}
	if err := repository.emitSessionStorageRunChanged(ctx, tx, current, task.sessionID); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"taskRef": task.ref, "recordedTaskState": task.state,
		"recordedVolumeState": custody.state, "pastFact": !fresh, "matchedVolumeIncarnation": matched}},
		projectID: task.projectID, resourceKind: "WORKSPACE_VOLUME_TASK", resourceRef: task.ref,
		summary: workspaceVolumeObservationSummary}, nil
}
