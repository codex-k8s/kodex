// Package archive реализует content-addressed S3 archive и независимый restore proof.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
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
	"github.com/aws/smithy-go"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"golang.org/x/sys/unix"
)

const (
	maximumCredentialBytes  = 16 << 10
	maximumArchiveBytes     = int64(64 << 30)
	maximumEntries          = 1_000_000
	minimumArchiveRetention = 90 * 24 * time.Hour
)

type Config struct {
	Endpoint, Bucket, Region, TLSServerName, CAFile string
	AccessKeyIDFile, SecretAccessKeyFile            string
	SessionTokenFile                                string
	RequestTimeout                                  time.Duration
}

type Store struct {
	client  *s3.Client
	bucket  string
	timeout time.Duration
}

type Result struct {
	Reference        string
	SHA256           string
	VersionID        string
	ObjectKey        string
	KMSKeyARN        string
	ObjectLockMode   string
	ProvenanceSHA256 string
	Size             int64
	Metadata         map[string]string
	RetainUntil      time.Time
}

type SnapshotProvenance struct {
	SnapshotPVCUID, SourcePVCUID, SourcePVCResourceVersion string
}

type restoreProof struct {
	Schema             string `json:"schema"`
	ExecutionID        string `json:"execution_id"`
	ArchiveReference   string `json:"archive_reference"`
	ArchiveSHA256      string `json:"archive_sha256"`
	ArchiveVersionID   string `json:"archive_version_id"`
	RestoredTreeSHA256 string `json:"restored_tree_sha256"`
}

type rehydrateProof struct {
	Schema, SourceExecutionID, TargetExecutionID, ArchiveReference string
	ArchiveSHA256, ArchiveVersionID, RestoredTreeSHA256, PVCUID    string
	AssignmentGeneration                                           uint64
}

const rehydrateMarkerName = ".mattercodex-rehydrate-proof.json"
const rehydrateOwnerName = ".mattercodex-rehydrate-owner.json"

type rehydrateOwner struct {
	Schema               string `json:"schema"`
	SourceExecutionID    string `json:"source_execution_id"`
	TargetExecutionID    string `json:"target_execution_id"`
	PVCUID               string `json:"pvc_uid"`
	AssignmentGeneration uint64 `json:"assignment_generation"`
}

func Open(ctx context.Context, config Config) (*Store, error) {
	if config.Endpoint == "" || config.Bucket == "" || config.Region == "" ||
		config.TLSServerName == "" || config.CAFile == "" ||
		config.AccessKeyIDFile == "" || config.SecretAccessKeyFile == "" ||
		config.SessionTokenFile == "" ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 30*time.Second {
		return nil, errors.New("s3 archive configuration is invalid")
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Path != "" {
		return nil, errors.New("s3 archive endpoint is invalid")
	}
	accessKey, err := readCredential(config.AccessKeyIDFile)
	if err != nil {
		return nil, err
	}
	secretKey, err := readCredential(config.SecretAccessKeyFile)
	if err != nil {
		return nil, err
	}
	sessionToken, err := readCredential(config.SessionTokenFile)
	if err != nil {
		return nil, err
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read S3 CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse S3 CA")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName, RootCAs: roots}
	transport.Proxy = nil
	loaded, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(config.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)),
		awsconfig.WithHTTPClient(&http.Client{Transport: transport, Timeout: config.RequestTimeout}),
	)
	if err != nil {
		return nil, errors.New("load S3 client configuration")
	}
	client := s3.NewFromConfig(loaded, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(config.Endpoint)
		options.UsePathStyle = true
	})
	store := &Store{client: client, bucket: config.Bucket, timeout: config.RequestTimeout}
	checkCtx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	versioning, err := store.client.GetBucketVersioning(checkCtx, &s3.GetBucketVersioningInput{Bucket: aws.String(config.Bucket)})
	if err != nil || versioning.Status != types.BucketVersioningStatusEnabled {
		return nil, errors.New("s3 archive bucket versioning is not enabled")
	}
	objectLock, err := store.client.GetObjectLockConfiguration(
		checkCtx, &s3.GetObjectLockConfigurationInput{Bucket: aws.String(config.Bucket)},
	)
	if err != nil || objectLock.ObjectLockConfiguration == nil ||
		objectLock.ObjectLockConfiguration.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		return nil, errors.New("s3 archive Object Lock is not enabled")
	}
	encryption, err := store.client.GetBucketEncryption(
		checkCtx, &s3.GetBucketEncryptionInput{Bucket: aws.String(config.Bucket)},
	)
	if err != nil || encryption.ServerSideEncryptionConfiguration == nil ||
		!kmsEncryptionConfigured(encryption.ServerSideEncryptionConfiguration.Rules) {
		return nil, errors.New("s3 archive KMS encryption is not enabled")
	}
	publicAccess, err := store.client.GetPublicAccessBlock(
		checkCtx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(config.Bucket)},
	)
	if err != nil || publicAccess.PublicAccessBlockConfiguration == nil ||
		!aws.ToBool(publicAccess.PublicAccessBlockConfiguration.BlockPublicAcls) ||
		!aws.ToBool(publicAccess.PublicAccessBlockConfiguration.BlockPublicPolicy) ||
		!aws.ToBool(publicAccess.PublicAccessBlockConfiguration.IgnorePublicAcls) ||
		!aws.ToBool(publicAccess.PublicAccessBlockConfiguration.RestrictPublicBuckets) {
		return nil, errors.New("s3 archive public access block is not enforced")
	}
	if _, err := store.client.ListObjectsV2(checkCtx, &s3.ListObjectsV2Input{
		Bucket: aws.String(config.Bucket), MaxKeys: aws.Int32(1),
	}); !accessDenied(err) {
		return nil, errors.New("s3 archive identity can list bucket")
	}
	return store, nil
}

