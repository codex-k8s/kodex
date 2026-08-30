package s3backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

var canonicalSessionArchiveKey = regexp.MustCompile(
	`^session-archive/v1/org_[a-z0-9]+/prj_[a-z0-9]+/ses_[a-z0-9]+/g[1-9][0-9]*/sat_[a-z0-9]+-a[1-9][0-9]*\.tar$`,
)

func TestSeaweedFSSessionArchiveBackupFixtureE2E(t *testing.T) {
	if os.Getenv("BACKUP_SESSION_ARCHIVE_E2E") != "1" {
		t.Skip("local session archive backup E2E is disabled")
	}
	endpoint := requireBackupLoopbackEndpoint(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_ENDPOINT"))
	accessKey := readBackupE2ESecret(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_ACCESS_KEY_FILE"))
	secretKey := readBackupE2ESecret(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_SECRET_KEY_FILE"))
	objectKey := strings.TrimSpace(os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_OBJECT_KEY"))
	if !canonicalSessionArchiveKey.MatchString(objectKey) {
		t.Fatal("session archive fixture key is not canonical")
	}
	phase := strings.TrimSpace(os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_PHASE"))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	archive, err := NewClient(ctx, configspec.S3{
		Name: "session-archives", Endpoint: endpoint, Region: "us-east-1",
		Bucket: "kodex-session-archives", AccessKeyID: accessKey, SecretAccessKey: secretKey,
		UsePathStyle: true, AllowInsecureLocal: true,
	})
	if err != nil || archive.Check(ctx) != nil {
		t.Fatal("local session archive bucket is unavailable")
	}

	switch phase {
	case "prepare":
		payload := readPrivateE2EFile(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_FIXTURE_FILE"), 4<<20)
		putSessionArchiveFixture(t, ctx, archive, objectKey, payload, true)
	case "mutate":
		payload := readPrivateE2EFile(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_FIXTURE_FILE"), 4<<20)
		putSessionArchiveFixture(t, ctx, archive, objectKey, payload, false)
	case "find-backup":
		payload := readPrivateE2EFile(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_FIXTURE_FILE"), 4<<20)
		startedAt, err := time.Parse(time.RFC3339Nano,
			strings.TrimSpace(os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_STARTED_AT")))
		if err != nil {
			t.Fatal("backup start boundary is invalid")
		}
		backupID := findSessionArchiveBackup(t, ctx, endpoint, accessKey, secretKey,
			objectKey, payload, startedAt)
		writePrivateE2EResult(t, os.Getenv("BACKUP_SESSION_ARCHIVE_E2E_BACKUP_ID_FILE"), backupID+"\n")
	case "cleanup":
		deleteExactObjectVersions(t, ctx, archive, objectKey)
	default:
		t.Fatal("session archive backup E2E phase is invalid")
	}
}

func putSessionArchiveFixture(t *testing.T, ctx context.Context, client *Client, key string,
	payload []byte, requireAbsent bool) {
	t.Helper()
	digestValue := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(digestValue[:])
	input := &s3.PutObjectInput{
		Bucket: aws.String(client.config.Bucket), Key: aws.String(key), Body: bytes.NewReader(payload),
		ContentLength: aws.Int64(int64(len(payload))), ContentType: aws.String("application/x-tar"),
		ChecksumSHA256: aws.String(checksumBase64(digest)), Metadata: map[string]string{digestMetadataKey: digest},
	}
	if requireAbsent {
		input.IfNoneMatch = aws.String("*")
	}
	output, err := client.api.PutObject(ctx, input)
	if err != nil || strings.TrimSpace(aws.ToString(output.VersionId)) == "" {
		t.Fatal("write session archive backup fixture")
	}
	receipt, err := client.head(ctx, key, aws.ToString(output.VersionId))
	if err != nil || receipt.ChecksumSHA256 != digest || receipt.SizeBytes != int64(len(payload)) {
		t.Fatal("session archive backup fixture receipt mismatch")
	}
	readback, err := client.readBytes(ctx, receipt, int64(len(payload))+1)
	if err != nil || !bytes.Equal(readback, payload) {
		t.Fatal("session archive backup fixture readback mismatch")
	}
}

func findSessionArchiveBackup(t *testing.T, ctx context.Context, endpoint, accessKey, secretKey,
	objectKey string, expected []byte, startedAt time.Time) string {
	t.Helper()
	destination, err := NewClient(ctx, configspec.S3{
		Name: "backup-repository", Endpoint: endpoint, Region: "us-east-1", Bucket: "kodex-backups",
		AccessKeyID: accessKey, SecretAccessKey: secretKey, UsePathStyle: true, AllowInsecureLocal: true,
	})
	if err != nil {
		t.Fatal("initialize local backup repository")
	}
	repository, err := NewRepository(destination, "kodex")
	if err != nil || repository.Check(ctx, nil) != nil {
		t.Fatal("local backup repository is unavailable")
	}
	backups, _, err := repository.Catalog(ctx)
	if err != nil {
		t.Fatal("read verified backup catalog")
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].StartedAt.After(backups[j].StartedAt) })
	digestValue := sha256.Sum256(expected)
	expectedDigest := "sha256:" + hex.EncodeToString(digestValue[:])
	for _, backup := range backups {
		if backup.StartedAt.Before(startedAt) {
			continue
		}
		for _, object := range backup.PlatformObjects {
			if object.StoreName != "session-archives" || object.Source.Key != objectKey {
				continue
			}
			if object.Source.Bucket != "kodex-session-archives" ||
				object.Source.ChecksumSHA256 != expectedDigest || object.Source.SizeBytes != int64(len(expected)) {
				t.Fatal("session archive source receipt is not exact")
			}
			readback, err := repository.destination.readBytes(ctx, object.Backup, int64(len(expected))+1)
			if err != nil || !bytes.Equal(readback, expected) {
				t.Fatal("session archive backup object exact-byte readback failed")
			}
			return backup.BackupID
		}
	}
	t.Fatal("verified backup with the canonical session archive fixture is absent")
	return ""
}

func deleteExactObjectVersions(t *testing.T, ctx context.Context, client *Client, key string) {
	t.Helper()
	input := &s3.ListObjectVersionsInput{Bucket: aws.String(client.config.Bucket), Prefix: aws.String(key)}
	for {
		output, err := client.api.ListObjectVersions(ctx, input)
		if err != nil {
			t.Fatal("list session archive fixture versions for cleanup")
		}
		for _, version := range output.Versions {
			if aws.ToString(version.Key) != key {
				continue
			}
			if _, err := client.api.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(client.config.Bucket), Key: version.Key, VersionId: version.VersionId,
			}); err != nil {
				t.Fatal("delete session archive fixture version")
			}
		}
		for _, marker := range output.DeleteMarkers {
			if aws.ToString(marker.Key) != key {
				continue
			}
			if _, err := client.api.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(client.config.Bucket), Key: marker.Key, VersionId: marker.VersionId,
			}); err != nil {
				t.Fatal("delete session archive fixture marker")
			}
		}
		if !aws.ToBool(output.IsTruncated) {
			break
		}
		if output.NextKeyMarker == nil || output.NextVersionIdMarker == nil {
			t.Fatal("session archive fixture cleanup pagination is incomplete")
		}
		input.KeyMarker = output.NextKeyMarker
		input.VersionIdMarker = output.NextVersionIdMarker
	}
	remaining, err := client.api.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
		Bucket: aws.String(client.config.Bucket), Prefix: aws.String(key),
	})
	if err != nil {
		t.Fatal("read back session archive fixture cleanup")
	}
	for _, version := range remaining.Versions {
		if aws.ToString(version.Key) == key {
			t.Fatal("session archive fixture version remains after cleanup")
		}
	}
	for _, marker := range remaining.DeleteMarkers {
		if aws.ToString(marker.Key) == key {
			t.Fatal("session archive fixture marker remains after cleanup")
		}
	}
}

