package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

// fullRestoreTaskFile is the fixed task mount used only by the disposable full-restore kernel fixture.
// RU: fullRestoreTaskFile задаёт фиксированный путь задания только для одноразовой kernel-оснастки полного восстановления.
const fullRestoreTaskFile = "/var/run/config/kodex/session-archive/task.json"

// fullRestoreSnapshotTask creates consistent session, PVC and source pins for native-writer restore tests.
// RU: fullRestoreSnapshotTask создаёт согласованные привязки сессии, PVC и источника для проверки восстановления native writer.
func fullRestoreSnapshotTask(t *testing.T) model.Task {
	t.Helper()
	pvc, err := runtimecontract.SessionPVCName("ses_abcdefgh")
	if err != nil {
		t.Fatal("fixture PVC binding is invalid")
	}
	body := []byte("{\"type\":\"session_meta\"}\n")
	hash := sha256.Sum256(body)
	return model.Task{TaskRef: "sat_abcdefgh", Kind: "SNAPSHOT", OrganizationRef: "org_abcdefgh",
		SessionRef: "ses_abcdefgh", ProviderAccountRef: "pacc_abcdefgh", RuntimeRevisionRef: "rrev_abcdefgh",
		RuntimeRevisionVersion: 1, RuntimeRevisionDigest: stringDigest('a'), ContentGeneration: 1,
		CodexSessionID: "00000000-0000-4000-8000-000000000001", PVCName: pvc, InputDigest: stringDigest('b'),
		SourceRelativePath: ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000001.jsonl",
		SourceSHA256:       hex.EncodeToString(hash[:]), SourceSizeBytes: int64(len(body)),
		TargetObjectKey: "session-archive/v1/org_abcdefgh/ses_abcdefgh/g1/sat_abcdefgh-a1.tar", Attempt: 1}
}

// fullRestoreFixtureSnapshot archives synthetic session bytes in an isolated in-memory store and returns the verified receipt.
// RU: fullRestoreFixtureSnapshot архивирует синтетические байты сессии в изолированное хранилище памяти и возвращает проверенную квитанцию.
func fullRestoreFixtureSnapshot(t *testing.T) (*objectstoragetest.Store, model.Result) {
	t.Helper()
	task := fullRestoreSnapshotTask(t)
	root := t.TempDir()
	path := filepath.Join(root, task.SourceRelativePath)
	if os.MkdirAll(filepath.Dir(path), 0o750) != nil || os.WriteFile(path, []byte("{\"type\":\"session_meta\"}\n"), 0o640) != nil {
		t.Fatal("create bounded synthetic snapshot source")
	}
	store := objectstoragetest.New()
	result, err := Snapshot(t.Context(), store, root, task)
	if err != nil {
		t.Fatal("create verified synthetic archive receipt")
	}
	return store, result
}

