package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// validWorkspaceVolumeFailure closes the safe error vocabulary without accepting raw API diagnostics or caller-selected owner states.
// validWorkspaceVolumeFailure замыкает safe error vocabulary без raw API diagnostics либо caller-selected owner states.
func validWorkspaceVolumeFailure(input command.WorkspaceVolumeTaskInput) bool {
	return input.ObservedPVCUID == "" && input.JobUID == "" && input.PodUID == "" && input.Ready == nil &&
		contains([]string{"ORIGIN_CLOSED", "CREATE_UNCONFIRMED", "PREPARE_UNCONFIRMED", "PVC_MISSING", "PVC_REPLACED",
			"OWNERSHIP_CHANGED", "STORAGE_UNAVAILABLE"}, input.SafeErrorCode)
}

// failWorkspaceVolumeTaskTx terminates only a current unreported attempt and retains its uncertain physical custody without retry write credit.
// failWorkspaceVolumeTaskTx завершает только current unreported attempt, сохраняя uncertain physical custody без retry write credit.
func (repository *Repository) failWorkspaceVolumeTaskTx(ctx context.Context, tx pgx.Tx, current scope, input command.WorkspaceVolumeTaskInput) (commandOutcome, error) {
	if !validWorkspaceVolumeFailure(input) {
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
	if !currentWorkspaceVolumeAttempt(task, attempt, input) {
		return commandOutcome{}, errs.ErrForbidden
	}
	if err := repository.closeWorkspaceVolumeTaskTx(ctx, tx, current, task, "DEAD_LETTER", input.SafeErrorCode); err != nil {
		return commandOutcome{}, err
	}
	custody, err := scanWorkspaceVolumeCustody(tx.QueryRow(ctx, queryWorkspaceVolumeGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "workspace_ref": task.workspaceRef}))
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"taskRef": task.ref,
		"recordedTaskState": "DEAD_LETTER", "recordedVolumeState": custody.state}},
		projectID: task.projectID, resourceKind: "WORKSPACE_VOLUME_TASK", resourceRef: task.ref,
		summary: "Подготовка рабочей папки приостановлена"}, nil
}
