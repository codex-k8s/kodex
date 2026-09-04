package s3backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

type fakeObject struct {
	body, checksum, etag, version string
	metadata                      map[string]string
}

type fakeS3 struct {
	objects             map[string]fakeObject
	lastIfNoMatch       string
	lastDeleteIfMatch   string
	lastDeleteVersionID string
	nextVersion         int
	deleteHook          func()
	deleteResponseLost  bool
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]fakeObject{}, nextVersion: 1}
}

func (fake *fakeS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}

func (fake *fakeS3) GetBucketVersioning(context.Context, *s3.GetBucketVersioningInput, ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error) {
	return &s3.GetBucketVersioningOutput{Status: types.BucketVersioningStatusEnabled}, nil
}

func (fake *fakeS3) ListObjectVersions(_ context.Context, input *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	keys := make([]string, 0, len(fake.objects))
	for key := range fake.objects {
		if strings.HasPrefix(key, aws.ToString(input.Prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	versions := make([]types.ObjectVersion, 0, len(keys))
	for _, key := range keys {
		object := fake.objects[key]
		versions = append(versions, types.ObjectVersion{Key: aws.String(key), VersionId: aws.String(object.version),
			ETag: aws.String(object.etag), Size: aws.Int64(int64(len(object.body))), IsLatest: aws.Bool(true)})
	}
	return &s3.ListObjectVersionsOutput{Versions: versions, IsTruncated: aws.Bool(false)}, nil
}

func (fake *fakeS3) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	object, exists := fake.objects[aws.ToString(input.Key)]
	if !exists || object.version != aws.ToString(input.VersionId) {
		return nil, responseError(http.StatusNotFound)
	}
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(object.body))), ETag: aws.String(object.etag),
		VersionId: aws.String(object.version), ChecksumSHA256: aws.String(object.checksum), Metadata: object.metadata}, nil
}

func (fake *fakeS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	object, exists := fake.objects[aws.ToString(input.Key)]
	if !exists || object.version != aws.ToString(input.VersionId) {
		return nil, responseError(http.StatusNotFound)
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewBufferString(object.body)),
		ContentLength: aws.Int64(int64(len(object.body))), ETag: aws.String(object.etag),
		VersionId: aws.String(object.version), ChecksumSHA256: aws.String(object.checksum)}, nil
}

func (fake *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	key := aws.ToString(input.Key)
	fake.lastIfNoMatch = aws.ToString(input.IfNoneMatch)
	if _, exists := fake.objects[key]; exists && fake.lastIfNoMatch == "*" {
		return nil, responseError(http.StatusPreconditionFailed)
	}
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	version := "version-" + string(rune('0'+fake.nextVersion))
	fake.nextVersion++
	etag := "\"etag-" + version + "\""
	fake.objects[key] = fakeObject{body: string(payload), checksum: aws.ToString(input.ChecksumSHA256),
		etag: etag, version: version, metadata: input.Metadata}
	return &s3.PutObjectOutput{ETag: aws.String(etag), VersionId: aws.String(version)}, nil
}

func (fake *fakeS3) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	key := aws.ToString(input.Key)
	object, exists := fake.objects[key]
	fake.lastDeleteIfMatch = aws.ToString(input.IfMatch)
	fake.lastDeleteVersionID = aws.ToString(input.VersionId)
	if !exists {
		return nil, responseError(http.StatusNotFound)
	}
	if fake.lastDeleteVersionID != "" && object.version != fake.lastDeleteVersionID {
		return nil, responseError(http.StatusNotFound)
	}
	if fake.lastDeleteIfMatch != "" && strings.Trim(fake.lastDeleteIfMatch, "\"") != strings.Trim(object.etag, "\"") {
		return nil, responseError(http.StatusPreconditionFailed)
	}
	delete(fake.objects, key)
	if fake.deleteHook != nil {
		hook := fake.deleteHook
		fake.deleteHook = nil
		hook()
	}
	if fake.deleteResponseLost {
		fake.deleteResponseLost = false
		return nil, responseError(http.StatusInternalServerError)
	}
	return &s3.DeleteObjectOutput{}, nil
}

