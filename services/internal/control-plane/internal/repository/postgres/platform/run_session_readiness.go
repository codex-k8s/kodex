package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRunSessionReadiness embeds exact run/session storage state, provider-account availability,
// earlier execution blocker and latest archive task metadata after canonical run eligibility. It
// projects readiness hints without changing leases, storage or provider state.
// queryRunSessionReadiness встраивает точные состояние хранилища run/session, доступность
// provider-account, блокирующее раннее исполнение и последнюю задачу архива после канонической
// проверки видимости запуска. Проецирует признаки готовности без изменения leases, хранилища и
// состояния провайдера.
//
//go:embed sql/run_session_readiness.sql
var queryRunSessionReadiness string

// attachRunSessionReadiness skips runs without a session, otherwise validates exact
// session/storage/task metadata and assigns a closed-priority blocker reason: storage, account,
// then earlier execution. Unknown safe error codes normalize to UNKNOWN and empty ones to NONE. It
// attaches hints only; current command eligibility still requires authoritative checks.
// attachRunSessionReadiness пропускает запуск без сессии, иначе проверяет точные метаданные
// сессии, хранилища и задачи и назначает причину по закрытому приоритету: storage, account, затем
// раннее исполнение. Неизвестные безопасные коды ошибок заменяет UNKNOWN, пустые — NONE. Добавляет
// только признаки; допустимость команды требует авторитетных проверок.
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

// validSessionReadinessStorage accepts only the closed set of known live, snapshot, archive,
// restore, error and purged storage states. It validates readback shape and does not itself allow
// a storage transition.
// validSessionReadinessStorage принимает только закрытый набор известных состояний live, snapshot,
// archive, restore, error и purged. Проверяет форму readback, но сам не разрешает переход
// хранилища.
func validSessionReadinessStorage(state string) bool {
	switch state {
	case "UNTRACKED", "LIVE", "SNAPSHOT_READY", "SNAPSHOTTING", "DELETE_PVC_READY", "ARCHIVED", "RESTORE_READY", "RESTORING", "ERROR", "PURGED":
		return true
	default:
		return false
	}
}

// validSessionReadinessTask requires a nonnil bounded task ref, nonnegative attempt within
// configured limits and a known SNAPSHOT/RESTORE/DELETE_PVC kind plus
// READY/CLAIMED/SUCCEEDED/DEAD_LETTER/CANCELLED state. It does not verify claim ownership, lease
// freshness or external archive completion.
// validSessionReadinessTask требует непустую корректную ссылку задачи, неотрицательную попытку в
// заданных пределах и известные вид SNAPSHOT/RESTORE/DELETE_PVC и состояние
// READY/CLAIMED/SUCCEEDED/DEAD_LETTER/CANCELLED. Не проверяет владельца захвата, свежесть аренды и
// внешнее завершение архива.
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
