package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRunSessionReadinessCasterPreservesExactBinding(t *testing.T) {
	t.Parallel()
	if castRun(entity.Run{}).SessionReadiness != nil {
		t.Fatal("missing session proof invented")
	}
	value := entity.Run{Ref: "run_fixture01", SessionRef: "ses_fixture01", SessionReadiness: &entity.RunSessionReadiness{SessionRef: "ses_fixture01", StorageState: "ERROR", Reason: "STORAGE_NOT_LIVE", LatestArchiveTask: &entity.RunSessionArchiveTask{Ref: "sat_fixture01", Kind: "SNAPSHOT", State: "DEAD_LETTER", Attempt: 5, MaximumAttempts: 5, SafeErrorCode: "SESSION_ARCHIVE_SOURCE_INVALID"}}}
	result := castRun(value).SessionReadiness
	if result.SessionRef != value.SessionRef || result.StorageState != cp.RunSessionStorageState_RUN_SESSION_STORAGE_STATE_ERROR || result.LatestArchiveTask.Ref != "sat_fixture01" || result.LatestArchiveTask.Attempt != 5 {
		t.Fatal("session/task proof lost")
	}
	value.SessionReadiness.StorageState = "PURGED"
	if castRun(value).SessionReadiness.StorageState != cp.RunSessionStorageState_RUN_SESSION_STORAGE_STATE_PURGED {
		t.Fatal("canonical purged storage state lost")
	}
}
