// Package objectstore реализует service-owned S3 writer для immutable
// InstructionSet content. Metadata authority остаётся в control-plane.
package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
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
}

func New(config Config) (*Client, error) {
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != config.TLSServerName ||
		parsed.Path != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		invalidBucket(config.Bucket) || config.MaximumObjectBytes < 1 || config.MaximumObjectBytes > 262144 ||
		config.Timeout < time.Second || config.Timeout > time.Minute {
		return nil, errors.New("S3 instruction object store configuration is invalid")
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
		return nil, errors.New("create S3 instruction object store client")
	}
	return &Client{config: config, client: configured}, nil
}

func (client *Client) Check(ctx context.Context) error {
	exists, err := client.client.BucketExists(ctx, client.config.Bucket)
	if err != nil || !exists {
		return errors.New("S3 instruction object store bucket is not ready")
	}
	versioning, err := client.client.GetBucketVersioning(ctx, client.config.Bucket)
	if err != nil || !versioning.Enabled() {
		return errors.New("S3 instruction object store bucket versioning is not ready")
	}
	return nil
}

func (client *Client) Put(ctx context.Context, projectID, key string, raw []byte, mediaType, expectedSHA256 string) (domainobjectstore.Object, error) {
	if invalidSegment(projectID) || invalidObjectKey(key) || len(raw) == 0 || int64(len(raw)) > client.config.MaximumObjectBytes ||
		digest(raw) != expectedSHA256 || mediaType != "text/markdown" {
		return domainobjectstore.Object{}, errors.New("S3 instruction object input is invalid")
	}
	objectKey := path.Join("projects", projectID, key)
	if existing, found, err := client.stat(ctx, objectKey, int64(len(raw)), mediaType, expectedSHA256); err != nil {
		return domainobjectstore.Object{}, err
	} else if found {
		return existing, nil
	}
	result, err := client.client.PutObject(ctx, client.config.Bucket, objectKey, bytes.NewReader(raw), int64(len(raw)),
		minio.PutObjectOptions{ContentType: mediaType, UserMetadata: map[string]string{"mattercodex-sha256": expectedSHA256}, DisableMultipart: true})
	if err != nil || result.Key != objectKey || result.Size != int64(len(raw)) {
		return domainobjectstore.Object{}, errors.New("S3 instruction object write failed")
	}
	readback, found, err := client.stat(ctx, objectKey, int64(len(raw)), mediaType, expectedSHA256)
	if err != nil || !found {
		return domainobjectstore.Object{}, errors.New("S3 instruction object write readback failed")
	}
	return readback, nil
}

func (client *Client) stat(ctx context.Context, objectKey string, size int64, mediaType, expectedSHA256 string) (domainobjectstore.Object, bool, error) {
	info, err := client.client.StatObject(ctx, client.config.Bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "NoSuchKey", "NoSuchObject", "NotFound":
			return domainobjectstore.Object{}, false, nil
		default:
			return domainobjectstore.Object{}, false, errors.New("read S3 instruction object metadata")
		}
	}
	storedDigest := info.Metadata.Get("X-Amz-Meta-Mattercodex-Sha256")
	if storedDigest == "" {
		storedDigest = info.UserMetadata["mattercodex-sha256"]
	}
	if info.Key != objectKey || info.Size != size || info.ContentType != mediaType || storedDigest != expectedSHA256 {
		return domainobjectstore.Object{}, false, errors.New("S3 instruction object idempotency conflict")
	}
	reference := "s3://" + client.config.Bucket + "/" + objectKey
	if info.VersionID != "" {
		reference += "?versionId=" + url.QueryEscape(info.VersionID)
	}
	return domainobjectstore.Object{Reference: reference, VersionID: info.VersionID,
		SHA256: expectedSHA256, Size: uint64(size), MediaType: mediaType}, true, nil
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
