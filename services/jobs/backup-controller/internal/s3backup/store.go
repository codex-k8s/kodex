// Package s3backup реализует immutable backup repository поверх versioned S3.
package s3backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

const digestMetadataKey = "kodex-sha256"
const operationLockInputError = "backup repository operation lock input is invalid"

var (
	ErrConflict = errors.New("immutable S3 object already exists")
	ErrNotFound = errors.New("S3 object is not found")
)

type api interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	GetBucketVersioning(context.Context, *s3.GetBucketVersioningInput, ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error)
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type Client struct {
	api    api
	config configspec.S3
}

type Repository struct {
	destination *Client
	prefix      string
}

type OperationLock struct {
	repository *Repository
	receipt    manifest.Receipt
}

type operationLockDocument struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	Operation     string    `json:"operation"`
	OperationID   string    `json:"operationId"`
	AcquiredAt    time.Time `json:"acquiredAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func NewClient(ctx context.Context, value configspec.S3) (*Client, error) {
	loaded, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(value.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			value.AccessKeyID, value.SecretAccessKey, value.SessionToken,
		)),
	)
	if err != nil {
		return nil, errors.New("initialize S3 client")
	}
	client := s3.NewFromConfig(loaded, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(value.Endpoint)
		options.UsePathStyle = value.UsePathStyle
	})
	return &Client{api: client, config: value}, nil
}

func NewRepository(destination *Client, prefix string) (*Repository, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if destination == nil || prefix == "" || strings.Contains(prefix, "..") {
		return nil, errors.New("backup repository configuration is invalid")
	}
	return &Repository{destination: destination, prefix: prefix}, nil
}

func (client *Client) Check(ctx context.Context) error {
	if client == nil || client.api == nil || client.config.Bucket == "" {
		return errors.New("S3 client is invalid")
	}
	if _, err := client.api.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(client.config.Bucket)}); err != nil {
		return errors.New("S3 bucket is unavailable")
	}
	versioning, err := client.api.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(client.config.Bucket)})
	if err != nil || versioning.Status != types.BucketVersioningStatusEnabled {
		return errors.New("S3 bucket versioning is not enabled")
	}
	return nil
}

func (repository *Repository) Check(ctx context.Context, sources []configspec.S3) error {
	if err := repository.destination.Check(ctx); err != nil {
		return err
	}
	for _, source := range sources {
		client, err := NewClient(ctx, source)
		if err != nil {
			return fmt.Errorf("initialize S3 source %s: failed", source.Name)
		}
		if err := client.Check(ctx); err != nil {
			return fmt.Errorf("check S3 source %s: failed", source.Name)
		}
	}
	return nil
}

func (repository *Repository) AcquireOperationLock(ctx context.Context, operation, operationID string,
	now time.Time, ttl time.Duration) (*OperationLock, error) {
	if operationID == "" || len(operationID) > 128 || now.IsZero() || ttl < 10*time.Minute || ttl > 25*time.Hour {
		return nil, errors.New(operationLockInputError)
	}
	switch operation {
	case "backup", "retention", "restore", "verify":
	default:
		return nil, errors.New(operationLockInputError)
	}
	document := operationLockDocument{
		SchemaVersion: 1, Kind: "kodex-backup-operation-lock", Operation: operation,
		OperationID: operationID, AcquiredAt: now, ExpiresAt: now.Add(ttl),
	}
	receipt, err := repository.PutJSON(ctx, "locks/controller.json", document)
	if errors.Is(err, ErrConflict) {
		existingReceipt, existing, readErr := repository.loadOperationLock(ctx)
		if readErr != nil {
			return nil, readErr
		}
		if now.Before(existing.ExpiresAt) {
			return nil, errors.New("backup repository operation is already locked")
		}
		if deleteErr := repository.deleteExpiredOperationLock(ctx, existingReceipt); deleteErr != nil {
			return nil, deleteErr
		}
		receipt, err = repository.PutJSON(ctx, "locks/controller.json", document)
		if errors.Is(err, ErrConflict) {
			return nil, errors.New("backup repository operation is already locked")
		}
	}
	if err != nil {
		return nil, err
	}
	return &OperationLock{repository: repository, receipt: receipt}, nil
}

func (repository *Repository) loadOperationLock(ctx context.Context) (manifest.Receipt, operationLockDocument, error) {
	var document operationLockDocument
	receipt, err := repository.LoadJSON(ctx, "locks/controller.json", 4<<10, &document)
	if err != nil || !validOperationLockDocument(document) {
		return manifest.Receipt{}, operationLockDocument{}, errors.New("backup repository operation lock readback is invalid")
	}
	return receipt, document, nil
}

func (repository *Repository) deleteExpiredOperationLock(ctx context.Context, receipt manifest.Receipt) error {
	if !receipt.Valid() || receipt.Bucket != repository.destination.config.Bucket ||
		receipt.Key != repository.key("locks/controller.json") {
		return errors.New("expired backup repository operation lock receipt is invalid")
	}
	_, err := repository.destination.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(receipt.Bucket), Key: aws.String(receipt.Key), VersionId: aws.String(receipt.VersionID),
	})
	if err != nil {
		return errors.New("delete expired backup repository operation lock")
	}
	if _, err = repository.destination.head(ctx, receipt.Key, receipt.VersionID); errors.Is(err, ErrNotFound) {
		return nil
	}
	return errors.New("expired backup repository operation lock deletion readback failed")
}

func validOperationLockDocument(document operationLockDocument) bool {
	if document.SchemaVersion != 1 || document.Kind != "kodex-backup-operation-lock" ||
		document.OperationID == "" || len(document.OperationID) > 128 || document.AcquiredAt.IsZero() ||
		document.ExpiresAt.IsZero() || !document.ExpiresAt.After(document.AcquiredAt) {
		return false
	}
	switch document.Operation {
	case "backup", "retention", "restore", "verify":
	default:
		return false
	}
	ttl := document.ExpiresAt.Sub(document.AcquiredAt)
	return ttl >= 10*time.Minute && ttl <= 25*time.Hour
}

func (lock *OperationLock) Release(ctx context.Context) error {
	if lock == nil || lock.repository == nil || !lock.receipt.Valid() {
		return errors.New("backup repository operation lock is invalid")
	}
	_, err := lock.repository.destination.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(lock.receipt.Bucket), Key: aws.String(lock.receipt.Key),
		VersionId: aws.String(lock.receipt.VersionID),
	})
	if err != nil {
		return errors.New("release backup repository operation lock")
	}
	_, err = lock.repository.receiptForKey(ctx, lock.receipt.Key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return errors.New("backup repository operation lock release readback failed")
}

func (repository *Repository) PutFile(ctx context.Context, key, mediaType, filePath, expectedDigest string, expectedSize int64) (manifest.Receipt, error) {
	return repository.destination.putFile(ctx, repository.key(key), mediaType, filePath, expectedDigest, expectedSize)
}

func (repository *Repository) PutJSON(ctx context.Context, key string, value any) (manifest.Receipt, error) {
	payload, digest, err := manifest.Marshal(value)
	if err != nil {
		return manifest.Receipt{}, err
	}
	return repository.destination.putBytes(ctx, repository.key(key), "application/json", payload, digest)
}

func (repository *Repository) EnsureJSON(ctx context.Context, key string, value any) (manifest.Receipt, error) {
	receipt, err := repository.PutJSON(ctx, key, value)
	if !errors.Is(err, ErrConflict) {
		return receipt, err
	}
	payload, digest, marshalErr := manifest.Marshal(value)
	if marshalErr != nil {
		return manifest.Receipt{}, marshalErr
	}
	existing, readErr := repository.receiptForKey(ctx, repository.key(key))
	if readErr != nil {
		return manifest.Receipt{}, readErr
	}
	readback, readErr := repository.destination.readBytes(ctx, existing, int64(len(payload))+1)
	if readErr != nil || existing.ChecksumSHA256 != digest || !bytes.Equal(readback, payload) {
		return manifest.Receipt{}, ErrConflict
	}
	return existing, nil
}

func (repository *Repository) EnsureVerification(ctx context.Context, backup manifest.Manifest,
	manifestReceipt manifest.Receipt, value manifest.Verification) error {
	if err := value.Validate(backup, manifestReceipt); err != nil {
		return err
	}
	key := path.Join("backups", backup.BackupID, "verification.json")
	if _, err := repository.PutJSON(ctx, key, value); !errors.Is(err, ErrConflict) {
		return err
	}
	var existing manifest.Verification
	if _, err := repository.LoadJSON(ctx, key, 1<<20, &existing); err != nil {
		return err
	}
	return existing.Validate(backup, manifestReceipt)
}

func (repository *Repository) EnsureRestoreIntent(ctx context.Context, value manifest.RestoreIntent) error {
	if !value.Matches(value) {
		return errors.New("restore intent is invalid")
	}
	key := path.Join("restores", value.RestoreID, "intent.json")
	if _, err := repository.PutJSON(ctx, key, value); !errors.Is(err, ErrConflict) {
		return err
	}
	var existing manifest.RestoreIntent
	if _, err := repository.LoadJSON(ctx, key, 1<<20, &existing); err != nil {
		return err
	}
	if !existing.Matches(value) {
		return ErrConflict
	}
	return nil
}

func (repository *Repository) LoadManifest(ctx context.Context, backupID string) (manifest.Manifest, manifest.Receipt, error) {
	key := repository.key(path.Join("backups", backupID, "manifest.json"))
	receipt, err := repository.receiptForKey(ctx, key)
	if err != nil {
		return manifest.Manifest{}, manifest.Receipt{}, err
	}
	payload, err := repository.destination.readBytes(ctx, receipt, 32<<20)
	if err != nil {
		return manifest.Manifest{}, manifest.Receipt{}, err
	}
	var value manifest.Manifest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decodeJSON(payload, &value); err != nil || value.Validate() != nil || value.BackupID != backupID ||
		!repository.backupReceiptsBound(value) {
		return manifest.Manifest{}, manifest.Receipt{}, errors.New("backup manifest readback is invalid")
	}
	return value, receipt, nil
}

func (repository *Repository) LoadVerifiedManifest(ctx context.Context, backupID string) (manifest.Manifest, manifest.Receipt, error) {
	value, receipt, err := repository.LoadManifest(ctx, backupID)
	if err != nil {
		return manifest.Manifest{}, manifest.Receipt{}, err
	}
	var verification manifest.Verification
	if _, err := repository.LoadJSON(ctx, path.Join("backups", backupID, "verification.json"),
		1<<20, &verification); err != nil {
		return manifest.Manifest{}, manifest.Receipt{}, errors.New("backup verification receipt is unavailable")
	}
	if err := verification.Validate(value, receipt); err != nil {
		return manifest.Manifest{}, manifest.Receipt{}, err
	}
	return value, receipt, nil
}

func (repository *Repository) LoadRestoreDrill(ctx context.Context, key string,
	backup manifest.Manifest) (manifest.RestoreDrill, error) {
	var value manifest.RestoreDrill
	if _, err := repository.LoadJSON(ctx, key, 4<<20, &value); err != nil {
		return manifest.RestoreDrill{}, err
	}
	if err := value.Validate(backup); err != nil {
		return manifest.RestoreDrill{}, err
	}
	return value, nil
}

func (repository *Repository) LoadJSON(ctx context.Context, key string, maximumBytes int64, target any) (manifest.Receipt, error) {
	receipt, err := repository.receiptForKey(ctx, repository.key(key))
	if err != nil {
		return manifest.Receipt{}, err
	}
	payload, err := repository.destination.readBytes(ctx, receipt, maximumBytes)
	if err != nil {
		return manifest.Receipt{}, err
	}
	if err := decodeJSON(payload, target); err != nil {
		return manifest.Receipt{}, errors.New("decode immutable S3 document")
	}
	return receipt, nil
}

func (repository *Repository) Exists(ctx context.Context, key string) (bool, error) {
	_, err := repository.receiptForKey(ctx, repository.key(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (repository *Repository) CopyPlatformObjects(ctx context.Context, backupID, workDirectory string, sources []configspec.S3) ([]manifest.PlatformObject, error) {
	result := make([]manifest.PlatformObject, 0)
	for _, sourceConfig := range sources {
		source, err := NewClient(ctx, sourceConfig)
		if err != nil {
			return result, fmt.Errorf("initialize S3 source %s: failed", sourceConfig.Name)
		}
		objects, err := source.currentInventory(ctx, sourceConfig.Prefix)
		if err != nil {
			return result, fmt.Errorf("inventory S3 source %s: failed", sourceConfig.Name)
		}
		for index, object := range objects {
			filePath := filepath.Join(workDirectory, fmt.Sprintf("object-%s-%08d", sourceConfig.Name, index))
			if err := source.download(ctx, object, filePath); err != nil {
				return result, fmt.Errorf("read S3 source %s object: failed", sourceConfig.Name)
			}
			identity := sha256.Sum256([]byte(object.Key + "\x00" + object.VersionID))
			backupKey := path.Join("backups", backupID, "objects", sourceConfig.Name,
				hex.EncodeToString(identity[:]))
			backup, putErr := repository.PutFile(ctx, backupKey, "application/octet-stream", filePath,
				object.ChecksumSHA256, object.SizeBytes)
			removeErr := os.Remove(filePath)
			if putErr == nil {
				result = append(result, manifest.PlatformObject{StoreName: sourceConfig.Name, Source: object, Backup: backup})
			}
			if putErr != nil || removeErr != nil {
				return result, fmt.Errorf("store S3 source %s object: failed", sourceConfig.Name)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StoreName != result[j].StoreName {
			return result[i].StoreName < result[j].StoreName
		}
		return result[i].Source.Key < result[j].Source.Key
	})
	return result, nil
}

func (repository *Repository) Verify(ctx context.Context, value manifest.Manifest, manifestReceipt manifest.Receipt, workDirectory string) error {
	if _, err := repository.destination.readBytes(ctx, manifestReceipt, 32<<20); err != nil {
		return err
	}
	for _, database := range value.Databases {
		for _, receipt := range []manifest.Receipt{database.Dump, database.Schema} {
			if err := repository.destination.verify(ctx, receipt); err != nil {
				return err
			}
		}
	}
	for _, object := range value.PlatformObjects {
		if err := repository.destination.verify(ctx, object.Backup); err != nil {
			return err
		}
	}
	return nil
}

func (repository *Repository) RestorePlatformObjects(ctx context.Context, value manifest.Manifest, targetConfig configspec.S3, workDirectory string) ([]manifest.Receipt, error) {
	target, err := NewClient(ctx, targetConfig)
	if err != nil {
		return nil, errors.New("initialize restore S3 target")
	}
	if err := target.Check(ctx); err != nil {
		return nil, err
	}
	existing, err := target.listVersions(ctx, targetConfig.Prefix)
	if err != nil {
		return nil, err
	}
	if len(existing) != 0 {
		return nil, errors.New("restore S3 target is not empty")
	}
	result := make([]manifest.Receipt, 0, len(value.PlatformObjects))
	for index, object := range value.PlatformObjects {
		filePath := filepath.Join(workDirectory, fmt.Sprintf("restore-object-%08d", index))
		if err := repository.destination.download(ctx, object.Backup, filePath); err != nil {
			return result, errors.New("read backup object for restore")
		}
		targetKey := object.Source.Key
		if prefix := strings.Trim(targetConfig.Prefix, "/"); prefix != "" {
			targetKey = prefix + "/" + object.Source.Key
		}
		receipt, putErr := target.putFile(ctx, targetKey, "application/octet-stream", filePath,
			object.Source.ChecksumSHA256, object.Source.SizeBytes)
		removeErr := os.Remove(filePath)
		if putErr == nil {
			result = append(result, receipt)
		}
		if putErr != nil || removeErr != nil {
			return result, errors.New("restore platform object")
		}
	}
	return result, nil
}

func (repository *Repository) Download(ctx context.Context, receipt manifest.Receipt, filePath string) error {
	return repository.destination.download(ctx, receipt, filePath)
}

func (repository *Repository) Catalog(ctx context.Context) ([]manifest.Manifest, map[string]time.Time, error) {
	objects, err := repository.destination.listVersions(ctx, repository.key("backups/"))
	if err != nil {
		return nil, nil, err
	}
	backupIDs := map[string]struct{}{}
	available := make(map[string]struct{}, len(objects))
	for _, object := range objects {
		relative := strings.TrimPrefix(object.Key, repository.key("backups/")+"/")
		available[relative] = struct{}{}
		parts := strings.Split(relative, "/")
		if len(parts) < 2 {
			continue
		}
		if parts[1] == "manifest.json" {
			backupIDs[parts[0]] = struct{}{}
		}
	}
	values := make([]manifest.Manifest, 0, len(backupIDs))
	byID := make(map[string]manifest.Manifest, len(backupIDs))
	for backupID := range backupIDs {
		if _, exists := available[path.Join(backupID, "verification.json")]; !exists {
			continue
		}
		value, _, err := repository.LoadVerifiedManifest(ctx, backupID)
		if err != nil {
			return nil, nil, err
		}
		values = append(values, value)
		byID[backupID] = value
	}
	drilled := map[string]time.Time{}
	for relative := range available {
		parts := strings.Split(relative, "/")
		if len(parts) != 3 || parts[1] != "restore-drills" || !strings.HasSuffix(parts[2], ".json") {
			continue
		}
		backup, exists := byID[parts[0]]
		if !exists {
			continue
		}
		drill, err := repository.LoadRestoreDrill(ctx, path.Join("backups", relative), backup)
		if err != nil {
			return nil, nil, err
		}
		if drill.BackupID != parts[0] {
			return nil, nil, errors.New("restore drill backup binding is invalid")
		}
		if drill.CompletedAt.After(drilled[parts[0]]) {
			drilled[parts[0]] = drill.CompletedAt
		}
	}
	return values, drilled, nil
}

func (repository *Repository) DeleteBackup(ctx context.Context, backupID string) error {
	prefix := repository.key(path.Join("backups", backupID)) + "/"
	objects, err := repository.destination.listVersions(ctx, prefix)
	if err != nil {
		return err
	}
	for _, object := range objects {
		if _, err := repository.destination.api.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(repository.destination.config.Bucket), Key: aws.String(object.Key),
			VersionId: aws.String(object.VersionID),
		}); err != nil {
			return errors.New("delete retained backup object")
		}
	}
	readback, err := repository.destination.listVersions(ctx, prefix)
	if err != nil || len(readback) != 0 {
		return errors.New("retained backup deletion readback failed")
	}
	return nil
}

func (repository *Repository) key(relative string) string {
	return path.Join(repository.prefix, relative)
}

func (repository *Repository) backupReceiptsBound(value manifest.Manifest) bool {
	prefix := repository.key(path.Join("backups", value.BackupID)) + "/"
	bound := func(receipt manifest.Receipt) bool {
		return receipt.Bucket == repository.destination.config.Bucket && strings.HasPrefix(receipt.Key, prefix)
	}
	for _, database := range value.Databases {
		if !bound(database.Dump) || !bound(database.Schema) {
			return false
		}
	}
	for _, object := range value.PlatformObjects {
		if !bound(object.Backup) {
			return false
		}
	}
	return true
}

func (repository *Repository) receiptForKey(ctx context.Context, key string) (manifest.Receipt, error) {
	objects, err := repository.destination.inventory(ctx, key)
	if err != nil {
		return manifest.Receipt{}, err
	}
	for _, object := range objects {
		if object.Key == key {
			return object, nil
		}
	}
	return manifest.Receipt{}, ErrNotFound
}

func (client *Client) inventory(ctx context.Context, prefix string) ([]manifest.Receipt, error) {
	versions, err := client.listVersions(ctx, prefix)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	result := make([]manifest.Receipt, 0, len(versions))
	for _, version := range versions {
		if _, exists := seen[version.Key]; exists {
			return nil, errors.New("mutable or duplicated S3 object version is forbidden")
		}
		seen[version.Key] = struct{}{}
		receipt, err := client.head(ctx, version.Key, version.VersionID)
		if err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (client *Client) currentInventory(ctx context.Context, prefix string) ([]manifest.Receipt, error) {
	input := &s3.ListObjectVersionsInput{Bucket: aws.String(client.config.Bucket)}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	current := map[string]string{}
	deleted := map[string]struct{}{}
	for {
		output, err := client.api.ListObjectVersions(ctx, input)
		if err != nil {
			return nil, errors.New("list current S3 object versions")
		}
		for _, version := range output.Versions {
			if !aws.ToBool(version.IsLatest) {
				continue
			}
			key, versionID := aws.ToString(version.Key), aws.ToString(version.VersionId)
			_, markedDeleted := deleted[key]
			if key == "" || versionID == "" || current[key] != "" || markedDeleted {
				return nil, errors.New("current S3 version inventory is incomplete")
			}
			current[key] = versionID
		}
		for _, marker := range output.DeleteMarkers {
			if aws.ToBool(marker.IsLatest) {
				key := aws.ToString(marker.Key)
				if key == "" || current[key] != "" {
					return nil, errors.New("current S3 delete marker inventory is incomplete")
				}
				deleted[key] = struct{}{}
			}
		}
		if !aws.ToBool(output.IsTruncated) {
			break
		}
		if output.NextKeyMarker == nil || output.NextVersionIdMarker == nil {
			return nil, errors.New("S3 version pagination is incomplete")
		}
		input.KeyMarker = output.NextKeyMarker
		input.VersionIdMarker = output.NextVersionIdMarker
	}
	result := make([]manifest.Receipt, 0, len(current))
	for key, versionID := range current {
		if _, isDeleted := deleted[key]; isDeleted {
			continue
		}
		receipt, err := client.head(ctx, key, versionID)
		if err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (client *Client) listVersions(ctx context.Context, prefix string) ([]manifest.Receipt, error) {
	input := &s3.ListObjectVersionsInput{Bucket: aws.String(client.config.Bucket)}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	result := make([]manifest.Receipt, 0)
	for {
		output, err := client.api.ListObjectVersions(ctx, input)
		if err != nil {
			return nil, errors.New("list S3 object versions")
		}
		if len(output.DeleteMarkers) != 0 {
			return nil, errors.New("S3 delete marker is forbidden in immutable inventory")
		}
		for _, version := range output.Versions {
			if !aws.ToBool(version.IsLatest) || aws.ToString(version.Key) == "" || aws.ToString(version.VersionId) == "" {
				return nil, errors.New("S3 version inventory is incomplete")
			}
			result = append(result, manifest.Receipt{Bucket: client.config.Bucket, Key: aws.ToString(version.Key),
				VersionID: aws.ToString(version.VersionId), ETag: strings.Trim(aws.ToString(version.ETag), "\""),
				SizeBytes: aws.ToInt64(version.Size)})
		}
		if !aws.ToBool(output.IsTruncated) {
			break
		}
		if output.NextKeyMarker == nil || output.NextVersionIdMarker == nil {
			return nil, errors.New("S3 version pagination is incomplete")
		}
		input.KeyMarker = output.NextKeyMarker
		input.VersionIdMarker = output.NextVersionIdMarker
	}
	return result, nil
}

func (client *Client) head(ctx context.Context, key, versionID string) (manifest.Receipt, error) {
	output, err := client.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(client.config.Bucket), Key: aws.String(key), VersionId: aws.String(versionID),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return manifest.Receipt{}, mapError(err)
	}
	digest := output.Metadata[digestMetadataKey]
	if !validDigest(digest) || output.ContentLength == nil || aws.ToString(output.VersionId) != versionID ||
		strings.Trim(aws.ToString(output.ETag), "\"") == "" {
		return manifest.Receipt{}, errors.New("S3 object receipt is incomplete")
	}
	if output.ChecksumSHA256 != nil && aws.ToString(output.ChecksumSHA256) != checksumBase64(digest) {
		return manifest.Receipt{}, errors.New("S3 object checksum receipt mismatches metadata")
	}
	return manifest.Receipt{Bucket: client.config.Bucket, Key: key, VersionID: versionID,
		ETag: strings.Trim(aws.ToString(output.ETag), "\""), ChecksumSHA256: digest,
		SizeBytes: aws.ToInt64(output.ContentLength)}, nil
}

func (client *Client) putBytes(ctx context.Context, key, mediaType string, payload []byte, digest string) (manifest.Receipt, error) {
	return client.put(ctx, key, mediaType, bytes.NewReader(payload), int64(len(payload)), digest)
}

func (client *Client) putFile(ctx context.Context, key, mediaType, filePath, digest string, size int64) (manifest.Receipt, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return manifest.Receipt{}, errors.New("open immutable backup payload")
	}
	defer file.Close()
	return client.put(ctx, key, mediaType, file, size, digest)
}

func (client *Client) put(ctx context.Context, key, mediaType string, body io.Reader, size int64, digest string) (manifest.Receipt, error) {
	if key == "" || mediaType == "" || size < 0 || !validDigest(digest) {
		return manifest.Receipt{}, errors.New("immutable S3 put input is invalid")
	}
	output, err := client.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(client.config.Bucket), Key: aws.String(key), Body: body,
		ContentLength: aws.Int64(size), ContentType: aws.String(mediaType), IfNoneMatch: aws.String("*"),
		ChecksumSHA256: aws.String(checksumBase64(digest)), Metadata: map[string]string{digestMetadataKey: digest},
	})
	if err != nil {
		return manifest.Receipt{}, mapError(err)
	}
	versionID := aws.ToString(output.VersionId)
	if versionID == "" {
		return manifest.Receipt{}, errors.New("S3 immutable write has no exact version receipt")
	}
	receipt, err := client.head(ctx, key, versionID)
	if err != nil {
		return manifest.Receipt{}, err
	}
	if receipt.ChecksumSHA256 != digest || receipt.SizeBytes != size || receipt.ETag != strings.Trim(aws.ToString(output.ETag), "\"") {
		return manifest.Receipt{}, errors.New("S3 immutable write readback mismatch")
	}
	if err := client.verify(ctx, receipt); err != nil {
		return manifest.Receipt{}, err
	}
	return receipt, nil
}

func (client *Client) download(ctx context.Context, receipt manifest.Receipt, filePath string) error {
	if !receipt.Valid() || receipt.Bucket != client.config.Bucket {
		return errors.New("S3 download receipt is invalid")
	}
	output, err := client.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(client.config.Bucket), Key: aws.String(receipt.Key), VersionId: aws.String(receipt.VersionID),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return mapError(err)
	}
	if output.Body == nil {
		return errors.New("S3 object body is absent")
	}
	defer output.Body.Close()
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("create bounded S3 download file")
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(output.Body, receipt.SizeBytes+1))
	closeErr := file.Close()
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if copyErr != nil || closeErr != nil || size != receipt.SizeBytes || digest != receipt.ChecksumSHA256 ||
		aws.ToString(output.VersionId) != receipt.VersionID || strings.Trim(aws.ToString(output.ETag), "\"") != receipt.ETag {
		_ = os.Remove(filePath)
		return errors.New("S3 object content readback mismatch")
	}
	return nil
}

func (client *Client) verify(ctx context.Context, receipt manifest.Receipt) error {
	output, err := client.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(client.config.Bucket), Key: aws.String(receipt.Key), VersionId: aws.String(receipt.VersionID),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return mapError(err)
	}
	if output.Body == nil {
		return errors.New("S3 verification body is absent")
	}
	defer output.Body.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(output.Body, receipt.SizeBytes+1))
	if err != nil || size != receipt.SizeBytes ||
		"sha256:"+hex.EncodeToString(hash.Sum(nil)) != receipt.ChecksumSHA256 ||
		aws.ToString(output.VersionId) != receipt.VersionID || strings.Trim(aws.ToString(output.ETag), "\"") != receipt.ETag {
		return errors.New("S3 independent content verification failed")
	}
	return nil
}

func (client *Client) readBytes(ctx context.Context, receipt manifest.Receipt, maximumBytes int64) ([]byte, error) {
	if receipt.SizeBytes > maximumBytes {
		return nil, errors.New("S3 readback exceeds the configured boundary")
	}
	output, err := client.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(client.config.Bucket),
		Key: aws.String(receipt.Key), VersionId: aws.String(receipt.VersionID), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return nil, mapError(err)
	}
	if output.Body == nil {
		return nil, errors.New("S3 readback body is absent")
	}
	defer output.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(output.Body, maximumBytes+1))
	if err != nil || int64(len(payload)) != receipt.SizeBytes ||
		aws.ToString(output.VersionId) != receipt.VersionID ||
		strings.Trim(aws.ToString(output.ETag), "\"") != receipt.ETag {
		return nil, errors.New("S3 readback size mismatch")
	}
	digest := sha256.Sum256(payload)
	if "sha256:"+hex.EncodeToString(digest[:]) != receipt.ChecksumSHA256 {
		return nil, errors.New("S3 readback digest mismatch")
	}
	return payload, nil
}

func decodeJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("immutable S3 document has trailing content")
	}
	return nil
}

func checksumBase64(digest string) string {
	value, _ := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	return base64.StdEncoding.EncodeToString(value)
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func mapError(err error) error {
	var responseError *smithyhttp.ResponseError
	if errors.As(err, &responseError) {
		switch responseError.HTTPStatusCode() {
		case http.StatusPreconditionFailed, http.StatusConflict:
			return ErrConflict
		case http.StatusNotFound:
			return ErrNotFound
		}
	}
	return errors.New("S3 operation failed")
}