func accessDenied(err error) bool {
	var apiError smithy.APIError
	return errors.As(err, &apiError) &&
		(apiError.ErrorCode() == "AccessDenied" || apiError.ErrorCode() == "Forbidden")
}

func kmsEncryptionConfigured(rules []types.ServerSideEncryptionRule) bool {
	for _, rule := range rules {
		if rule.ApplyServerSideEncryptionByDefault != nil &&
			rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm == types.ServerSideEncryptionAwsKms {
			return true
		}
	}
	return false
}

func (store *Store) Archive(
	ctx context.Context,
	source string,
	execution entity.Execution,
	provenance SnapshotProvenance,
) (Result, error) {
	if execution.Validate() != nil || !filepath.IsAbs(source) ||
		!validPinnedRetention(execution, time.Now().UTC()) ||
		provenance.SnapshotPVCUID == "" || provenance.SourcePVCUID == "" ||
		provenance.SourcePVCResourceVersion == "" {
		return Result{}, errs.ErrInvalidInput
	}
	temporary, err := os.CreateTemp("", "mattercodex-runtime-archive-*.tar.gz")
	if err != nil {
		return Result{}, errors.New("create temporary runtime archive")
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	digest := sha256.New()
	size, err := writeDeterministicArchive(io.MultiWriter(temporary, digest), source)
	closeErr := temporary.Close()
	if err != nil {
		return Result{}, err
	}
	if closeErr != nil {
		return Result{}, errors.New("close temporary runtime archive")
	}
	shaHex := hex.EncodeToString(digest.Sum(nil))
	key := path.Join("runtime", execution.OrganizationID, execution.ProjectID,
		execution.SessionID, execution.ID, "archive.tar.gz")
	file, err := os.Open(temporaryPath)
	if err != nil {
		return Result{}, errors.New("open temporary runtime archive")
	}
	defer file.Close()
	checksum := base64.StdEncoding.EncodeToString(digest.Sum(nil))
	metadata := map[string]string{
		"execution-id": execution.ID, "runtime-revision-sha256": execution.RuntimeRevisionSHA256,
		"immutable-input-sha256":      execution.ImmutableInputSHA256,
		"snapshot-pvc-uid":            provenance.SnapshotPVCUID,
		"source-pvc-uid":              provenance.SourcePVCUID,
		"source-pvc-resource-version": provenance.SourcePVCResourceVersion,
	}
	result := Result{SHA256: shaHex, Size: size, Metadata: metadata, RetainUntil: execution.ArchiveRetainUntil}
	if existing, found, err := store.existingObject(ctx, key, result); err != nil {
		return Result{}, err
	} else if found {
		return existing, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, store.timeout)
	put, err := store.client.PutObject(requestCtx, &s3.PutObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key), Body: file,
		ContentLength: aws.Int64(size), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		ChecksumSHA256: aws.String(checksum), ContentType: aws.String("application/gzip"),
		Metadata: metadata, ServerSideEncryption: types.ServerSideEncryptionAwsKms,
		ObjectLockMode:            types.ObjectLockModeCompliance,
		ObjectLockRetainUntilDate: aws.Time(execution.ArchiveRetainUntil),
	})
	cancel()
	if err != nil {
		return Result{}, errors.New("upload runtime archive")
	}
	if put.VersionId == nil || !validVersionID(*put.VersionId) {
		return Result{}, errors.New("s3 archive version is missing")
	}
	result.VersionID = *put.VersionId
	result.Reference = buildReference(store.bucket, key, result.VersionID)
	result.ObjectKey = key
	if err := store.verifyHead(ctx, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (store *Store) RestoreAndProve(
	ctx context.Context,
	execution entity.Execution,
	reference, expectedSHA256 string,
) (Result, error) {
	_, _, versionID, err := parseReference(reference)
	if err != nil || len(expectedSHA256) != sha256.Size*2 {
		return Result{}, errs.ErrArchiveUnverified
	}
	archivePath, err := store.downloadExactArchive(ctx, execution, reference, expectedSHA256)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.Remove(archivePath) }()
	restoreDir, err := os.MkdirTemp("", "mattercodex-runtime-restore-tree-*")
	if err != nil {
		return Result{}, errors.New("create temporary restore tree")
	}
	defer func() { _ = os.RemoveAll(restoreDir) }()
	if err := extractArchive(archivePath, restoreDir); err != nil {
		return Result{}, err
	}
	treeDigest, err := treeSHA256(restoreDir)
	if err != nil {
		return Result{}, err
	}
	proof := restoreProof{Schema: "mattercodex.runtime-restore-proof.v1", ExecutionID: execution.ID,
		ArchiveReference: reference, ArchiveSHA256: expectedSHA256, ArchiveVersionID: versionID,
		RestoredTreeSHA256: treeDigest}
	proofRaw, err := json.Marshal(proof)
	if err != nil {
		return Result{}, errors.New("encode restore proof")
	}
	proofDigest := sha256.Sum256(proofRaw)
	proofSHA256 := hex.EncodeToString(proofDigest[:])
	proofKey := path.Join("runtime-restore-proof", execution.OrganizationID, execution.ProjectID,
		execution.SessionID, execution.ID, "restore-proof.json")
	proofMetadata := map[string]string{
		"execution-id": execution.ID, "archive-sha256": expectedSHA256,
		"runtime-revision-sha256": execution.RuntimeRevisionSHA256,
	}
	result := Result{SHA256: proofSHA256, Size: int64(len(proofRaw)), Metadata: proofMetadata,
		RetainUntil: execution.ArchiveRetainUntil}
	if existing, found, err := store.existingObject(ctx, proofKey, result); err != nil {
		return Result{}, err
	} else if found {
		return existing, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, store.timeout)
	put, err := store.client.PutObject(requestCtx, &s3.PutObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(proofKey), Body: strings.NewReader(string(proofRaw)),
		ContentLength: aws.Int64(int64(len(proofRaw))), ContentType: aws.String("application/json"),
		ChecksumAlgorithm:         types.ChecksumAlgorithmSha256,
		ChecksumSHA256:            aws.String(base64.StdEncoding.EncodeToString(proofDigest[:])),
		Metadata:                  proofMetadata,
		ServerSideEncryption:      types.ServerSideEncryptionAwsKms,
		ObjectLockMode:            types.ObjectLockModeCompliance,
		ObjectLockRetainUntilDate: aws.Time(execution.ArchiveRetainUntil),
	})
	cancel()
	if err != nil || put.VersionId == nil || !validVersionID(*put.VersionId) {
		return Result{}, errors.New("upload versioned restore proof")
	}
	result.VersionID = *put.VersionId
	result.Reference = buildReference(store.bucket, proofKey, result.VersionID)
	result.ObjectKey = proofKey
	if err := store.verifyHead(ctx, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// RestoreToAndProve восстанавливает exact version в пустой целевой PVC и
// возвращает digest доказательства, связанного с UID этого PVC.
func (store *Store) RestoreToAndProve(
	ctx context.Context,
	source, target entity.Execution,
	destination, pvcUID string,
) (Result, error) {
	if source.Validate() != nil || target.Validate() != nil || !filepath.IsAbs(destination) ||
		target.RestoreAssignmentState != "BOUND" || pvcUID == "" ||
		pvcUID != target.RestoreTargetPVCUID || target.RestoreSourceExecutionID != source.ID ||
		target.RestoreSourceArchiveReference != source.ArchiveReference ||
		target.RestoreSourceArchiveSHA256 != source.ArchiveSHA256 ||
		target.RestoreSourceVersion != source.Version ||
		target.RestoreSourceArchiveObjectKey != source.ArchiveObjectKey ||
		target.RestoreSourceArchiveVersionID != source.ArchiveVersionID ||
		target.RestoreSourceArchiveKMSKeyARN != source.ArchiveKMSKeyARN ||
		target.RestoreSourceArchiveObjectLockMode != source.ArchiveObjectLockMode ||
		!target.RestoreSourceArchiveRetainUntil.Equal(source.ArchiveRetainUntil) ||
		target.RestoreSourceRetentionPolicyID != source.RetentionPolicyID ||
		target.RestoreSourceRetentionPolicyVersion != source.RetentionPolicyVersion ||
		target.RestoreSourceProvenanceSHA256 != source.ArchiveProvenanceSHA256 {
		return Result{}, errs.ErrInvalidInput
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return Result{}, errors.New("read rehydrate target")
	}
	stagingName := ".mattercodex-restore-" + target.ID + "-" + pvcUID + ".staging"
	finalName := "session"
	for _, entry := range entries {
		if (entry.Name() != "lost+found" || !entry.IsDir()) &&
			entry.Name() != stagingName && entry.Name() != finalName {
			return Result{}, errors.New("rehydrate target is not empty")
		}
	}
	staging := filepath.Join(destination, stagingName)
	final := filepath.Join(destination, finalName)
	if existing, markerErr := readRehydrateMarker(final); markerErr == nil {
		return rejoinRehydrateProof(source, target, pvcUID, final, existing)
	} else if finalInfo, statErr := os.Lstat(final); statErr == nil {
		owner, ownerErr := readRehydrateOwner(final)
		if ownerErr != nil || !finalInfo.IsDir() || finalInfo.Mode()&os.ModeSymlink != 0 ||
			owner.SourceExecutionID != source.ID || owner.TargetExecutionID != target.ID ||
			owner.PVCUID != pvcUID || owner.AssignmentGeneration != target.RestoreAssignmentGeneration {
			return Result{}, errs.ErrArchiveUnverified
		}
		// Только exact-owned incomplete generation можно удалить. Чужая либо
		// недоказанная final tree всегда вызывает закрытый отказ.
		if err := os.RemoveAll(final); err != nil || syncDirectory(destination) != nil {
			return Result{}, errors.New("remove incomplete rehydrate generation")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Result{}, errors.New("inspect rehydrate publication")
	} else if !errors.Is(markerErr, os.ErrNotExist) {
		return Result{}, markerErr
	}
	if err := os.RemoveAll(staging); err != nil || os.Mkdir(staging, 0o750) != nil {
		return Result{}, errors.New("prepare rehydrate staging")
	}
	defer func() { _ = os.RemoveAll(staging) }()
	ownerRaw, err := json.Marshal(rehydrateOwner{
		Schema: "mattercodex.runtime-rehydrate-owner.v1", SourceExecutionID: source.ID,
		TargetExecutionID: target.ID, PVCUID: pvcUID,
		AssignmentGeneration: target.RestoreAssignmentGeneration,
	})
	if err != nil || os.WriteFile(filepath.Join(staging, rehydrateOwnerName), ownerRaw, 0o400) != nil {
		return Result{}, errors.New("write rehydrate owner")
	}
	archivePath, err := store.downloadExactArchive(
		ctx, source, source.ArchiveReference, source.ArchiveSHA256,
	)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.Remove(archivePath) }()
	if err := extractArchive(archivePath, staging); err != nil {
		return Result{}, err
	}
	// Один canonical entry set используется до публикации и при rejoin после
	// потерянного ответа: оба служебных marker-файла исключены.
	treeDigest, err := restoredTreeSHA256(staging)
	if err != nil {
		return Result{}, err
	}
	_, _, versionID, err := parseReference(source.ArchiveReference)
	if err != nil {
		return Result{}, err
	}
	proof := rehydrateProof{Schema: "mattercodex.runtime-rehydrate-proof.v1",
		SourceExecutionID: source.ID, TargetExecutionID: target.ID,
		ArchiveReference: source.ArchiveReference, ArchiveSHA256: source.ArchiveSHA256,
		ArchiveVersionID: versionID, RestoredTreeSHA256: treeDigest, PVCUID: pvcUID,
		AssignmentGeneration: target.RestoreAssignmentGeneration}
	raw, err := json.Marshal(proof)
	if err != nil {
		return Result{}, errors.New("encode rehydrate proof")
	}
	if err := os.WriteFile(filepath.Join(staging, rehydrateMarkerName), raw, 0o400); err != nil {
		return Result{}, errors.New("write rehydrate marker")
	}
	if err := syncTree(ctx, staging); err != nil {
		return Result{}, errors.New("sync rehydrate staging")
	}
	if err := syncDirectory(destination); err != nil {
		return Result{}, errors.New("sync rehydrate parent before publication")
	}
	if err := os.Rename(staging, final); err != nil {
		return Result{}, errors.New("publish rehydrate tree")
	}
	if err := syncDirectory(destination); err != nil {
		return Result{}, errors.New("sync rehydrate publication")
	}
	return rehydrateProofResult(proof)
}

func rejoinRehydrateProof(
	source, target entity.Execution,
	pvcUID, final string,
	existing rehydrateProof,
) (Result, error) {
	if existing.SourceExecutionID != source.ID || existing.TargetExecutionID != target.ID ||
		existing.ArchiveReference != source.ArchiveReference || existing.ArchiveSHA256 != source.ArchiveSHA256 ||
		existing.PVCUID != pvcUID || existing.AssignmentGeneration != target.RestoreAssignmentGeneration {
		return Result{}, errs.ErrArchiveUnverified
	}
	actualTreeSHA256, digestErr := restoredTreeSHA256(final)
	if digestErr != nil || actualTreeSHA256 != existing.RestoredTreeSHA256 {
		return Result{}, errs.ErrArchiveUnverified
	}
	return rehydrateProofResult(existing)
}

func readRehydrateOwner(final string) (rehydrateOwner, error) {
	raw, err := os.ReadFile(filepath.Join(final, rehydrateOwnerName))
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return rehydrateOwner{}, errs.ErrArchiveUnverified
	}
	var owner rehydrateOwner
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&owner) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		owner.Schema != "mattercodex.runtime-rehydrate-owner.v1" ||
		owner.SourceExecutionID == "" || owner.TargetExecutionID == "" || owner.PVCUID == "" ||
		owner.AssignmentGeneration == 0 {
		return rehydrateOwner{}, errs.ErrArchiveUnverified
	}
	return owner, nil
}

