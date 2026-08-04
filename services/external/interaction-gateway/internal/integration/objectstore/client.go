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
	Endpoint               string
	PublicDownloadEndpoint string
	TLSServerName          string
	CAFile                 string
	ClientCertificateFile  string
	ClientPrivateKeyFile   string
	AccessKeyFile          string
	SecretKeyFile          string
	SessionTokenFile       string
	Bucket                 string
	MaximumObjectBytes     int64
	Timeout                time.Duration
}

type Client struct {
	config         Config
	client         *minio.Client
	downloadClient *minio.Client
}

func New(config Config) (*Client, error) {
	parsed, err := url.Parse(config.Endpoint)
	publicEndpoint, publicErr := url.Parse(config.PublicDownloadEndpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != config.TLSServerName ||
		parsed.Path != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		invalidBucket(config.Bucket) || config.MaximumObjectBytes < 1<<20 ||
		config.MaximumObjectBytes > 1<<30 || config.Timeout < time.Second || config.Timeout > time.Minute ||
		publicErr != nil || publicEndpoint.Scheme != "https" || publicEndpoint.Host == "" ||
		publicEndpoint.User != nil || publicEndpoint.RawQuery != "" || publicEndpoint.Fragment != "" || publicEndpoint.Path != "" {
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
	downloadClient, err := minio.New(publicEndpoint.Host, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, sessionToken), Secure: true,
	})
	if err != nil {
		return nil, errors.New("create protected S3 download signer")
	}
	return &Client{config: config, client: configured, downloadClient: downloadClient}, nil
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
	objectKey := path.Join("projects", projectID, key)
	if existing, found, err := client.stat(ctx, objectKey, int64(len(raw)), mediaType, expectedSHA256); err != nil {
		return domainobjectstore.Object{}, err
	} else if found {
		return existing, nil
	}
	result, err := client.client.PutObject(ctx, client.config.Bucket, objectKey, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{
		ContentType: mediaType, UserMetadata: map[string]string{"mattercodex-sha256": expectedSHA256},
		DisableMultipart: int64(len(raw)) <= client.config.MaximumObjectBytes,
	})
	if err != nil || result.Key != objectKey || result.Size != int64(len(raw)) {
		return domainobjectstore.Object{}, errors.New("S3 object write failed")
	}
	readback, found, statErr := client.stat(ctx, objectKey, int64(len(raw)), mediaType, expectedSHA256)
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

func (client *Client) ProtectedURL(ctx context.Context, projectID, reference string, expectedSize uint64,
	expectedSHA256, name string, ttl time.Duration) (string, error) {
	bucket, objectKey, versionID, err := parseReference(reference)
	if err != nil || bucket != client.config.Bucket || invalidSegment(projectID) ||
		!strings.HasPrefix(objectKey, "projects/"+projectID+"/") || expectedSize == 0 ||
		expectedSize > uint64(client.config.MaximumObjectBytes) || len(expectedSHA256) != 64 ||
		ttl < time.Minute || ttl > time.Hour || filepath.Base(name) != name {
		return "", errors.New("protected S3 download input is invalid")
	}
	inspected, found, err := client.Inspect(ctx, projectID, reference, expectedSHA256)
	if err != nil || !found || inspected.Size != expectedSize {
		return "", errors.New("protected S3 download metadata mismatch")
	}
	parameters := url.Values{
		"response-content-disposition": {"attachment; filename=\"" + strings.ReplaceAll(name, "\"", "") + "\""},
	}
	if versionID != "" {
		parameters.Set("versionId", versionID)
	}
	protected, err := client.downloadClient.PresignedGetObject(ctx, bucket, objectKey, ttl, parameters)
	if err != nil || protected.Scheme != "https" {
		return "", errors.New("create protected S3 download URL")
	}
	return protected.String(), nil
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
