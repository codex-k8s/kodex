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
)

const (
	maximumCredentialBytes = 16 << 10
	maximumArchiveBytes    = int64(64 << 30)
	maximumEntries         = 1_000_000
	archiveObjectRetention = 30 * 24 * time.Hour
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
	Reference string
	SHA256    string
	VersionID string
	Size      int64
	Metadata  map[string]string
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
) (Result, error) {
	if execution.Validate() != nil || !filepath.IsAbs(source) {
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
		execution.SessionID, execution.ID, shaHex+".tar.gz")
	file, err := os.Open(temporaryPath)
	if err != nil {
		return Result{}, errors.New("open temporary runtime archive")
	}
	defer file.Close()
	checksum := base64.StdEncoding.EncodeToString(digest.Sum(nil))
	metadata := map[string]string{
		"execution-id": execution.ID, "runtime-revision-sha256": execution.RuntimeRevisionSHA256,
		"immutable-input-sha256": execution.ImmutableInputSHA256,
	}
	result := Result{SHA256: shaHex, Size: size, Metadata: metadata}
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
		ObjectLockRetainUntilDate: aws.Time(time.Now().UTC().Add(archiveObjectRetention)),
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
	if err := store.verifyHead(ctx, result); err != nil {
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
		execution.SessionID, execution.ID, proofSHA256+".json")
	proofMetadata := map[string]string{
		"execution-id": execution.ID, "archive-sha256": expectedSHA256,
		"runtime-revision-sha256": execution.RuntimeRevisionSHA256,
	}
	result := Result{SHA256: proofSHA256, Size: int64(len(proofRaw)), Metadata: proofMetadata}
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
		ObjectLockRetainUntilDate: aws.Time(time.Now().UTC().Add(archiveObjectRetention)),
	})
	cancel()
	if err != nil || put.VersionId == nil || !validVersionID(*put.VersionId) {
		return Result{}, errors.New("upload versioned restore proof")
	}
	result.VersionID = *put.VersionId
	result.Reference = buildReference(store.bucket, proofKey, result.VersionID)
	if err := store.verifyHead(ctx, result); err != nil {
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
		pvcUID == "" || target.RestoreSourceExecutionID != source.ID ||
		target.RestoreSourceArchiveReference != source.ArchiveReference ||
		target.RestoreSourceArchiveSHA256 != source.ArchiveSHA256 {
		return Result{}, errs.ErrInvalidInput
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return Result{}, errors.New("read rehydrate target")
	}
	for _, entry := range entries {
		if entry.Name() != "lost+found" || !entry.IsDir() {
			return Result{}, errors.New("rehydrate target is not empty")
		}
	}
	archivePath, err := store.downloadExactArchive(
		ctx, source, source.ArchiveReference, source.ArchiveSHA256,
	)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.Remove(archivePath) }()
	if err := extractArchive(archivePath, destination); err != nil {
		return Result{}, err
	}
	treeDigest, err := treeSHA256(destination)
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
		ArchiveVersionID: versionID, RestoredTreeSHA256: treeDigest, PVCUID: pvcUID}
	raw, err := json.Marshal(proof)
	if err != nil {
		return Result{}, errors.New("encode rehydrate proof")
	}
	digest := sha256.Sum256(raw)
	return Result{Reference: "journal://" + target.ID + "/rehydrate-proof",
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
	if hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 || object.VersionId == nil ||
		!validVersionID(*object.VersionId) || *object.VersionId != versionID ||
		object.ChecksumSHA256 == nil || *object.ChecksumSHA256 != base64.StdEncoding.EncodeToString(digest.Sum(nil)) ||
		!metadataMatches(object.Metadata, map[string]string{
			"execution-id": execution.ID, "runtime-revision-sha256": execution.RuntimeRevisionSHA256,
			"immutable-input-sha256": execution.ImmutableInputSHA256,
		}) {
		return "", errs.ErrArchiveUnverified
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
		head.ObjectLockMode != types.ObjectLockModeCompliance ||
		!metadataMatches(head.Metadata, expected.Metadata) {
		return Result{}, false, errs.ErrArchiveUnverified
	}
	expected.VersionID = *head.VersionId
	expected.Reference = buildReference(store.bucket, key, expected.VersionID)
	return expected, true, nil
}

func (store *Store) verifyHead(ctx context.Context, result Result) error {
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
		head.ObjectLockMode != types.ObjectLockModeCompliance ||
		!metadataMatches(head.Metadata, result.Metadata) {
		return errs.ErrArchiveUnverified
	}
	return nil
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

func writeDeterministicArchive(destination io.Writer, root string) (int64, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("runtime archive root is unsafe")
	}
	var paths []string
	var totalInputBytes int64
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		if len(paths) >= maximumEntries {
			return errors.New("runtime archive has too many entries")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket) != 0 {
			return errors.New("runtime archive contains unsupported entry")
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return errors.New("runtime archive contains unsupported entry")
		}
		if info.Mode().IsRegular() {
			if info.Size() < 0 || info.Size() > maximumArchiveBytes-totalInputBytes {
				return errors.New("runtime archive input exceeds size limit")
			}
			totalInputBytes += info.Size()
		}
		paths = append(paths, current)
		return nil
	})
	if err != nil {
		return 0, errors.New("inspect runtime archive tree")
	}
	sort.Strings(paths)
	counter := &countingWriter{writer: destination}
	gzipWriter, err := gzip.NewWriterLevel(counter, gzip.BestCompression)
	if err != nil {
		return 0, errors.New("create runtime archive compressor")
	}
	gzipWriter.Header = gzip.Header{ModTime: time.Unix(0, 0).UTC(), OS: 255}
	tarWriter := tar.NewWriter(gzipWriter)
	for _, current := range paths {
		info, err := os.Lstat(current)
		if err != nil {
			return 0, errors.New("restat runtime archive entry")
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			return 0, errors.New("runtime archive path is unsafe")
		}
		name := filepath.ToSlash(relative)
		if info.IsDir() {
			name += "/"
		}
		header := &tar.Header{Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(),
			ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Unix(0, 0).UTC(), ChangeTime: time.Unix(0, 0).UTC(),
			Uid: 0, Gid: 0, Format: tar.FormatPAX}
		if info.IsDir() {
			header.Typeflag, header.Size = tar.TypeDir, 0
		} else {
			header.Typeflag = tar.TypeReg
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return 0, errors.New("write runtime archive header")
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(current)
		if err != nil {
			return 0, errors.New("open runtime archive entry")
		}
		openedInfo, statErr := file.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			return 0, errors.New("runtime archive entry changed during snapshot")
		}
		copied, copyErr := io.CopyN(tarWriter, file, info.Size())
		closedInfo, finalStatErr := file.Stat()
		closeErr := file.Close()
		if copyErr != nil || finalStatErr != nil || closeErr != nil || copied != info.Size() ||
			!os.SameFile(openedInfo, closedInfo) || closedInfo.Size() != openedInfo.Size() ||
			!closedInfo.ModTime().Equal(openedInfo.ModTime()) {
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