// TestPreparedRestoreThenCurrentWorkspaceAndNativeCapture runs the full UID/volume ABI only in an explicitly enabled disposable kernel container.
// RU: TestPreparedRestoreThenCurrentWorkspaceAndNativeCapture проверяет полный ABI UID/тома лишь в явно включённом одноразовом kernel-контейнере.
func TestPreparedRestoreThenCurrentWorkspaceAndNativeCapture(t *testing.T) {
	if os.Getenv("KODEX_TEST_FULL_RESTORE_ABI") != "1" {
		return
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated full ABI driver identity is invalid")
	}
	runnerBinary := os.Getenv("KODEX_TEST_RUNNER_CAPTURE_BINARY")
	prepareBinary := os.Getenv("KODEX_TEST_RESTORE_PREPARE_BINARY")
	for path, expected := range map[string]string{runnerBinary: "runner-capture.test", prepareBinary: "restore-prepare.test"} {
		info, err := os.Lstat(path)
		if err != nil || !filepath.IsAbs(path) || filepath.Base(path) != expected || !info.Mode().IsRegular() {
			t.Fatal("isolated full ABI fixture executable is invalid")
		}
	}
	archiveBinary, err := os.Executable()
	if err != nil {
		t.Fatal("isolated archive executable is unavailable")
	}
	run := func(binary, test, stage string, uid uint32, failure bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run", "^"+test+"$")
		command.Env = []string{"KODEX_TEST_FULL_RESTORE_ABI=1", "KODEX_TEST_RESTORE_WORKSPACE=/workspace", "KODEX_TEST_RESTORE_MODE=" + stage}
		if failure {
			command.Env = append(command.Env, "KODEX_TEST_RESTORE_PREPARE_EXPECT_FAILURE=1")
		}
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{29000}}}
		if command.Run() != nil {
			t.Fatalf("isolated full RESTORE ABI stage failed: %s", stage)
		}
	}
	if os.Chown("/workspace", 0, 29000) != nil || os.Chmod("/workspace", 0o2770) != nil {
		t.Fatal("prepare isolated workspace volume root")
	}
	run(runnerBinary, "TestRestoredWorkspaceGuardKernelFixture", "PARENT_PREPARE", 10001, false)
	state := "/workspace/.kodex/state"
	if os.Mkdir(state, 0o770) != nil || os.Chown(state, 0, 29000) != nil || os.Chmod(state, 0o2770) != nil {
		t.Fatal("prepare isolated kubelet state volume root")
	}
	_, receipt := fullRestoreFixtureSnapshot(t)
	task := fullRestoreSnapshotTask(t)
	task.Kind, task.TargetObjectKey = "RESTORE", ""
	task.Archive = &model.ArchiveBinding{ArchiveRef: "sar_abcdefgh", FormatVersion: receipt.FormatVersion,
		ObjectKey: receipt.ObjectKey, ObjectVersion: receipt.ObjectVersion, ObjectETag: receipt.ObjectETag,
		ObjectDigest: receipt.ObjectDigest, ObjectSizeBytes: receipt.ObjectSizeBytes,
		SourceRelativePath: task.SourceRelativePath, SourceSHA256: task.SourceSHA256, SourceSizeBytes: task.SourceSizeBytes}
	raw, err := json.Marshal(task)
	if err != nil || os.WriteFile(fullRestoreTaskFile, raw, 0o444) != nil {
		t.Fatal("write exact synthetic immutable restore task")
	}
	run(prepareBinary, "TestPrepareRestoredHomeKernelFixture", "DIRECTORY_PREPARE", 10001, false)
	home := filepath.Join(state, "codex-home")
	if os.Chown(home, 10002, 29000) != nil {
		t.Fatal("prepare isolated wrong-owner negative")
	}
	run(prepareBinary, "TestPrepareRestoredHomeKernelFixture", "WRONG_OWNER", 10001, true)
	if os.Chown(home, 10001, 29000) != nil || os.Remove(home) != nil || os.Symlink("/tmp", home) != nil {
		t.Fatal("prepare isolated symlink negative")
	}
	run(prepareBinary, "TestPrepareRestoredHomeKernelFixture", "SYMLINK", 10001, true)
	if os.Remove(home) != nil {
		t.Fatal("clear isolated negative symlink")
	}
	run(prepareBinary, "TestPrepareRestoredHomeKernelFixture", "DIRECTORY_PREPARE", 10001, false)
	run(archiveBinary, "TestFullPreparedRestoreKernelFixture", "RESTORE", 10002, false)
	run(runnerBinary, "TestRestoredWorkspaceGuardKernelFixture", "WORKSPACE_GUARD", 10001, false)
	run(runnerBinary, "TestRestoredRolloutCaptureFixture", "FULL_NATIVE_WRITER", 10002, false)
}

