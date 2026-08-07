// Package objectstore реализует service-owned S3 adapter без владения
// artifact metadata, которое остаётся в control-plane.
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

	domainobjectstore "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/objectstore"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint              string
	TLSServerName         string
	CAFile                string
	ClientCertificateFile string
	ClientPrivateKeyFile  string
	AccessKeyFile         string
	SecretKeyFile         string
	SessionTokenFile      string
	Bucket                string
	MaximumObjectBytes    int64
	Timeout               time.Duration
}

type Client struct {
	config Config
	client *minio.Client
}

func New(config Config) (*Client, error) {
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != config.TLSServerName ||
		parsed.Path != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		invalidBucket(config.Bucket) || config.MaximumObjectBytes < 1<<20 ||
		config.MaximumObjectBytes > 1<<30 || config.Timeout < time.Second || config.Timeout > time.Minute {
		return nil, errors.New("S3 object store configuration is invalid")
	}
	accessKey, err := readCredential(config.AccessKeyFile)
	if err != nil {
		return nil, errors.New("read S3 access credential")
	}
	secretKey, err := readCredential(config.SecretKeyFile)
	if err != nil {
		return nil, errors.New("read S3 secret credential")
	}
	sessionToken := ""
	if config.SessionTokenFile != "" {
		sessionToken, err = readCredential(config.SessionTokenFile)
		if err != nil {
			return nil, errors.New("read S3 session credential")
		}
	}
	certificate, err := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load S3 client identity")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read S3 CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse S3 CA")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
			RootCAs: roots, Certificates: []tls.Certificate{certificate},
		},
		ForceAttemptHTTP2: true, MaxIdleConns: 16, MaxIdleConnsPerHost: 8,
		IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: config.Timeout,
	}
	configured, err := minio.New(parsed.Host, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, sessionToken), Secure: true,
		Transport: transport,
	})
	if err != nil {
		return nil, errors.New("create S3 object store client")
	}
	return &Client{config: config, client: configured}, nil
}

func (client *Client) Check(ctx context.Context) error {
	exists, err := client.client.BucketExists(ctx, client.config.Bucket)
	if err != nil || !exists {
		return errors.New("S3 object store bucket is not ready")
	}
	return nil
}

func (client *Client) Put(ctx context.Context, projectID, key string, raw []byte, mediaType, expectedSHA256 string) (domainobjectstore.Object, error) {
	if invalidSegment(projectID) || invalidObjectKey(key) || len(raw) == 0 ||
		int64(len(raw)) > client.config.MaximumObjectBytes || digest(raw) != expectedSHA256 ||
		len(mediaType) < 3 || len(mediaType) > 255 {
		return domainobjectstore.Object{}, errors.New("S3 object input is invalid")
	}
	return client.PutStream(ctx, projectID, key, bytes.NewReader(raw), int64(len(raw)), mediaType, expectedSHA256)
}

func (client *Client) PutStream(ctx context.Context, projectID, key string, raw io.ReadSeeker, size int64,
	mediaType, expectedSHA256 string,
) (domainobjectstore.Object, error) {
	if invalidSegment(projectID) || invalidObjectKey(key) || raw == nil || size < 1 ||
		size > client.config.MaximumObjectBytes || len(mediaType) < 3 || len(mediaType) > 255 {
		return domainobjectstore.Object{}, errors.New("S3 object stream input is invalid")
	}
	if _, err := raw.Seek(0, io.SeekStart); err != nil {
		return domainobjectstore.Object{}, errors.New("seek S3 object stream")
	}
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(raw, size+1))
	if err != nil || written != size || hex.EncodeToString(hasher.Sum(nil)) != expectedSHA256 {
		return domainobjectstore.Object{}, errors.New("S3 object stream digest is invalid")
	}
	if _, err := raw.Seek(0, io.SeekStart); err != nil {
		return domainobjectstore.Object{}, errors.New("rewind S3 object stream")
	}
	objectKey := path.Join("projects", projectID, key)
	if existing, found, err := client.stat(ctx, objectKey, size, mediaType, expectedSHA256); err != nil {
		return domainobjectstore.Object{}, err
	} else if found {
		return existing, nil
	}
	result, err := client.client.PutObject(ctx, client.config.Bucket, objectKey, raw, size, minio.PutObjectOptions{
		ContentType: mediaType, UserMetadata: map[string]string{"mattercodex-sha256": expectedSHA256},
		DisableMultipart: size <= client.config.MaximumObjectBytes,
	})
	if err != nil || result.Key != objectKey || result.Size != size {
		return domainobjectstore.Object{}, errors.New("S3 object write failed")
	}
	readback, found, statErr := client.stat(ctx, objectKey, size, mediaType, expectedSHA256)
	if statErr != nil || !found {
		return domainobjectstore.Object{}, errors.New("S3 object write readback failed")
	}
	return readback, nil
}