func syncTree(ctx context.Context, root string) error {
	directories := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errs.ErrArchiveUnverified
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return errs.ErrArchiveUnverified
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func readRehydrateMarker(final string) (rehydrateProof, error) {
	info, err := os.Lstat(final)
	if err != nil {
		return rehydrateProof{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return rehydrateProof{}, errs.ErrArchiveUnverified
	}
	raw, err := os.ReadFile(filepath.Join(final, rehydrateMarkerName))
	if errors.Is(err, os.ErrNotExist) {
		return rehydrateProof{}, os.ErrNotExist
	}
	if err != nil || len(raw) == 0 || len(raw) > 8192 {
		return rehydrateProof{}, errs.ErrArchiveUnverified
	}
	var proof rehydrateProof
	if json.Unmarshal(raw, &proof) != nil || proof.Schema != "mattercodex.runtime-rehydrate-proof.v1" {
		return rehydrateProof{}, errs.ErrArchiveUnverified
	}
	return proof, nil
}

func rehydrateProofResult(proof rehydrateProof) (Result, error) {
	raw, err := json.Marshal(proof)
	if err != nil {
		return Result{}, errors.New("encode rehydrate proof")
	}
	digest := sha256.Sum256(raw)
	return Result{Reference: "journal://" + proof.TargetExecutionID + "/rehydrate-proof",
		SHA256: hex.EncodeToString(digest[:]), Size: int64(len(raw))}, nil
}

func (store *Store) downloadExactArchive(
	ctx context.Context,
	execution entity.Execution,
	reference, expectedSHA256 string,
) (string, error) {
	bucket, key, versionID, err := parseReference(reference)
	if err != nil || bucket != store.bucket || len(expectedSHA256) != sha256.Size*2 {
		return "", errs.ErrArchiveUnverified
	}
	temporary, err := os.CreateTemp("", "mattercodex-runtime-restore-*.tar.gz")
	if err != nil {
		return "", errors.New("create temporary restore archive")
	}
	archivePath := temporary.Name()
	failed := true
	defer func() {
		if failed {
			_ = os.Remove(archivePath)
		}
	}()
	requestCtx, cancel := context.WithTimeout(ctx, store.timeout)
	object, err := store.client.GetObject(requestCtx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(versionID),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		cancel()
		_ = temporary.Close()
		return "", errors.New("download exact runtime archive")
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(object.Body, maximumArchiveBytes+1))
	closeBodyErr := object.Body.Close()
	cancel()
	closeFileErr := temporary.Close()
	if copyErr != nil || closeBodyErr != nil || closeFileErr != nil || written > maximumArchiveBytes {
		return "", errors.New("read exact runtime archive")
	}
	if key != execution.ArchiveObjectKey || versionID != execution.ArchiveVersionID ||
		execution.ArchiveObjectLockMode != "COMPLIANCE" ||
		hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 || object.VersionId == nil ||
		!validVersionID(*object.VersionId) || *object.VersionId != versionID ||
		object.ChecksumSHA256 == nil || *object.ChecksumSHA256 != base64.StdEncoding.EncodeToString(digest.Sum(nil)) ||
		object.ServerSideEncryption != types.ServerSideEncryptionAwsKms ||
		object.SSEKMSKeyId == nil || *object.SSEKMSKeyId != execution.ArchiveKMSKeyARN ||
		object.ObjectLockMode != types.ObjectLockModeCompliance || object.ObjectLockRetainUntilDate == nil ||
		!object.ObjectLockRetainUntilDate.Equal(execution.ArchiveRetainUntil) ||
		!metadataContains(object.Metadata, map[string]string{
			"execution-id": execution.ID, "runtime-revision-sha256": execution.RuntimeRevisionSHA256,
			"immutable-input-sha256": execution.ImmutableInputSHA256,
		}) {
		return "", errs.ErrArchiveUnverified
	}
	if archiveProvenanceSHA256(Result{
		Reference: reference, SHA256: expectedSHA256, VersionID: versionID,
		ObjectKey: key, KMSKeyARN: *object.SSEKMSKeyId, ObjectLockMode: "COMPLIANCE",
		RetainUntil: execution.ArchiveRetainUntil, Metadata: object.Metadata,
	}) != execution.ArchiveProvenanceSHA256 {
		return "", errs.ErrArchiveUnverified
	}
	if err := store.verifyRetention(ctx, bucket, key, versionID, execution.ArchiveRetainUntil); err != nil {
		return "", err
	}
	failed = false
	return archivePath, nil
}

func (store *Store) existingObject(
	ctx context.Context,
	key string,
	expected Result,
) (Result, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	head, err := store.client.HeadObject(requestCtx, &s3.HeadObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && (apiError.ErrorCode() == "NotFound" || apiError.ErrorCode() == "NoSuchKey") {
			return Result{}, false, nil
		}
		return Result{}, false, errors.New("read existing content-addressed S3 object")
	}
	if head.VersionId == nil || !validVersionID(*head.VersionId) || head.ContentLength == nil ||
		*head.ContentLength != expected.Size || head.ChecksumSHA256 == nil ||
		*head.ChecksumSHA256 != base64Digest(expected.SHA256) ||
		head.ServerSideEncryption != types.ServerSideEncryptionAwsKms ||
		head.SSEKMSKeyId == nil || !strings.HasPrefix(*head.SSEKMSKeyId, "arn:") ||
		head.ObjectLockMode != types.ObjectLockModeCompliance ||
		head.ObjectLockRetainUntilDate == nil || !head.ObjectLockRetainUntilDate.Equal(expected.RetainUntil) ||
		!metadataMatches(head.Metadata, expected.Metadata) {
		return Result{}, false, errs.ErrArchiveUnverified
	}
	expected.VersionID = *head.VersionId
	expected.ObjectKey = key
	expected.KMSKeyARN = *head.SSEKMSKeyId
	expected.ObjectLockMode = "COMPLIANCE"
	expected.Reference = buildReference(store.bucket, key, expected.VersionID)
	expected.ProvenanceSHA256 = archiveProvenanceSHA256(expected)
	if err := store.verifyRetention(ctx, store.bucket, key, expected.VersionID, expected.RetainUntil); err != nil {
		return Result{}, false, err
	}
	return expected, true, nil
}

func (store *Store) verifyHead(ctx context.Context, result *Result) error {
	if result == nil {
		return errs.ErrArchiveUnverified
	}
	bucket, key, versionID, err := parseReference(result.Reference)
	if err != nil || bucket != store.bucket || versionID != result.VersionID {
		return errs.ErrArchiveUnverified
	}
	requestCtx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	head, err := store.client.HeadObject(requestCtx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(versionID), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil || head.VersionId == nil || !validVersionID(*head.VersionId) || *head.VersionId != versionID ||
		head.ContentLength == nil || *head.ContentLength != result.Size ||
		head.ChecksumSHA256 == nil || *head.ChecksumSHA256 != base64Digest(result.SHA256) ||
		head.ServerSideEncryption != types.ServerSideEncryptionAwsKms ||
		head.SSEKMSKeyId == nil || !strings.HasPrefix(*head.SSEKMSKeyId, "arn:") ||
		head.ObjectLockMode != types.ObjectLockModeCompliance ||
		head.ObjectLockRetainUntilDate == nil || !head.ObjectLockRetainUntilDate.Equal(result.RetainUntil) ||
		!metadataMatches(head.Metadata, result.Metadata) {
		return errs.ErrArchiveUnverified
	}
	result.ObjectKey = key
	result.KMSKeyARN = *head.SSEKMSKeyId
	result.ObjectLockMode = "COMPLIANCE"
	result.ProvenanceSHA256 = archiveProvenanceSHA256(*result)
	return store.verifyRetention(ctx, bucket, key, versionID, result.RetainUntil)
}

func archiveProvenanceSHA256(result Result) string {
	raw, err := json.Marshal(struct {
		Reference, SHA256, VersionID, ObjectKey, KMSKeyARN, ObjectLockMode string
		RetainUntil                                                        time.Time
		Metadata                                                           map[string]string
	}{result.Reference, result.SHA256, result.VersionID, result.ObjectKey,
		result.KMSKeyARN, result.ObjectLockMode, result.RetainUntil, result.Metadata})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (store *Store) verifyRetention(
	ctx context.Context,
	bucket, key, versionID string,
	expected time.Time,
) error {
	if expected.IsZero() || !expected.After(time.Now().UTC()) {
		return errs.ErrArchiveUnverified
	}
	requestCtx, cancel := context.WithTimeout(ctx, store.timeout)
	defer cancel()
	readback, err := store.client.GetObjectRetention(requestCtx, &s3.GetObjectRetentionInput{
		Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(versionID),
	})
	if err != nil || readback.Retention == nil ||
		readback.Retention.Mode != types.ObjectLockRetentionModeCompliance ||
		readback.Retention.RetainUntilDate == nil ||
		!readback.Retention.RetainUntilDate.Equal(expected) {
		return errs.ErrArchiveUnverified
	}
	return nil
}

func validPinnedRetention(execution entity.Execution, now time.Time) bool {
	if execution.PVCRetentionSeconds < 86400 ||
		execution.ArchiveRetentionSeconds < uint64(minimumArchiveRetention/time.Second) ||
		execution.PVCCleanupEligibleAt.IsZero() || execution.ArchiveRetainUntil.IsZero() {
		return false
	}
	pinnedAt := execution.PVCCleanupEligibleAt.Add(
		-time.Duration(execution.PVCRetentionSeconds) * time.Second,
	)
	minimumRetainUntil := pinnedAt.Add(
		time.Duration(execution.ArchiveRetentionSeconds) * time.Second,
	)
	return !execution.ArchiveRetainUntil.Before(minimumRetainUntil) &&
		execution.ArchiveRetainUntil.After(now)
}

func metadataMatches(actual, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, value := range expected {
		if actual[strings.ToLower(key)] != value {
			return false
		}
	}
	return true
}

func metadataContains(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[strings.ToLower(key)] != value {
			return false
		}
	}
	return true
}

type archiveEntry struct {
	name string
	stat unix.Stat_t
}

func writeDeterministicArchive(destination io.Writer, root string) (int64, error) {
	return writeDeterministicArchiveExcept(destination, root)
}

func writeDeterministicArchiveExcept(destination io.Writer, root string, ignoredRootEntries ...string) (int64, error) {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return 0, errors.New("open immutable runtime archive root")
	}
	defer unix.Close(rootFD)
	var rootStat unix.Stat_t
	if unix.Fstat(rootFD, &rootStat) != nil || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return 0, errors.New("runtime archive root is unsafe")
	}
	ignored := make(map[string]struct{}, len(ignoredRootEntries))
	for _, entry := range ignoredRootEntries {
		ignored[entry] = struct{}{}
	}
	entries, totalInputBytes, err := collectArchiveEntries(rootFD, "", ignored, nil, 0)
	if err != nil {
		return 0, err
	}
	_ = totalInputBytes
	sort.Slice(entries, func(left, right int) bool { return entries[left].name < entries[right].name })
	counter := &countingWriter{writer: destination}
	gzipWriter, err := gzip.NewWriterLevel(counter, gzip.BestCompression)
	if err != nil {
		return 0, errors.New("create runtime archive compressor")
	}
	gzipWriter.Header = gzip.Header{ModTime: time.Unix(0, 0).UTC(), OS: 255}
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		name := entry.name
		isDirectory := entry.stat.Mode&unix.S_IFMT == unix.S_IFDIR
		if isDirectory {
			name += "/"
		}
		header := &tar.Header{Name: name, Mode: int64(entry.stat.Mode & 0o777), Size: entry.stat.Size,
			ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Unix(0, 0).UTC(), ChangeTime: time.Unix(0, 0).UTC(),
			Uid: 0, Gid: 0, Format: tar.FormatPAX}
		if isDirectory {
			header.Typeflag, header.Size = tar.TypeDir, 0
		} else {
			header.Typeflag = tar.TypeReg
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return 0, errors.New("write runtime archive header")
		}
		if isDirectory {
			continue
		}
		fd, err := openArchiveEntry(rootFD, entry.name)
		if err != nil {
			return 0, errors.New("open runtime archive entry")
		}
		file := os.NewFile(uintptr(fd), entry.name)
		var opened unix.Stat_t
		if unix.Fstat(fd, &opened) != nil || !sameArchiveStat(entry.stat, opened) {
			_ = file.Close()
			return 0, errors.New("runtime archive entry changed during snapshot")
		}
		copied, copyErr := io.CopyN(tarWriter, file, entry.stat.Size)
		var closed unix.Stat_t
		finalStatErr := unix.Fstat(fd, &closed)
		closeErr := file.Close()
		if copyErr != nil || finalStatErr != nil || closeErr != nil || copied != entry.stat.Size ||
			!sameArchiveStat(opened, closed) {
			return 0, errors.New("copy runtime archive entry")
		}
	}
	if err := tarWriter.Close(); err != nil {
		return 0, errors.New("close runtime tar stream")
	}
	if err := gzipWriter.Close(); err != nil {
		return 0, errors.New("close runtime gzip stream")
	}
	if counter.written > maximumArchiveBytes {
		return 0, errors.New("runtime archive exceeds size limit")
	}
	return counter.written, nil
}