// TestFullPreparedRestoreKernelFixture restores the pinned task as the native writer UID in the dedicated full-ABI stage.
// RU: TestFullPreparedRestoreKernelFixture восстанавливает закреплённое задание с UID native writer на выделенном этапе полного ABI.
func TestFullPreparedRestoreKernelFixture(t *testing.T) {
	if os.Getenv("KODEX_TEST_FULL_RESTORE_ABI") != "1" || os.Getenv("KODEX_TEST_RESTORE_MODE") != "RESTORE" {
		return
	}
	if os.Geteuid() != 10002 || os.Getenv("KODEX_TEST_RESTORE_WORKSPACE") != "/workspace" {
		t.Fatal("isolated native restore identity is invalid")
	}
	task, err := model.DecodeFile(fullRestoreTaskFile)
	if err != nil {
		t.Fatal("read exact synthetic restore task")
	}
	store, _ := fullRestoreFixtureSnapshot(t)
	if _, err := Restore(t.Context(), store, "/workspace", task); err != nil {
		t.Fatal("verified native archive restore failed")
	}
}

// TestSnapshotRestoreAndDeleteExactArchive exercises the same exact-byte lifecycle for project and system scope.
// RU: TestSnapshotRestoreAndDeleteExactArchive проверяет один жизненный цикл точных байтов в проектном и системном scope.
func TestSnapshotRestoreAndDeleteExactArchive(t *testing.T) {
	t.Run("PROJECT", func(t *testing.T) { snapshotRestoreAndDeleteExactArchive(t, "prj_abcdefgh") })
	t.Run("SYSTEM", func(t *testing.T) { snapshotRestoreAndDeleteExactArchive(t, "") })
}

// snapshotRestoreAndDeleteExactArchive allocates a private workspace for one scoped lifecycle fixture.
// RU: snapshotRestoreAndDeleteExactArchive выделяет закрытый workspace для оснастки жизненного цикла одного scope.
func snapshotRestoreAndDeleteExactArchive(t *testing.T, projectRef string) {
	t.Helper()
	snapshotRestoreAndDeleteExactArchiveAtWorkspace(t, t.TempDir(), projectRef)
}

// snapshotRestoreAndDeleteExactArchiveAtWorkspace verifies restored bytes and mode, a repeated snapshot and exact-version deletion.
// RU: snapshotRestoreAndDeleteExactArchiveAtWorkspace проверяет восстановленные байты и режим, повторный снимок и удаление точной версии.
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

// TestRestoreThenRunnerCaptureAcrossUIDs compares restored-file ownership across UIDs only in an isolated root fixture.
// RU: TestRestoreThenRunnerCaptureAcrossUIDs сравнивает владельцев восстановленного файла между UID лишь в изолированной root-оснастке.
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
	}{{"WRONG_RESTORE_OWNER", 10001}, {"NATIVE_WRITER_RESTORE_OWNER", 10002}} {
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
			run(runnerBinary, "TestRestoredRolloutCaptureFixture", 10002, scenario.name)
		})
	}
}

// TestRestoreSourceOwnershipFixture accepts only the bound disposable workspace and one of the two fixture UIDs.
// RU: TestRestoreSourceOwnershipFixture допускает лишь закреплённый одноразовый workspace и один из двух UID оснастки.
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

// TestRestoreRejectsReceiptMismatchBeforeWriting prevents source writes from a missing or mismatched object receipt.
// RU: TestRestoreRejectsReceiptMismatchBeforeWriting запрещает запись источника при отсутствующей либо чужой квитанции объекта.
func TestRestoreRejectsReceiptMismatchBeforeWriting(t *testing.T) {
	workspace := t.TempDir()
	task := model.Task{Kind: "RESTORE", SourceSHA256: stringDigest('a'), SourceSizeBytes: 1,
		Archive: &model.ArchiveBinding{ObjectKey: "session-archive/v1/test.tar", ObjectVersion: "memory-v1", ObjectETag: "wrong", ObjectDigest: "sha256:" + stringDigest('b'), ObjectSizeBytes: 1}}
	if _, err := Restore(context.Background(), objectstoragetest.New(), workspace, task); err == nil {
		t.Fatal("Restore() accepted a missing or mismatched receipt")
	}
}