func readPrivateE2EFile(t *testing.T, value string, maximumBytes int64) []byte {
	t.Helper()
	value = strings.TrimSpace(value)
	info, err := os.Lstat(value)
	if err != nil || !filepath.IsAbs(value) || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > maximumBytes {
		t.Fatal("session archive E2E file is invalid")
	}
	payload, err := os.ReadFile(value)
	if err != nil || int64(len(payload)) != info.Size() {
		t.Fatal("read session archive E2E file")
	}
	return payload
}

func writePrivateE2EResult(t *testing.T, value, content string) {
	t.Helper()
	value = strings.TrimSpace(value)
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		t.Fatal("session archive E2E result path is invalid")
	}
	parent, err := os.Lstat(filepath.Dir(value))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0o077 != 0 {
		t.Fatal("session archive E2E result directory is unsafe")
	}
	file, err := os.OpenFile(value, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create session archive E2E result")
	}
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(value)
		t.Fatal(errors.New("write session archive E2E result"))
	}
}

func findPlatformObject(value manifest.Manifest, storeName, key string) (manifest.PlatformObject, bool) {
	for _, object := range value.PlatformObjects {
		if object.StoreName == storeName && object.Source.Key == key {
			return object, true
		}
	}
	return manifest.PlatformObject{}, false
}
