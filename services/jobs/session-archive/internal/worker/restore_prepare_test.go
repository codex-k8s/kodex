package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

func restorePreparationTask(t *testing.T) model.Task {
	t.Helper()
	pvc, err := runtimecontract.SessionPVCName("ses_fixture01")
	if err != nil {
		t.Fatal(err)
	}
	path := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000001.jsonl"
	digest := strings.Repeat("a", 64)
	return model.Task{TaskRef: "sat_fixture01", Kind: "RESTORE", OrganizationRef: "org_fixture01",
		SessionRef: "ses_fixture01", ProviderAccountRef: "pacc_fixture01", RuntimeRevisionRef: "rrev_fixture01",
		RuntimeRevisionVersion: 1, RuntimeRevisionDigest: digest, ContentGeneration: 1, InputDigest: digest,
		CodexSessionID: "00000000-0000-4000-8000-000000000001", PVCName: pvc, Attempt: 1,
		SourceRelativePath: path, SourceSHA256: digest, SourceSizeBytes: 1,
		Archive: &model.ArchiveBinding{ArchiveRef: "sar_fixture01", FormatVersion: 1, ObjectKey: "archive-fixture",
			ObjectVersion: "fixture-v1", ObjectETag: "fixture-etag", ObjectDigest: "sha256:" + digest,
			ObjectSizeBytes: 1, SourceRelativePath: path, SourceSHA256: digest, SourceSizeBytes: 1}}
}

func TestRestorePreparationAcceptsOnlyExactRestoreBinding(t *testing.T) {
	if validateRestorePreparationTask(restorePreparationTask(t)) != nil {
		t.Fatal("valid restore preparation binding rejected")
	}
	for _, mutate := range []func(*model.Task){
		func(task *model.Task) { task.Kind = "SNAPSHOT"; task.Archive = nil; task.TargetObjectKey = "fixture" },
		func(task *model.Task) { task.Kind = "DELETE_PVC" },
		func(task *model.Task) { task.Kind = "DELETE_OBJECT"; task.TargetObjectKey = "fixture" },
		func(task *model.Task) { task.Kind = "UNKNOWN" },
		func(task *model.Task) { task.PVCName = "foreign" },
		func(task *model.Task) { task.Archive.SourceRelativePath = "../foreign" },
		func(task *model.Task) { task.Archive.SourceSHA256 = strings.Repeat("b", 64) },
		func(task *model.Task) { task.Archive.SourceSizeBytes++ },
	} {
		task := restorePreparationTask(t)
		mutate(&task)
		if validateRestorePreparationTask(task) == nil {
			t.Fatal("non-restore or mismatched task was accepted")
		}
	}
}

// Вызывается только отдельной kernel-fixture: production CLI и fixed paths те же.
func TestPrepareRestoredHomeKernelFixture(t *testing.T) {
	if os.Getenv("KODEX_TEST_FULL_RESTORE_ABI") != "1" {
		return
	}
	if os.Geteuid() != 10001 || filepath.Clean(restoreTaskFile) != restoreTaskFile {
		t.Fatal("restore preparer fixture identity is invalid")
	}
	err := PrepareRestore(t.Context())
	if os.Getenv("KODEX_TEST_RESTORE_PREPARE_EXPECT_FAILURE") == "1" {
		if err == nil {
			t.Fatal("unsafe preparation was accepted")
		}
		return
	}
	if err != nil {
		t.Fatal("canonical non-root restore preparation failed")
	}
	if err := PrepareRestore(t.Context()); err != nil {
		t.Fatal("canonical restore preparation is not idempotent")
	}
}