func collectArchiveEntries(
	directoryFD int,
	prefix string,
	ignoredRootEntries map[string]struct{},
	entries []archiveEntry,
	total int64,
) ([]archiveEntry, int64, error) {
	duplicate, err := unix.Dup(directoryFD)
	if err != nil {
		return nil, 0, errors.New("duplicate runtime archive directory")
	}
	directory := os.NewFile(uintptr(duplicate), prefix)
	names, err := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if err != nil || closeErr != nil {
		return nil, 0, errors.New("read immutable runtime archive directory")
	}
	sort.Strings(names)
	for _, name := range names {
		if prefix == "" {
			if _, ignored := ignoredRootEntries[name]; ignored {
				continue
			}
		}
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return nil, 0, errors.New("runtime archive path is unsafe")
		}
		if len(entries) >= maximumEntries {
			return nil, 0, errors.New("runtime archive has too many entries")
		}
		fd, err := openArchiveEntry(directoryFD, name)
		if err != nil {
			return nil, 0, errors.New("open immutable runtime archive entry")
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil {
			unix.Close(fd)
			return nil, 0, errors.New("inspect immutable runtime archive entry")
		}
		relative := name
		if prefix != "" {
			relative = prefix + "/" + name
		}
		switch stat.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			entries = append(entries, archiveEntry{name: relative, stat: stat})
			entries, total, err = collectArchiveEntries(fd, relative, ignoredRootEntries, entries, total)
		case unix.S_IFREG:
			if stat.Nlink != 1 || stat.Size < 0 || stat.Size > maximumArchiveBytes-total {
				err = errors.New("runtime archive file is unsafe")
			} else {
				total += stat.Size
				entries = append(entries, archiveEntry{name: relative, stat: stat})
			}
		default:
			err = errors.New("runtime archive contains unsupported entry")
		}
		unix.Close(fd)
		if err != nil {
			return nil, 0, err
		}
	}
	return entries, total, nil
}

