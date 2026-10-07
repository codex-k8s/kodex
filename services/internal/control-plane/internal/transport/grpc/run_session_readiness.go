package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func castRunSessionReadiness(value *entity.RunSessionReadiness) *cp.RunSessionReadiness {
	if value == nil {
		return nil
	}
	read := &cp.RunSessionReadiness{SessionRef: value.SessionRef,
		StorageState: cp.RunSessionStorageState(cp.RunSessionStorageState_value["RUN_SESSION_STORAGE_STATE_"+value.StorageState]),
		Reason:       cp.RunSessionReadinessReason(cp.RunSessionReadinessReason_value["RUN_SESSION_READINESS_REASON_"+value.Reason])}
	if task := value.LatestArchiveTask; task != nil {
		read.LatestArchiveTask = &cp.RunSessionArchiveTask{Ref: task.Ref,
			Kind:    cp.SessionArchiveTaskKind(cp.SessionArchiveTaskKind_value["SESSION_ARCHIVE_TASK_KIND_"+task.Kind]),
			State:   cp.RunSessionArchiveTaskState(cp.RunSessionArchiveTaskState_value["RUN_SESSION_ARCHIVE_TASK_STATE_"+task.State]),
			Attempt: task.Attempt, MaximumAttempts: task.MaximumAttempts, SafeErrorCode: task.SafeErrorCode}
	}
	return read
}
