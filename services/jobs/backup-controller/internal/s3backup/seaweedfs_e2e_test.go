package s3backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

func TestSeaweedFSBackupRestoreDrillReadbackE2E(t *testing.T) {
	if os.Getenv("BACKUP_RESTORE_E2E") != "1" {
		t.Skip("local backup restore E2E is disabled")
	}
	endpoint := requireBackupLoopbackEndpoint(t, os.Getenv("BACKUP_RESTORE_E2E_ENDPOINT"))
	accessKey := readBackupE2ESecret(t, os.Getenv("BACKUP_RESTORE_E2E_ACCESS_KEY_FILE"))
	secretKey := readBackupE2ESecret(t, os.Getenv("BACKUP_RESTORE_E2E_SECRET_KEY_FILE"))
	backupID := strings.TrimSpace(os.Getenv("BACKUP_RESTORE_E2E_BACKUP_ID"))
	restoreID := strings.TrimSpace(os.Getenv("BACKUP_RESTORE_E2E_RESTORE_ID"))
	targetPrefix := strings.Trim(strings.TrimSpace(os.Getenv("BACKUP_RESTORE_E2E_TARGET_PREFIX")), "/")
	if backupID == "" || restoreID == "" || strings.ContainsAny(backupID+restoreID, "\r\n/") {
		t.Fatal("backup or restore identifier is invalid")
	}
	if !strings.HasPrefix(targetPrefix, "e2e-restore-") || strings.Contains(targetPrefix, "..") {
		t.Fatal("disposable restore target prefix is invalid")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := NewClient(ctx, configspec.S3{
		Name: "backup-repository", Endpoint: endpoint, Region: "us-east-1",
		Bucket: "kodex-backups", AccessKeyID: accessKey, SecretAccessKey: secretKey,
		UsePathStyle: true, AllowInsecureLocal: true,
	})
	if err != nil {
		t.Fatal("initialize local backup repository")
	}
	repository, err := NewRepository(client, "kodex")
	if err != nil || repository.Check(ctx, nil) != nil {
		t.Fatal("local backup repository is unavailable")
	}
	backup, _, err := repository.LoadVerifiedManifest(ctx, backupID)
	if err != nil {
		t.Fatalf("verified backup readback failed: %v", err)
	}
	drill, err := repository.LoadRestoreDrill(
		ctx,
		path.Join("backups", backupID, "restore-drills", restoreID+".json"),
		backup,
	)
	if err != nil || drill.BackupID != backupID || drill.RestoreID != restoreID {
		t.Fatal("immutable restore drill receipt readback failed")
	}
	expectedObjectKey := strings.TrimSpace(os.Getenv("BACKUP_RESTORE_E2E_EXPECTED_OBJECT_KEY"))
	expectedObjectFile := strings.TrimSpace(os.Getenv("BACKUP_RESTORE_E2E_EXPECTED_OBJECT_FILE"))
	if (expectedObjectKey == "") != (expectedObjectFile == "") {
		t.Fatal("expected restore object input is incomplete")
	}
	var expectedObject []byte
	if expectedObjectKey != "" {
		if !canonicalSessionArchiveKey.MatchString(expectedObjectKey) {
			t.Fatal("expected session archive object key is not canonical")
		}
		expectedObject = readPrivateE2EFile(t, expectedObjectFile, 4<<20)
		source, exists := findPlatformObject(backup, "session-archives", expectedObjectKey)
		if !exists {
			t.Fatal("expected session archive object is absent from backup manifest")
		}
		digest := sha256.Sum256(expectedObject)
		if source.Source.ChecksumSHA256 != "sha256:"+hex.EncodeToString(digest[:]) ||
			source.Source.SizeBytes != int64(len(expectedObject)) {
			t.Fatal("expected session archive manifest receipt mismatch")
		}
	}
	target, err := NewClient(ctx, configspec.S3{
		Name: "restore-fixture", Endpoint: endpoint, Region: "us-east-1",
		Bucket: "kodex-restore-fixture", Prefix: targetPrefix,
		AccessKeyID: accessKey, SecretAccessKey: secretKey,
		UsePathStyle: true, AllowInsecureLocal: true,
	})
	if err != nil || target.Check(ctx) != nil {
		t.Fatal("disposable restore object target is unavailable")
	}
	versions, err := target.listVersions(ctx, targetPrefix)
	if err != nil || len(versions) != len(drill.Objects) {
		t.Fatal("disposable restore object readback does not match receipt")
	}
	if expectedObjectKey != "" {
		targetKey := targetPrefix + "/" + expectedObjectKey
		var restored manifest.Receipt
		for _, version := range versions {
			if version.Key == targetKey {
				restored, err = target.head(ctx, version.Key, version.VersionID)
				break
			}
		}
		if err != nil || !restored.Valid() {
			t.Fatal("restored session archive receipt is unavailable")
		}
		readback, readErr := target.readBytes(ctx, restored, int64(len(expectedObject))+1)
		if readErr != nil || !bytes.Equal(readback, expectedObject) {
			t.Fatal("restored session archive exact-byte readback failed")
		}
	}
	for _, version := range versions {
		if _, err := target.api.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String("kodex-restore-fixture"), Key: aws.String(version.Key),
			VersionId: aws.String(version.VersionID),
		}); err != nil {
			t.Fatal("cleanup disposable restore object version")
		}
	}
	if remaining, err := target.listVersions(ctx, targetPrefix); err != nil || len(remaining) != 0 {
		t.Fatal("disposable restore object cleanup readback failed")
	}
}

func requireBackupLoopbackEndpoint(t *testing.T, raw string) string {
	t.Helper()
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") ||
		(endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost") || endpoint.Port() == "" {
		t.Fatal("backup E2E endpoint must be loopback-only HTTP")
	}
	return raw
}

func readBackupE2ESecret(t *testing.T, secretPath string) string {
	t.Helper()
	info, err := os.Lstat(secretPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		!filepath.IsAbs(secretPath) || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("backup E2E credential file is invalid")
	}
	raw, err := os.ReadFile(secretPath)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		t.Fatal("backup E2E credential cannot be read")
	}
	value := strings.TrimSpace(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	if value == "" || strings.ContainsAny(value, "\r\n") {
		t.Fatal(errors.New("backup E2E credential is invalid"))
	}
	return value
}