// TestSnapshotCleansUpObjectAfterReadbackMismatch requires cleanup when stored-object ETag readback disagrees.
// RU: TestSnapshotCleansUpObjectAfterReadbackMismatch требует очистки при несовпадении перечитанного ETag объекта.
func TestSnapshotCleansUpObjectAfterReadbackMismatch(t *testing.T) {
	workspace := t.TempDir()
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000004.jsonl"
	task := snapshotSourceTask(t, workspace, relative, "session-archive/v1/readback-mismatch.tar")
	store := &readbackMismatchStore{Store: objectstoragetest.New()}
	if _, err := Snapshot(context.Background(), store, workspace, task); err == nil {
		t.Fatal("Snapshot() accepted a readback mismatch")
	}
	if _, err := store.Head(context.Background(), task.TargetObjectKey, ""); err != objectstorage.ErrNotFound {
		t.Fatalf("partial archive object was not deleted: %v", err)
	}
}

// TestSnapshotCleansUpObjectWhenPutReadbackFails removes bytes left by a failed upload receipt path.
// RU: TestSnapshotCleansUpObjectWhenPutReadbackFails удаляет байты, оставшиеся после отказа выдачи квитанции загрузки.
func TestSnapshotCleansUpObjectWhenPutReadbackFails(t *testing.T) {
	workspace := t.TempDir()
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000005.jsonl"
	task := snapshotSourceTask(t, workspace, relative, "session-archive/v1/put-readback-failure.tar")
	store := &putReadbackFailureStore{Store: objectstoragetest.New()}
	if _, err := Snapshot(context.Background(), store, workspace, task); err == nil {
		t.Fatal("Snapshot() accepted a failed Put readback")
	}
	if _, err := store.Head(context.Background(), task.TargetObjectKey, ""); err != objectstorage.ErrNotFound {
		t.Fatalf("object left by failed Put readback was not deleted: %v", err)
	}
}

// snapshotSourceTask writes exact synthetic source bytes and binds their size and digest to the caller's object key.
// RU: snapshotSourceTask записывает точные синтетические байты источника и связывает их размер и дайджест с ключом объекта вызывающего теста.
func snapshotSourceTask(t *testing.T, workspace, relative, objectKey string) model.Task {
	t.Helper()
	body := []byte("{\"type\":\"session_meta\"}\n")
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	return model.Task{SourceRelativePath: relative, SourceSHA256: hex.EncodeToString(hash[:]),
		SourceSizeBytes: int64(len(body)), TargetObjectKey: objectKey}
}

// readbackMismatchStore corrupts returned ETag metadata while retaining the stored object for cleanup assertions.
// RU: readbackMismatchStore искажает возвращённый ETag, сохраняя объект для проверки очистки.
type readbackMismatchStore struct{ *objectstoragetest.Store }

// Get delegates the read and injects a mismatched ETag to exercise receipt validation.
// RU: Get делегирует чтение и подставляет чужой ETag для проверки валидации квитанции.
func (store *readbackMismatchStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	object.ETag = "mismatched-etag"
	return object, err
}

// putReadbackFailureStore simulates an upload that stores bytes but cannot return a successful receipt.
// RU: putReadbackFailureStore имитирует загрузку с сохранёнными байтами без успешной квитанции.
type putReadbackFailureStore struct{ *objectstoragetest.Store }

// Put persists the object before returning a conflict to force the snapshot's unknown-result cleanup path.
// RU: Put сохраняет объект и возвращает конфликт, вынуждая snapshot выполнить очистку при неопределённом результате.
func (store *putReadbackFailureStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	if _, err := store.Store.Put(ctx, input); err != nil {
		return objectstorage.Receipt{}, err
	}
	return objectstorage.Receipt{}, objectstorage.ErrConflict
}

// stringDigest builds deterministic 64-byte digest-shaped fixture input from one character.
// RU: stringDigest создаёт детерминированное 64-байтовое значение в форме дайджеста из одного символа.
func stringDigest(character byte) string {
	raw := make([]byte, 64)
	for index := range raw {
		raw[index] = character
	}
	return string(raw)
}