func openArchiveEntry(directoryFD int, name string) (int, error) {
	return unix.Openat2(directoryFD, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
}

func sameArchiveStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Nlink == 1 && right.Nlink == 1 && left.Size == right.Size &&
		left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func extractArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return errors.New("open restore archive")
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("open restore gzip stream")
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := 0
	var totalSize int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("read restore tar stream")
		}
		entries++
		if entries > maximumEntries || header.Name == "" || path.IsAbs(header.Name) ||
			path.Clean(header.Name) != strings.TrimSuffix(header.Name, "/") || strings.HasPrefix(header.Name, "../") {
			return errors.New("restore archive path is unsafe")
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		relative, relErr := filepath.Rel(destination, target)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("restore archive path escapes destination")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return errors.New("create restore directory")
			}
		case tar.TypeReg:
			if header.Size < 0 || header.Size > maximumArchiveBytes-totalSize {
				return errors.New("restore entry exceeds size limit")
			}
			totalSize += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return errors.New("create restore parent directory")
			}
			created, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fs.FileMode(header.Mode)&0o750)
			if err != nil {
				return errors.New("create restore file")
			}
			copied, copyErr := io.CopyN(created, tarReader, header.Size)
			closeErr := created.Close()
			if copyErr != nil || closeErr != nil || copied != header.Size {
				return errors.New("write restore file")
			}
		default:
			return errors.New("restore archive entry type is forbidden")
		}
	}
	return nil
}

