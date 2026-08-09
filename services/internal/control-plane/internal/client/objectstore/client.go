// Package objectstore реализует service-owned S3 writer для immutable
// InstructionSet и Schedule prompt content. Metadata authority остаётся в control-plane.
package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint, TLSServerName, CAFile                        string
	ClientCertificateFile, ClientPrivateKeyFile            string
	AccessKeyFile, SecretKeyFile, SessionTokenFile, Bucket string
	MaximumObjectBytes                                     int64
	Timeout                                                time.Duration
}

type Client struct {
	config Config
	client *minio.Client
	fence  readinessFence
}

type readinessFence interface {
	WithInstructionObjectReadinessFence(context.Context, func(context.Context) error) error
}

var errReadinessCleanup = errors.New("S3 control-plane content object store readiness cleanup failed")

const (
	readinessObjectPrefix    = "projects/00000000-0000-0000-0000-000000000000/instruction-sets/control-plane-readiness/"
	readinessObjectKey       = readinessObjectPrefix + "probe.md"
	scheduleReadinessPrefix  = "projects/00000000-0000-0000-0000-000000000000/schedule-prompts/control-plane-readiness/"
	scheduleReadinessKey     = scheduleReadinessPrefix + "probe.md"
	readinessMaximumVersions = 32
)

var readinessContent = []byte("# MatterCodex control-plane readiness\n")
var scheduleReadinessContent = []byte("# MatterCodex Schedule prompt readiness\n")

func New(config Config, fence readinessFence) (*Client, error) {
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != config.TLSServerName ||
		parsed.Path != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		invalidBucket(config.Bucket) || config.MaximumObjectBytes < 1 || config.MaximumObjectBytes > 262144 ||
		config.Timeout < time.Second || config.Timeout > time.Minute || fence == nil {
		return nil, errors.New("S3 control-plane content object store configuration is invalid")
	}
	accessKey, err := readCredential(config.AccessKeyFile)
	if err != nil {
		return nil, errors.New("read S3 instruction access credential")
	}
	secretKey, err := readCredential(config.SecretKeyFile)
	if err != nil {
		return nil, errors.New("read S3 instruction secret credential")
	}
	sessionToken := ""
	if config.SessionTokenFile != "" {
		sessionToken, err = readCredential(config.SessionTokenFile)
		if err != nil {
			return nil, errors.New("read S3 instruction session credential")
		}
	}
	certificate, err := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load S3 instruction client identity")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read S3 instruction CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse S3 instruction CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13,
		ServerName: config.TLSServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}},
		ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 4,
		IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: config.Timeout}
	configured, err := minio.New(parsed.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey,
		secretKey, sessionToken), Secure: true, Transport: transport})
	if err != nil {
		return nil, errors.New("create S3 control-plane content object store client")
	}
	return &Client{config: config, client: configured, fence: fence}, nil
}

func (client *Client) Check(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, client.config.Timeout)
	defer cancel()
	exists, err := client.client.BucketExists(checkCtx, client.config.Bucket)
	if err != nil || !exists {
		return errors.New("S3 control-plane content object store bucket is not ready")
	}
	versioning, err := client.client.GetBucketVersioning(checkCtx, client.config.Bucket)
	if err != nil || !versioning.Enabled() {
		return errors.New("S3 control-plane content object store bucket versioning is not ready")
	}
	return client.withReadinessFence(checkCtx, func(fencedCtx context.Context) error {
		// Все replica сначала восстанавливают authoritative S3 state под одним
		// PostgreSQL session-lifetime fence. Ambiguous Put/Delete переживает
		// replacement, а live VersionID другого probe удалить невозможно.
		if err := reconcileReadinessObjects(fencedCtx, client.client, client.config.Bucket); err != nil {
			return err
		}
		if err := reconcileReadinessObjectsAtPrefix(fencedCtx, client.client, client.config.Bucket,
			scheduleReadinessPrefix); err != nil {
			return err
		}
		if err := client.checkWorkingPath(fencedCtx); err != nil {
			return err
		}
		return client.checkWorkingPathAt(fencedCtx, scheduleReadinessKey,
			scheduleReadinessPrefix, scheduleReadinessContent)
	})
}

func (client *Client) withReadinessFence(
	ctx context.Context,
	callback func(context.Context) error,
) error {
	if client.fence == nil || callback == nil {
		return errors.New("S3 control-plane content object store readiness fence is unavailable")
	}
	return client.fence.WithInstructionObjectReadinessFence(ctx, callback)
}

