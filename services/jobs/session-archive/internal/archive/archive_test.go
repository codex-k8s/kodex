package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

func TestSnapshotRestoreAndDeleteExactArchive(t *testing.T) {
	t.Run("PROJECT", func(t *testing.T) { snapshotRestoreAndDeleteExactArchive(t, "prj_abcdefgh") })
	t.Run("SYSTEM", func(t *testing.T) { snapshotRestoreAndDeleteExactArchive(t, "") })
}

func snapshotRestoreAndDeleteExactArchive(t *testing.T, projectRef string) {
	t.Helper()
	snapshotRestoreAndDeleteExactArchiveAtWorkspace(t, t.TempDir(), projectRef)
}

func snapshotRestoreAndDeleteExactArchiveAtWorkspace(t *testing.T, workspace, projectRef string) {
	t.Helper()
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000001.jsonl"
	body := []byte("{\"type\":\"session_meta\"}\n")
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	task := model.Task{TaskRef: "sat_abcdefgh", Kind: "SNAPSHOT", OrganizationRef: "org_abcdefgh", ProjectRef: projectRef,
		SessionRef: "ses_abcdefgh", ProviderAccountRef: "pacc_abcdefgh", RuntimeRevisionRef: "rrev_abcdefgh",
		RuntimeRevisionVersion: 1, RuntimeRevisionDigest: stringDigest('a'), CodexSessionID: "00000000-0000-4000-8000-000000000001",
		ContentGeneration: 1, PVCName: "runtime-session-0123456789abcdef", InputDigest: stringDigest('b'),
		SourceRelativePath: relative, SourceSHA256: hex.EncodeToString(hash[:]), SourceSizeBytes: int64(len(body)),
		TargetObjectKey: "session-archive/v1/org_abcdefgh/prj_abcdefgh/ses_abcdefgh/g1/sat_abcdefgh-a1.tar", Attempt: 1}
	store := objectstoragetest.New()
	result, err := Snapshot(context.Background(), store, workspace, task)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if err := os.RemoveAll(filepath.Join(workspace, ".kodex")); err != nil {
		t.Fatal(err)
	}
	restore := task
	restore.Kind = "RESTORE"
	restore.TargetObjectKey = ""
	restore.Archive = &model.ArchiveBinding{ArchiveRef: "sar_abcdefgh",
		FormatVersion: result.FormatVersion, ObjectKey: result.ObjectKey, ObjectVersion: result.ObjectVersion, ObjectETag: result.ObjectETag,
		ObjectDigest: result.ObjectDigest, ObjectSizeBytes: result.ObjectSizeBytes, SourceRelativePath: relative,
		SourceSHA256: task.SourceSHA256, SourceSizeBytes: task.SourceSizeBytes}
	if _, err := Restore(context.Background(), store, workspace, restore); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != string(body) {
		t.Fatalf("restored source = %q, err=%v", restored, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("restored source lost its restricted mode")
	}
	// Следующий snapshot должен принимать точно восстановленные байты без
	// изменения сохранённого owner/runtime binding.
	if repeated, err := Snapshot(context.Background(), store, workspace, task); err != nil ||
		repeated.SourceSHA256 != task.SourceSHA256 || repeated.SourceSizeBytes != task.SourceSizeBytes {
		t.Fatalf("snapshot after restore failed: %v", err)
	}
	deletion := task
	deletion.Kind = "DELETE_OBJECT"
	deletion.TargetObjectVersion = result.ObjectVersion
	if _, err := Delete(context.Background(), store, deletion); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

// Отдельная kernel-проверка запускается только root в изолированном /tmp.
// Обычные unit-тесты не получают дополнительных UID либо capabilities.
func TestRestoreThenRunnerCaptureAcrossUIDs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires an explicit isolated root fixture")
	}
	runnerBinary := os.Getenv("KODEX_TEST_RUNNER_CAPTURE_BINARY")
	info, err := os.Lstat(runnerBinary)
	if err != nil || !filepath.IsAbs(runnerBinary) || filepath.Base(runnerBinary) != "runner-capture.test" || !info.Mode().IsRegular() {
		t.Fatal("isolated runner fixture binary is required")
	}
	archiveBinary, err := os.Executable()
	if err != nil {
		t.Fatal("isolated archive fixture binary is unavailable")
	}
	for _, scenario := range []struct {
		name string
		uid  uint32
	}{{"OLD_RESTORE_OWNER", 10002}, {"RUNNER_RESTORE_OWNER", 10001}} {
		t.Run(scenario.name, func(t *testing.T) {
			workspace, err := os.MkdirTemp("", "kodex-session-restore-")
			if err != nil {
				t.Fatal("create isolated restore workspace")
			}
			t.Cleanup(func() { _ = os.RemoveAll(workspace) })
			relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000001.jsonl"
			if err := os.MkdirAll(filepath.Dir(filepath.Join(workspace, relative)), 0o750); err != nil {
				t.Fatal("create isolated restore directories")
			}
			for path := filepath.Dir(filepath.Join(workspace, relative)); path != filepath.Dir(workspace); path = filepath.Dir(path) {
				if os.Chown(path, 0, 29000) != nil || os.Chmod(path, 0o770|os.ModeSetgid) != nil {
					t.Fatal("prepare isolated shared volume group")
				}
			}
			run := func(binary, test string, uid uint32, mode string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, binary, "-test.run", "^"+test+"$")
				command.Env = []string{"KODEX_TEST_RESTORE_WORKSPACE=" + workspace, "KODEX_TEST_RESTORE_MODE=" + mode}
				command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{29000}}}
				if err := command.Run(); err != nil {
					t.Fatal("isolated restore/capture fixture failed")
				}
			}
			run(archiveBinary, "TestRestoreSourceOwnershipFixture", scenario.uid, "RESTORE")
			stat, err := os.Stat(filepath.Join(workspace, relative))
			if err != nil {
				t.Fatal("restored source is missing")
			}
			owner := stat.Sys().(*syscall.Stat_t)
			if owner.Uid != scenario.uid || owner.Gid != 29000 || stat.Mode().Perm() != 0o640 {
				t.Fatal("restored source owner/group/mode differs from canonical worker identity")
			}
			run(runnerBinary, "TestRestoredRolloutCaptureFixture", 10001, scenario.name)
		})
	}
}

