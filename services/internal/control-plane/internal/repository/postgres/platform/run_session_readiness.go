package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/run_session_readiness.sql
var queryRunSessionReadiness string

func attachRunSessionReadiness(ctx context.Context, runner queryRunner, scope scope, run *entity.Run) error {
	if run.SessionRef == "" {
		return nil
	}
	read := &entity.RunSessionReadiness{}
	task := &entity.RunSessionArchiveTask{}
	var accountAvailable, earlierExecution bool
	if err := runner.QueryRow(ctx, queryRunSessionReadiness, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "run_ref": run.Ref, "session_ref": run.SessionRef,
	}).Scan(&read.SessionRef, &read.StorageState, &accountAvailable, &earlierExecution,
		&task.Ref, &task.Kind, &task.State, &task.Attempt, &task.MaximumAttempts, &task.SafeErrorCode); err != nil {
		return errs.ErrUnavailable
	}
	if read.SessionRef != run.SessionRef || len(read.SessionRef) < 8 || !validOverlayHistoryRef(read.SessionRef) || !validSessionReadinessStorage(read.StorageState) {
		return errs.ErrUnavailable
	}
	read.Reason = "NO_SESSION_BLOCKER"
	if read.StorageState != "LIVE" && read.StorageState != "UNTRACKED" {
		read.Reason = "STORAGE_NOT_LIVE"
	} else if !accountAvailable {
		read.Reason = "SESSION_ACCOUNT_UNAVAILABLE"
	} else if earlierExecution {
		read.Reason = "EARLIER_EXECUTION"
	}
	if task.Ref != "" {
		if !validSessionReadinessTask(task) {
			return errs.ErrUnavailable
		}
		if task.SafeErrorCode == "" {
			task.SafeErrorCode = "NONE"
		} else if !validSessionArchiveError(task.SafeErrorCode) && task.SafeErrorCode != "SESSION_ARCHIVE_LEASE_EXPIRED" && task.SafeErrorCode != "SESSION_BECAME_ACTIVE" {
			task.SafeErrorCode = "UNKNOWN"
		}
		read.LatestArchiveTask = task
	}
	run.SessionReadiness = read
	return nil
}

func validSessionReadinessStorage(state string) bool {
	switch state {
	case "UNTRACKED", "LIVE", "SNAPSHOT_READY", "SNAPSHOTTING", "DELETE_PVC_READY", "ARCHIVED", "RESTORE_READY", "RESTORING", "ERROR", "PURGED":
		return true
	default:
		return false
	}
}

func validSessionReadinessTask(task *entity.RunSessionArchiveTask) bool {
	if task == nil || len(task.Ref) < 8 || !validOverlayHistoryRef(task.Ref) || task.Attempt < 0 || task.MaximumAttempts < 1 || task.MaximumAttempts > sessionArchiveMaxAttempts || task.Attempt > task.MaximumAttempts {
		return false
	}
	switch task.Kind {
	case "SNAPSHOT", "RESTORE", "DELETE_PVC":
	default:
		return false
	}
	switch task.State {
	case "READY", "CLAIMED", "SUCCEEDED", "DEAD_LETTER", "CANCELLED":
		return true
	default:
		return false
	}
}