func (client *Client) checkWorkingPath(ctx context.Context) (resultErr error) {
	return client.checkWorkingPathAt(ctx, readinessObjectKey, readinessObjectPrefix, readinessContent)
}

func (client *Client) checkWorkingPathAt(
	ctx context.Context,
	objectKey, objectPrefix string,
	content []byte,
) (resultErr error) {
	expectedSHA256 := digest(content)
	put, err := client.client.PutObject(ctx, client.config.Bucket, objectKey,
		bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
			ContentType: "text/markdown", UserMetadata: map[string]string{"mattercodex-sha256": expectedSHA256},
			DisableMultipart: true,
		})
	if err != nil || put.Key != objectKey || put.Size != int64(len(content)) || put.VersionID == "" {
		// Commit мог состояться без доступного VersionID. Следующий probe любой
		// replica обязан найти версию через ListObjectVersions до нового Put.
		return errReadinessCleanup
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), client.config.Timeout)
		defer cancel()
		if err := client.client.RemoveObject(cleanupCtx, client.config.Bucket, objectKey,
			minio.RemoveObjectOptions{VersionID: put.VersionID}); err != nil {
			resultErr = errReadinessCleanup
			return
		}
		if err := reconcileReadinessObjectsAtPrefix(cleanupCtx, client.client, client.config.Bucket, objectPrefix); err != nil {
			resultErr = errReadinessCleanup
		}
	}()
	info, err := client.client.StatObject(ctx, client.config.Bucket, objectKey,
		minio.StatObjectOptions{VersionID: put.VersionID})
	if err != nil || info.Key != objectKey || info.VersionID != put.VersionID ||
		info.Size != int64(len(content)) || info.ContentType != "text/markdown" ||
		objectMetadataSHA256(info) != expectedSHA256 {
		return errors.New("S3 control-plane content object store readiness stat failed")
	}
	object, err := client.client.GetObject(ctx, client.config.Bucket, objectKey,
		minio.GetObjectOptions{VersionID: put.VersionID})
	if err != nil {
		return errors.New("S3 control-plane content object store readiness read failed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(object, int64(len(content))+1))
	closeErr := object.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(raw, content) || digest(raw) != expectedSHA256 {
		return errors.New("S3 control-plane content object store readiness readback failed")
	}
	return nil
}

type readinessObjectStore interface {
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
}

// reconcileReadinessObjects удаляет все versions и delete markers только из
// выделенного deterministic prefix, затем отдельным list доказывает пустоту.
// Bounds не позволяют readiness превратиться в неограниченный cleanup worker.
func reconcileReadinessObjects(ctx context.Context, store readinessObjectStore, bucket string) error {
	return reconcileReadinessObjectsAtPrefix(ctx, store, bucket, readinessObjectPrefix)
}

func reconcileReadinessObjectsAtPrefix(
	ctx context.Context,
	store readinessObjectStore,
	bucket, prefix string,
) error {
	versions, overflow, err := listReadinessVersionsAtPrefix(ctx, store, bucket, prefix)
	if err != nil {
		return errReadinessCleanup
	}
	for _, object := range versions {
		if err := store.RemoveObject(ctx, bucket, object.Key,
			minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
			return errReadinessCleanup
		}
	}
	remaining, remainingOverflow, err := listReadinessVersionsAtPrefix(ctx, store, bucket, prefix)
	if err != nil || overflow || remainingOverflow || len(remaining) != 0 {
		return errReadinessCleanup
	}
	return nil
}

func listReadinessVersionsAtPrefix(
	ctx context.Context,
	store readinessObjectStore,
	bucket, prefix string,
) ([]minio.ObjectInfo, bool, error) {
	objects := make([]minio.ObjectInfo, 0, 1)
	overflow := false
	for object := range store.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix: prefix, Recursive: true, WithVersions: true,
	}) {
		if object.Err != nil || object.Key == "" ||
			!strings.HasPrefix(object.Key, prefix) || object.VersionID == "" {
			return nil, false, errReadinessCleanup
		}
		if len(objects) == readinessMaximumVersions {
			overflow = true
			continue
		}
		objects = append(objects, object)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, errReadinessCleanup
	}
	return objects, overflow, nil
}