func TestRestoreSourceOwnershipFixture(t *testing.T) {
	if os.Getenv("KODEX_TEST_RESTORE_MODE") != "RESTORE" {
		return
	}
	workspace := os.Getenv("KODEX_TEST_RESTORE_WORKSPACE")
	if filepath.Dir(workspace) != os.TempDir() || !strings.HasPrefix(filepath.Base(workspace), "kodex-session-restore-") ||
		(os.Geteuid() != 10001 && os.Geteuid() != 10002) {
		t.Fatal("isolated restore fixture binding is invalid")
	}
	snapshotRestoreAndDeleteExactArchiveAtWorkspace(t, workspace, "")
}

func TestRestoreRejectsReceiptMismatchBeforeWriting(t *testing.T) {
	workspace := t.TempDir()
	task := model.Task{Kind: "RESTORE", SourceSHA256: stringDigest('a'), SourceSizeBytes: 1,
		Archive: &model.ArchiveBinding{ObjectKey: "session-archive/v1/test.tar", ObjectVersion: "memory-v1", ObjectETag: "wrong", ObjectDigest: "sha256:" + stringDigest('b'), ObjectSizeBytes: 1}}
	if _, err := Restore(context.Background(), objectstoragetest.New(), workspace, task); err == nil {
		t.Fatal("Restore() accepted a missing or mismatched receipt")
	}
}

func TestSnapshotCleansUpObjectAfterReadbackMismatch(t *testing.T) {
	workspace := t.TempDir()
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000004.jsonl"
	body := []byte("{\"type\":\"session_meta\"}\n")
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	task := model.Task{SourceRelativePath: relative, SourceSHA256: hex.EncodeToString(hash[:]),
		SourceSizeBytes: int64(len(body)), TargetObjectKey: "session-archive/v1/readback-mismatch.tar"}
	store := &readbackMismatchStore{Store: objectstoragetest.New()}
	if _, err := Snapshot(context.Background(), store, workspace, task); err == nil {
		t.Fatal("Snapshot() accepted a readback mismatch")
	}
	if _, err := store.Head(context.Background(), task.TargetObjectKey, ""); err != objectstorage.ErrNotFound {
		t.Fatalf("partial archive object was not deleted: %v", err)
	}
}

func TestSnapshotCleansUpObjectWhenPutReadbackFails(t *testing.T) {
	workspace := t.TempDir()
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000005.jsonl"
	body := []byte("{\"type\":\"session_meta\"}\n")
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	task := model.Task{SourceRelativePath: relative, SourceSHA256: hex.EncodeToString(hash[:]),
		SourceSizeBytes: int64(len(body)), TargetObjectKey: "session-archive/v1/put-readback-failure.tar"}
	store := &putReadbackFailureStore{Store: objectstoragetest.New()}
	if _, err := Snapshot(context.Background(), store, workspace, task); err == nil {
		t.Fatal("Snapshot() accepted a failed Put readback")
	}
	if _, err := store.Head(context.Background(), task.TargetObjectKey, ""); err != objectstorage.ErrNotFound {
		t.Fatalf("object left by failed Put readback was not deleted: %v", err)
	}
}

type readbackMismatchStore struct{ *objectstoragetest.Store }

func (store *readbackMismatchStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	object.ETag = "mismatched-etag"
	return object, err
}

type putReadbackFailureStore struct{ *objectstoragetest.Store }

func (store *putReadbackFailureStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	if _, err := store.Store.Put(ctx, input); err != nil {
		return objectstorage.Receipt{}, err
	}
	return objectstorage.Receipt{}, objectstorage.ErrConflict
}

func stringDigest(character byte) string {
	raw := make([]byte, 64)
	for index := range raw {
		raw[index] = character
	}
	return string(raw)
}
