package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

func TestSnapshotRestoreAndDeleteExactArchive(t *testing.T) {
	workspace := t.TempDir()
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
	task := model.Task{TaskRef: "sat_abcdefgh", Kind: "SNAPSHOT", OrganizationRef: "org_abcdefgh", ProjectRef: "prj_abcdefgh",
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
	deletion := task
	deletion.Kind = "DELETE_OBJECT"
	deletion.TargetObjectVersion = result.ObjectVersion
	if _, err := Delete(context.Background(), store, deletion); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
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