func objectNotFound(err error) bool {
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchObject", "NoSuchVersion", "NotFound":
		return true
	default:
		return false
	}
}

func (client *Client) Put(ctx context.Context, projectID, key string, raw []byte, mediaType, expectedSHA256 string) (domainobjectstore.Object, error) {
	if invalidSegment(projectID) || invalidObjectKey(key) || len(raw) == 0 || int64(len(raw)) > client.config.MaximumObjectBytes ||
		digest(raw) != expectedSHA256 || mediaType != "text/markdown" {
		return domainobjectstore.Object{}, errors.New("S3 instruction object input is invalid")
	}
	objectKey := path.Join("projects", projectID, key)
	if existing, found, err := client.stat(ctx, objectKey, "", int64(len(raw)), mediaType, expectedSHA256); err != nil {
		return domainobjectstore.Object{}, err
	} else if found {
		return existing, nil
	}
	result, err := client.client.PutObject(ctx, client.config.Bucket, objectKey, bytes.NewReader(raw), int64(len(raw)),
		minio.PutObjectOptions{ContentType: mediaType, UserMetadata: map[string]string{"mattercodex-sha256": expectedSHA256}, DisableMultipart: true})
	if err != nil || result.Key != objectKey || result.Size != int64(len(raw)) || result.VersionID == "" {
		return domainobjectstore.Object{}, errors.New("S3 instruction object write failed")
	}
	readback, found, err := client.stat(ctx, objectKey, result.VersionID, int64(len(raw)), mediaType, expectedSHA256)
	if err != nil || !found {
		return domainobjectstore.Object{}, errors.New("S3 instruction object write readback failed")
	}
	return readback, nil
}

func (client *Client) stat(
	ctx context.Context,
	objectKey, versionID string,
	size int64,
	mediaType, expectedSHA256 string,
) (domainobjectstore.Object, bool, error) {
	info, err := client.client.StatObject(ctx, client.config.Bucket, objectKey,
		minio.StatObjectOptions{VersionID: versionID})
	if err != nil {
		if objectNotFound(err) {
			return domainobjectstore.Object{}, false, nil
		}
		return domainobjectstore.Object{}, false, errors.New("read S3 instruction object metadata")
	}
	storedDigest := objectMetadataSHA256(info)
	if info.Key != objectKey || info.Size != size || info.ContentType != mediaType || storedDigest != expectedSHA256 ||
		(versionID != "" && info.VersionID != versionID) {
		return domainobjectstore.Object{}, false, errors.New("S3 instruction object idempotency conflict")
	}
	reference := "s3://" + client.config.Bucket + "/" + objectKey
	if info.VersionID != "" {
		reference += "?versionId=" + url.QueryEscape(info.VersionID)
	}
	return domainobjectstore.Object{Reference: reference, VersionID: info.VersionID,
		SHA256: expectedSHA256, Size: uint64(size), MediaType: mediaType}, true, nil
}

func objectMetadataSHA256(info minio.ObjectInfo) string {
	storedDigest := info.Metadata.Get("X-Amz-Meta-Mattercodex-Sha256")
	if storedDigest == "" {
		storedDigest = info.UserMetadata["mattercodex-sha256"]
	}
	return storedDigest
}

func readCredential(file string) (string, error) {
	if !filepath.IsAbs(file) {
		return "", errors.New("credential path is not absolute")
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16<<10 || info.Mode().Perm()&0o037 != 0 {
		return "", errors.New("credential file is unsafe")
	}
	raw, err := os.ReadFile(file)
	value := strings.TrimSpace(string(raw))
	if err != nil || value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("read bounded credential")
	}
	return value, nil
}

func invalidBucket(value string) bool {
	return len(value) < 3 || len(value) > 63 || value != strings.ToLower(value) || strings.ContainsAny(value, "_ /\\\x00\r\n")
}

func invalidSegment(value string) bool {
	if len(value) == 0 || len(value) > 128 || value == "." || value == ".." {
		return true
	}
	return strings.ContainsAny(value, "/\\\x00\r\n")
}

func invalidObjectKey(value string) bool {
	if len(value) == 0 || len(value) > 1024 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.ContainsAny(value, "\\\x00\r\n") {
		return true
	}
	for _, segment := range strings.Split(value, "/") {
		if invalidSegment(segment) {
			return true
		}
	}
	return false
}

func digest(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}
