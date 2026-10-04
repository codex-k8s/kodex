package httptransport

import (
	"encoding/json"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func sessionReadinessWireFixture() *cp.Run {
	run := systemRunIdentityFixture()
	run.SessionRef = "ses_fixture01"
	run.SessionReadiness = &cp.RunSessionReadiness{SessionRef: "ses_fixture01", StorageState: cp.RunSessionStorageState_RUN_SESSION_STORAGE_STATE_ERROR, Reason: cp.RunSessionReadinessReason_RUN_SESSION_READINESS_REASON_STORAGE_NOT_LIVE,
		LatestArchiveTask: &cp.RunSessionArchiveTask{Ref: "sat_fixture01", Kind: cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT, State: cp.RunSessionArchiveTaskState_RUN_SESSION_ARCHIVE_TASK_STATE_DEAD_LETTER, Attempt: 5, MaximumAttempts: 5, SafeErrorCode: "SESSION_ARCHIVE_SOURCE_INVALID"}}
	return run
}
func TestRunSessionReadinessWireClosed(t *testing.T) {
	t.Parallel()
	value, err := messageMap(&cp.GetRunResponse{Run: sessionReadinessWireFixture()})
	if err != nil {
		t.Fatal(err)
	}
	run := value["run"].(map[string]any)
	read := run["sessionReadiness"].(map[string]any)
	task := read["latestArchiveTask"].(map[string]any)
	if read["sessionRef"] != "ses_fixture01" || read["storageState"] != "ERROR" || read["reason"] != "STORAGE_NOT_LIVE" || task["state"] != "DEAD_LETTER" || task["kind"] != "SNAPSHOT" || task["attempt"] != float64(5) {
		t.Fatal("session proof not preserved")
	}
	// Нулевая attempt штатной READY-задачи присутствует явно, а не теряется в proto JSON.
	runFixture := sessionReadinessWireFixture()
	runFixture.SessionReadiness.LatestArchiveTask.Attempt = 0
	runFixture.SessionReadiness.LatestArchiveTask.State = cp.RunSessionArchiveTaskState_RUN_SESSION_ARCHIVE_TASK_STATE_READY
	if _, err := messageMap(&cp.GetRunResponse{Run: runFixture}); err != nil {
		t.Fatal(err)
	}
}
func TestRunSessionReadinessWireRejectsUnknownAndLeak(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*cp.Run){
		"foreign session":     func(r *cp.Run) { r.SessionReadiness.SessionRef = "ses_foreign01" },
		"unknown storage":     func(r *cp.Run) { r.SessionReadiness.StorageState = cp.RunSessionStorageState(999) },
		"unspecified storage": func(r *cp.Run) { r.SessionReadiness.StorageState = 0 },
		"unspecified reason":  func(r *cp.Run) { r.SessionReadiness.Reason = 0 },
		"false readiness": func(r *cp.Run) {
			r.SessionReadiness.Reason = cp.RunSessionReadinessReason_RUN_SESSION_READINESS_REASON_NO_SESSION_BLOCKER
		},
		"unknown state": func(r *cp.Run) { r.SessionReadiness.LatestArchiveTask.State = 999 },
		"cleanup mistaken for storage task": func(r *cp.Run) {
			r.SessionReadiness.LatestArchiveTask.Kind = cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_OBJECT
		},
		"bounds": func(r *cp.Run) { r.SessionReadiness.LatestArchiveTask.Attempt = 6 },
		"private error": func(r *cp.Run) {
			r.SessionReadiness.LatestArchiveTask.SafeErrorCode = "private sentinel password=secret"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := proto.Clone(sessionReadinessWireFixture()).(*cp.Run)
			mutate(r)
			value, err := messageMap(&cp.GetRunResponse{Run: r})
			if err == nil {
				t.Fatal("invalid session proof accepted")
			}
			raw, _ := json.Marshal(value)
			if strings.Contains(err.Error(), "private sentinel") || strings.Contains(string(raw), "password") {
				t.Fatal("private diagnostic leaked")
			}
		})
	}
}

func TestRunSessionReadinessPurgedRemainsBlocked(t *testing.T) {
	t.Parallel()
	run := sessionReadinessWireFixture()
	run.SessionReadiness.StorageState = cp.RunSessionStorageState_RUN_SESSION_STORAGE_STATE_PURGED
	value, err := messageMap(&cp.GetRunResponse{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	read := value["run"].(map[string]any)["sessionReadiness"].(map[string]any)
	if read["storageState"] != "PURGED" || read["reason"] != "STORAGE_NOT_LIVE" {
		t.Fatal("purged session incorrectly reported as ready")
	}
}

func TestRunSessionReadinessEnumsPinned(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		count  int
		actual int
	}{
		{11, cp.RunSessionStorageState(0).Descriptor().Values().Len()},
		{5, cp.RunSessionReadinessReason(0).Descriptor().Values().Len()},
		{6, cp.RunSessionArchiveTaskState(0).Descriptor().Values().Len()},
		{5, cp.SessionArchiveTaskKind(0).Descriptor().Values().Len()},
	} {
		if item.count != item.actual {
			t.Fatal("session diagnostic enum cardinality changed")
		}
	}
	read := map[string]any{"sessionRef": "ses_fixture01", "storageState": "LIVE", "reason": "NO_SESSION_BLOCKER", "privatePayload": "private sentinel"}
	if err := validateRunSessionReadinessShape(read, (&cp.RunSessionReadiness{}).ProtoReflect().Descriptor()); err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("unknown private diagnostic field accepted or leaked")
	}
}

func TestRunSessionReadinessLocalizationKeepsClosedWire(t *testing.T) {
	t.Parallel()
	fixture := sessionReadinessWireFixture()
	fixture.SafeErrorCode = "PROVIDER_RESPONSE_INVALID"
	value, err := messageMap(&cp.GetRunResponse{Run: fixture})
	if err != nil {
		t.Fatal(err)
	}
	LocalizeSafeErrors(value, func(string) string { return "Безопасное сообщение об ошибке" })
	run := value["run"].(map[string]any)
	read := run["sessionReadiness"].(map[string]any)
	task := read["latestArchiveTask"].(map[string]any)
	if len(task) != 6 || task["safeErrorCode"] != "SESSION_ARCHIVE_SOURCE_INVALID" {
		t.Fatal("code-only archive task shape changed during localization")
	}
	if _, present := task["safeErrorMessage"]; present {
		t.Fatal("undeclared archive task error message injected")
	}
	if err := validateRunSessionReadinessShape(task, (&cp.RunSessionArchiveTask{}).ProtoReflect().Descriptor()); err != nil {
		t.Fatal(err)
	}
	if run["safeErrorMessage"] != "Безопасное сообщение об ошибке" {
		t.Fatal("ordinary Run error localization suppressed")
	}
}