func (client *Client) stat(ctx context.Context, objectKey string, size int64, mediaType, expectedSHA256 string) (domainobjectstore.Object, bool, error) {
	info, err := client.client.StatObject(ctx, client.config.Bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		code := minio.ToErrorResponse(err).Code
		if code == "NoSuchKey" || code == "NoSuchObject" || code == "NotFound" {
			return domainobjectstore.Object{}, false, nil
		}
		return domainobjectstore.Object{}, false, errors.New("read S3 object metadata")
	}
	storedDigest := info.Metadata.Get("X-Amz-Meta-Mattercodex-Sha256")
	if storedDigest == "" {
		storedDigest = info.UserMetadata["mattercodex-sha256"]
	}
	if info.Key != objectKey || info.Size != size || info.ContentType != mediaType || storedDigest != expectedSHA256 {
		return domainobjectstore.Object{}, false, errors.New("S3 object idempotency conflict")
	}
	reference := "s3://" + client.config.Bucket + "/" + objectKey
	if info.VersionID != "" {
		reference += "?versionId=" + url.QueryEscape(info.VersionID)
	}
	return domainobjectstore.Object{
		Reference: reference, VersionID: info.VersionID, SHA256: expectedSHA256, Size: uint64(size),
		Name: path.Base(objectKey), MediaType: mediaType,
	}, true, nil
}

func (client *Client) Inspect(ctx context.Context, projectID, reference, expectedSHA256 string) (domainobjectstore.Object, bool, error) {
	parsed, parseErr := url.Parse(reference)
	if parseErr != nil || parsed.Scheme != "s3" || parsed.Host != client.config.Bucket {
		return domainobjectstore.Object{}, false, nil
	}
	bucket, objectKey, versionID, err := parseReference(reference)
	if err != nil || bucket != client.config.Bucket {
		return domainobjectstore.Object{}, false, errors.New("S3 object reference is invalid")
	}
	if invalidSegment(projectID) || !strings.HasPrefix(objectKey, "projects/"+projectID+"/") || len(expectedSHA256) != 64 {
		return domainobjectstore.Object{}, false, errors.New("S3 object reference is invalid")
	}
	info, err := client.client.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{VersionID: versionID})
	if err != nil || info.Key != objectKey || info.Size <= 0 || info.Size > client.config.MaximumObjectBytes ||
		info.ContentType == "" || info.ContentType == "application/octet-stream" {
		return domainobjectstore.Object{}, false, errors.New("S3 object metadata is unavailable")
	}
	storedDigest := info.Metadata.Get("X-Amz-Meta-Mattercodex-Sha256")
	if storedDigest == "" {
		storedDigest = info.UserMetadata["mattercodex-sha256"]
	}
	if storedDigest != expectedSHA256 {
		return domainobjectstore.Object{}, false, errors.New("S3 object metadata mismatch")
	}
	return domainobjectstore.Object{
		Reference: reference, VersionID: info.VersionID, SHA256: expectedSHA256,
		Size: uint64(info.Size), Name: path.Base(objectKey), MediaType: info.ContentType,
	}, true, nil
}

func (client *Client) Get(ctx context.Context, projectID, reference string, expectedSize uint64, expectedSHA256 string) ([]byte, error) {
	bucket, objectKey, versionID, err := parseReference(reference)
	if err != nil || bucket != client.config.Bucket || invalidSegment(projectID) ||
		!strings.HasPrefix(objectKey, "projects/"+projectID+"/") || expectedSize == 0 ||
		expectedSize > uint64(client.config.MaximumObjectBytes) || len(expectedSHA256) != 64 {
		return nil, errors.New("S3 object reference is invalid")
	}
	object, err := client.client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{VersionID: versionID})
	if err != nil {
		return nil, errors.New("open S3 object")
	}
	defer object.Close()
	raw, err := io.ReadAll(io.LimitReader(object, int64(expectedSize)+1))
	if err != nil || uint64(len(raw)) != expectedSize || digest(raw) != expectedSHA256 {
		return nil, errors.New("S3 object readback mismatch")
	}
	return raw, nil
}

func parseReference(reference string) (string, string, string, error) {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || invalidBucket(parsed.Host) || parsed.User != nil || parsed.Fragment != "" {
		return "", "", "", errors.New("invalid S3 reference")
	}
	objectKey := strings.TrimPrefix(parsed.EscapedPath(), "/")
	decoded, err := url.PathUnescape(objectKey)
	if err != nil || invalidObjectKey(decoded) {
		return "", "", "", errors.New("invalid S3 object key")
	}
	values := parsed.Query()
	if len(values) > 1 || (len(values) == 1 && len(values["versionId"]) != 1) {
		return "", "", "", errors.New("invalid S3 version")
	}
	return parsed.Host, decoded, values.Get("versionId"), nil
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
	return len(value) < 3 || len(value) > 63 || strings.Trim(value, "abcdefghijklmnopqrstuvwxyz0123456789.-") != ""
}

func invalidSegment(value string) bool {
	return value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n")
}

func invalidObjectKey(value string) bool {
	if len(value) < 1 || len(value) > 512 || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00\r\n") {
		return true
	}
	cleaned := path.Clean(value)
	return cleaned != value || cleaned == "." || strings.HasPrefix(cleaned, "../")
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
