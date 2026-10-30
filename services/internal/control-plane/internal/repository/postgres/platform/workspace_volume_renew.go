package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceVolumeReportOrigin resolves a trusted root only after an exact durable attempt/fence match.
	// queryWorkspaceVolumeReportOrigin разрешает trusted root только после exact durable attempt/fence match.
	//go:embed sql/workspace_volume__report_origin.sql
	queryWorkspaceVolumeReportOrigin string
	// queryWorkspaceVolumeRenew extends the same unreported live grant without changing its attempt or immutable input.
	// queryWorkspaceVolumeRenew продлевает тот же unreported live grant без изменения attempt либо immutable input.
	//go:embed sql/workspace_volume__renew.sql
	queryWorkspaceVolumeRenew string
)

// workspaceVolumeFenceArgs bounds the private proof before a database lookup; a reference alone cannot initiate deadline reconciliation.
// workspaceVolumeFenceArgs ограничивает private proof до database lookup; одна reference не запускает deadline reconciliation.
func workspaceVolumeFenceArgs(current scope, input command.WorkspaceVolumeTaskInput) (pgx.StrictNamedArgs, error) {
	if len(input.TaskRef) < 8 || len(input.TaskRef) > 96 || len(input.LeaseRef) < 8 || len(input.LeaseRef) > 96 ||
		len(input.Fence) < 8 || len(input.Fence) > 96 || input.Generation < 1 || input.Generation > 5 {
		return nil, errs.ErrForbidden
	}
	fence := sha256.Sum256([]byte(input.Fence))
	return pgx.StrictNamedArgs{"organization_id": current.organizationID, "task_ref": input.TaskRef,
		"lease_ref": input.LeaseRef, "generation": input.Generation, "fence_digest": hex.EncodeToString(fence[:])}, nil
}

// lockWorkspaceVolumeReportTaskTx binds a report to its durable fence before locking the server-owned root, session, volume and task.
// A busy root is unavailable rather than proof of a missing task, and no unverified reference can expire another graph.
// lockWorkspaceVolumeReportTaskTx связывает report с durable fence до блокировки server-owned root/session/volume/task.
// Busy root означает unavailable, не отсутствие task; непроверенная reference не может завершить чужой graph по deadline.
func lockWorkspaceVolumeReportTaskTx(ctx context.Context, tx pgx.Tx, current scope, input command.WorkspaceVolumeTaskInput) (workspaceVolumeTask, error) {
	args, err := workspaceVolumeFenceArgs(current, input)
	if err != nil {
		return workspaceVolumeTask{}, err
	}
	var rootID string
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeReportOrigin, args).Scan(&rootID); errors.Is(err, pgx.ErrNoRows) {
		return workspaceVolumeTask{}, errs.ErrForbidden
	} else if err != nil {
		return workspaceVolumeTask{}, errs.ErrUnavailable
	}
	var lockedRoot string
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeLockOrigin, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": rootID}).Scan(&lockedRoot); errors.Is(err, pgx.ErrNoRows) {
		return workspaceVolumeTask{}, errs.ErrUnavailable
	} else if err != nil {
		return workspaceVolumeTask{}, errs.ErrUnavailable
	}
	if lockedRoot != rootID {
		return workspaceVolumeTask{}, errs.ErrConflict
	}
	return lockWorkspaceVolumeTask(ctx, tx, current, input.TaskRef, rootID)
}

// RenewWorkspaceVolumeTask requires the exact service method, live current attempt and fresh origin before extending only its existing expiry.
// Expired, reported, replaced or terminal attempts never reopen; rejected current origins atomically close their grants and retain custody.
// RenewWorkspaceVolumeTask требует exact service method, live current attempt и fresh origin до продления только прежнего expiry.
// Expired/reported/replaced/terminal attempts не открываются; отказ current origin атомарно закрывает grants, сохраняя custody.
func (repository *Repository) RenewWorkspaceVolumeTask(ctx context.Context, principal value.Principal, input command.WorkspaceVolumeTaskInput) (map[string]any, error) {
	if principal.CallerWorkload != "session-archive" || principal.Permission != command.WorkspaceVolumeRenewPermission {
		return nil, errs.ErrForbidden
	}
	if _, err := workspaceVolumeFenceArgs(scope{}, input); err != nil {
		return nil, err
	}
	if input.ObservedPVCUID != "" || input.JobUID != "" || input.PodUID != "" || input.Ready != nil || input.SafeErrorCode != "" {
		return nil, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	if current.authorityProjectID != "" {
		return nil, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	task, err := lockWorkspaceVolumeReportTaskTx(ctx, tx, current, input)
	if err != nil {
		return nil, err
	}
	attempt, err := loadWorkspaceVolumeAttemptTx(ctx, tx, current, task, input)
	if err != nil {
		return nil, err
	}
	if task.state != "CLAIMED" || int64(task.attempt) != input.Generation || task.volumeGeneration != task.currentVolumeGeneration ||
		attempt.grantState != "ACTIVE" || !attempt.unexpired || attempt.reported {
		return nil, errs.ErrForbidden
	}
	deadline, allowed, err := repository.authorizeWorkspaceVolumeGrantTx(ctx, tx, current, task)
	if err != nil {
		return nil, err
	}
	if !allowed || deadline != nil && attempt.expiresAt.After(*deadline) {
		if err := repository.closeWorkspaceVolumeTaskTx(ctx, tx, current, task, "CANCELLED", "ORIGIN_CLOSED"); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return nil, errs.ErrForbidden
	}
	args, err := workspaceVolumeFenceArgs(current, input)
	if err != nil {
		return nil, err
	}
	delete(args, "organization_id")
	delete(args, "task_ref")
	args["task_id"] = task.id
	args["deadline_at"] = deadline
	var expires time.Time
	if err := tx.QueryRow(ctx, queryWorkspaceVolumeRenew, args).Scan(&expires); errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrForbidden
	} else if err != nil {
		return nil, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return map[string]any{"leaseRef": input.LeaseRef, "fence": input.Fence, "generation": input.Generation, "expiresAt": expires}, nil
}
