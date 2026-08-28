package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

func TestSeaweedFSSnapshotRestoreDeleteE2E(t *testing.T) {
	if os.Getenv("SESSION_ARCHIVE_E2E") != "1" {
		t.Skip("local SeaweedFS E2E is disabled")
	}
	endpoint := requireLoopbackEndpoint(t, os.Getenv("SESSION_ARCHIVE_E2E_ENDPOINT"))
	accessKey := readE2ESecret(t, os.Getenv("SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE"))
	secretKey := readE2ESecret(t, os.Getenv("SESSION_ARCHIVE_E2E_SECRET_KEY_FILE"))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store, err := s3store.New(ctx, s3store.Config{Endpoint: endpoint, Region: "us-east-1",
		Bucket: "kodex-session-archives", AccessKeyID: accessKey, SecretKey: secretKey, UsePathStyle: true})
	if err != nil || store.Check(ctx) != nil {
		t.Fatal("local SeaweedFS archive bucket is unavailable")
	}

	workspace := t.TempDir()
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000002.jsonl"
	body := []byte("{\"type\":\"session_meta\",\"id\":\"00000000-0000-4000-8000-000000000002\"}\n")
	path := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	sourceDigest := sha256.Sum256(body)
	objectSuffix := sha256.Sum256([]byte(t.Name() + time.Now().UTC().String()))
	task := model.Task{TaskRef: "sat_seaweedfs_e2e", Kind: "SNAPSHOT", OrganizationRef: "org_e2e00001",
		ProjectRef: "prj_e2e00001", SessionRef: "ses_e2e00001", ProviderAccountRef: "pacc_e2e0001",
		RuntimeRevisionRef: "rrev_e2e0001", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: stringDigest('c'),
		CodexSessionID: "00000000-0000-4000-8000-000000000002", ContentGeneration: 1,
		PVCName: "runtime-session-e2e0000000000000", InputDigest: stringDigest('d'), SourceRelativePath: relative,
		SourceSHA256: hex.EncodeToString(sourceDigest[:]), SourceSizeBytes: int64(len(body)), Attempt: 1,
		TargetObjectKey: "session-archive/e2e/" + hex.EncodeToString(objectSuffix[:]) + ".tar"}
	result, err := Snapshot(ctx, store, workspace, task)
	if err != nil {
		t.Fatalf("snapshot to local SeaweedFS: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.WithoutCancel(ctx), result.ObjectKey, result.ObjectVersion) })
	if err := os.RemoveAll(filepath.Join(workspace, ".kodex")); err != nil {
		t.Fatal(err)
	}
	restore := task
	restore.Kind = "RESTORE"
	restore.TargetObjectKey = ""
	restore.Archive = &model.ArchiveBinding{ArchiveRef: "sar_seaweedfs_e2e", FormatVersion: result.FormatVersion,
		ObjectKey: result.ObjectKey, ObjectVersion: result.ObjectVersion, ObjectETag: result.ObjectETag,
		ObjectDigest: result.ObjectDigest, ObjectSizeBytes: result.ObjectSizeBytes, SourceRelativePath: relative,
		SourceSHA256: task.SourceSHA256, SourceSizeBytes: task.SourceSizeBytes}
	if _, err := Restore(ctx, store, workspace, restore); err != nil {
		t.Fatalf("restore from local SeaweedFS: %v", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != string(body) {
		t.Fatalf("restored source mismatch: %v", err)
	}
	deletion := task
	deletion.Kind = "DELETE_OBJECT"
	deletion.TargetObjectVersion = result.ObjectVersion
	if _, err := Delete(ctx, store, deletion); err != nil {
		t.Fatalf("delete local SeaweedFS archive: %v", err)
	}
}

func requireLoopbackEndpoint(t *testing.T, raw string) string {
	t.Helper()
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost") || endpoint.Port() == "" {
		t.Fatal("E2E endpoint must be loopback-only HTTP")
	}
	return raw
}

func readE2ESecret(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !filepath.IsAbs(path) {
		t.Fatal("E2E credential file is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		t.Fatal("E2E credential cannot be read")
	}
	value := strings.TrimSpace(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	if value == "" || strings.ContainsAny(value, "\r\n") {
		t.Fatal(errors.New("E2E credential is invalid"))
	}
	return value
}