func treeSHA256(root string) (string, error) {
	hash := sha256.New()
	_, err := writeDeterministicArchive(hash, root)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func restoredTreeSHA256(root string) (string, error) {
	hash := sha256.New()
	_, err := writeDeterministicArchiveExcept(hash, root, rehydrateMarkerName, rehydrateOwnerName)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	written, err := writer.writer.Write(value)
	writer.written += int64(written)
	if writer.written > maximumArchiveBytes {
		return written, errors.New("runtime archive exceeds size limit")
	}
	return written, err
}

func buildReference(bucket, key, versionID string) string {
	query := url.Values{"versionId": []string{versionID}}
	return "s3://" + bucket + "/" + key + "?" + query.Encode()
}

func parseReference(reference string) (string, string, string, error) {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" || parsed.Fragment != "" ||
		parsed.RawQuery == "" || len(parsed.Query()) != 1 || len(parsed.Query()["versionId"]) != 1 {
		return "", "", "", errs.ErrArchiveUnverified
	}
	key := strings.TrimPrefix(parsed.EscapedPath(), "/")
	unescaped, err := url.PathUnescape(key)
	versionID := parsed.Query().Get("versionId")
	if err != nil || unescaped == "" || !validVersionID(versionID) || path.Clean(unescaped) != unescaped || strings.HasPrefix(unescaped, "../") {
		return "", "", "", errs.ErrArchiveUnverified
	}
	return parsed.Host, unescaped, versionID, nil
}

func validVersionID(value string) bool {
	return value != "" && value != "null" && len(value) <= 1024 &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func base64Digest(hexDigest string) string {
	raw, err := hex.DecodeString(hexDigest)
	if err != nil || len(raw) != sha256.Size {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func readCredential(file string) (string, error) {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCredentialBytes || info.Mode().Perm()&0o007 != 0 {
		return "", errors.New("s3 credential file is unsafe")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", errors.New("read S3 credential")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("s3 credential is invalid")
	}
	return value, nil
}