func responseError(status int) error {
	return &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
		Err: errors.New("fake S3 response")}
}

func testRepository(t *testing.T) (*Repository, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	client := &Client{api: fake, config: configspec.S3{Name: "backup", Bucket: "backup"}}
	repository, err := NewRepository(client, "kodex")
	if err != nil {
		t.Fatal(err)
	}
	return repository, fake
}

func TestOperationLockIsExclusiveAndExactlyReleased(t *testing.T) {
	t.Parallel()
	repository, fake := testRepository(t)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	lock, err := repository.AcquireOperationLock(context.Background(), "backup", "attempt", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastIfNoMatch != "*" {
		t.Fatalf("IfNoneMatch = %q", fake.lastIfNoMatch)
	}
	if _, err := repository.AcquireOperationLock(context.Background(), "restore", "restore", now, time.Hour); err == nil {
		t.Fatal("second operation acquired the immutable lock")
	}
	if err := lock.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.lastDeleteVersionID != "" || fake.lastDeleteIfMatch != "\"etag-version-1\"" {
		t.Fatalf("lock release delete condition = version %q, If-Match %q",
			fake.lastDeleteVersionID, fake.lastDeleteIfMatch)
	}
	if _, err := repository.AcquireOperationLock(context.Background(), "restore", "restore", now, time.Hour); err != nil {
		t.Fatalf("lock was not exactly released: %v", err)
	}
}

func TestOperationLockReleaseAcceptsLostResponseAfterExactDeletion(t *testing.T) {
	t.Parallel()
	repository, fake := testRepository(t)
	lock, err := repository.AcquireOperationLock(context.Background(), "backup", "attempt",
		time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fake.deleteResponseLost = true
	if err := lock.Release(context.Background()); err != nil {
		t.Fatalf("release after lost S3 response: %v", err)
	}
	if len(fake.objects) != 0 {
		t.Fatalf("lock object survived exact deletion: %#v", fake.objects)
	}
}

func TestOperationLockReplacesOnlyExpiredExactVersion(t *testing.T) {
	t.Parallel()
	repository, fake := testRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	stale, err := repository.AcquireOperationLock(ctx, "backup", "stale-attempt", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := repository.AcquireOperationLock(ctx, "restore", "restore-attempt", now.Add(time.Hour), time.Hour)
	if err != nil {
		t.Fatalf("replace expired lock: %v", err)
	}
	if replacement.receipt.VersionID == stale.receipt.VersionID {
		t.Fatal("expired lock replacement reused the stale exact version")
	}
	if err := stale.Release(ctx); err == nil {
		t.Fatal("stale lock holder released the replacement lock")
	}
	if fake.lastDeleteVersionID != "" || fake.lastDeleteIfMatch != "\"etag-version-1\"" {
		t.Fatal("stale lock holder did not use its exact conditional identity")
	}
	receipt, readback, err := repository.loadOperationLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.VersionID != replacement.receipt.VersionID || readback.Operation != "restore" ||
		readback.OperationID != "restore-attempt" || !readback.AcquiredAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("replacement lock readback = %#v, %#v", receipt, readback)
	}
	if err := replacement.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.objects) != 0 {
		t.Fatalf("lock objects after exact release = %#v", fake.objects)
	}
}

func TestOperationLockExpiredReplacementLosesRaceSafely(t *testing.T) {
	t.Parallel()
	repository, fake := testRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if _, err := repository.AcquireOperationLock(ctx, "backup", "stale-attempt", now, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	competitor := operationLockDocument{SchemaVersion: 1, Kind: "kodex-backup-operation-lock",
		Operation: "verify", OperationID: "competing-attempt", AcquiredAt: now.Add(10 * time.Minute),
		ExpiresAt: now.Add(20 * time.Minute)}
	var hookErr error
	fake.deleteHook = func() {
		_, hookErr = repository.PutJSON(ctx, "locks/controller.json", competitor)
	}
	if _, err := repository.AcquireOperationLock(ctx, "restore", "losing-attempt",
		now.Add(10*time.Minute), time.Hour); err == nil || err.Error() != "backup repository operation is already locked" {
		t.Fatalf("expired lock replacement race error = %v", err)
	}
	if hookErr != nil {
		t.Fatalf("create competing lock: %v", hookErr)
	}
	var readback operationLockDocument
	if _, err := repository.LoadJSON(ctx, "locks/controller.json", 4<<10, &readback); err != nil {
		t.Fatal(err)
	}
	if readback.OperationID != competitor.OperationID || readback.Operation != competitor.Operation {
		t.Fatalf("competing lock was overwritten: %#v", readback)
	}
}

func TestCatalogExposesOnlyVerifiedBackupsAndValidDrills(t *testing.T) {
	t.Parallel()
	repository, _ := testRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	backupID := "20260828T120000Z-0123456789abcdef"
	digestValue := sha256.Sum256([]byte("dump"))
	digest := "sha256:" + hex.EncodeToString(digestValue[:])
	dump, err := repository.destination.putBytes(ctx, repository.key("backups/"+backupID+"/payload.dump"),
		"application/octet-stream", []byte("dump"), digest)
	if err != nil {
		t.Fatal(err)
	}
	backup := manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup",
		BackupID: backupID, State: "complete", ControllerVersion: "test",
		ReleaseRevision: "sha256:test", StartedAt: now, CompletedAt: now.Add(time.Minute),
		ConsistencyModel: manifest.BoundedCrashConsistencyModel, ConsistencyStarted: now,
		ConsistencyFinished: now.Add(30 * time.Second),
		DatabaseCount:       1, Databases: []manifest.Database{{Name: "control-plane", Engine: "postgresql",
			ServerVersion: "180003", SchemaKind: "goose", SchemaVersion: "goose:1",
			SchemaChecksum: dump.ChecksumSHA256, SnapshotStarted: now, SnapshotFinished: now.Add(time.Second),
			Dump: dump, Schema: dump}}}
	manifestReceipt, err := repository.PutJSON(ctx, "backups/"+backup.BackupID+"/manifest.json", backup)
	if err != nil {
		t.Fatal(err)
	}
	values, _, err := repository.Catalog(ctx)
	if err != nil || len(values) != 0 {
		t.Fatalf("unverified catalog = %#v, %v", values, err)
	}
	verification := manifest.Verification{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup-verification",
		BackupID: backup.BackupID, Manifest: manifestReceipt, VerifiedAt: now.Add(2 * time.Minute), ObjectCount: 3}
	if err := repository.EnsureVerification(ctx, backup, manifestReceipt, verification); err != nil {
		t.Fatal(err)
	}
	values, drilled, err := repository.Catalog(ctx)
	if err != nil || len(values) != 1 || !drilled[backup.BackupID].IsZero() {
		t.Fatalf("verified catalog = %#v, %#v, %v", values, drilled, err)
	}
	drill := manifest.RestoreDrill{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-restore-drill", RestoreID: "restore-test",
		ApprovalID: "approval-test", BackupID: backup.BackupID, RequestSHA256: dump.ChecksumSHA256,
		TargetSetSHA256: dump.ChecksumSHA256, CompletedAt: now.Add(3 * time.Minute),
		Databases: []manifest.RestoreDatabase{{Name: "control-plane", SchemaVersion: "goose:1",
			TargetDigest: dump.ChecksumSHA256}}}
	if _, err := repository.PutJSON(ctx, "backups/"+backup.BackupID+"/restore-drills/restore-test.json", drill); err != nil {
		t.Fatal(err)
	}
	_, drilled, err = repository.Catalog(ctx)
	if err != nil || !drilled[backup.BackupID].Equal(drill.CompletedAt) {
		t.Fatalf("restore drill catalog = %#v, %v", drilled, err)
	}
}
