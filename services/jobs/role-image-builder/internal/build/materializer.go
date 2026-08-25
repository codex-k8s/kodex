package build

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

var ErrMaterialization = errors.New("role image input materialization failed")

const maximumManifestBytes = int64(1 << 20)

type MaterializerConfig struct {
	DockerConfig, Repository, TLSServerName string
	CAFile, CertificateFile, PrivateKeyFile string
}

// Materializer является trusted fetch boundary. Он получает pull-only mTLS и
// registry credential, пишет private emptyDir и не передаёт authority BuildKit.
type Materializer struct {
	client                 *http.Client
	repository, host, path string
	username, password     string
}

func newMaterializer(config MaterializerConfig) (*Materializer, error) {
	if !filepath.IsAbs(config.DockerConfig) || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.CertificateFile) || !filepath.IsAbs(config.PrivateKeyFile) ||
		!validRepository(config.Repository) || !validDNSName(config.TLSServerName) {
		return nil, errors.New("role image materializer configuration is invalid")
	}
	host, repositoryPath, found := strings.Cut(config.Repository, "/")
	if !found || host == "" || repositoryPath == "" {
		return nil, errors.New("role image materializer repository is invalid")
	}
	ca, err := os.ReadFile(config.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, ErrMaterialization
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, ErrMaterialization
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, ErrMaterialization
	}
	username, password, err := readRegistryCredential(config.DockerConfig, host)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: nil,
		TLSClientConfig: &tls.Config{ //nolint:gosec // fixed TLS 1.3 destination with exact SNI and CA.
			MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
			RootCAs: pool, Certificates: []tls.Certificate{certificate},
		},
		ForceAttemptHTTP2: false, DisableKeepAlives: true,
		DialContext: (&netDialer{timeout: 3 * time.Second}).DialContext,
	}
	return &Materializer{client: &http.Client{Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return ErrMaterialization }},
		repository: config.Repository, host: host, path: repositoryPath,
		username: username, password: password}, nil
}

// Check выполняет тот же authenticated mTLS registry path, что и materialization.
func (materializer *Materializer) Check(ctx context.Context) error {
	request, err := materializer.request(ctx, http.MethodGet, "https://"+materializer.host+"/v2/")
	if err != nil {
		return ErrMaterialization
	}
	response, err := materializer.client.Do(request)
	if err != nil {
		return ErrMaterialization
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrMaterialization
	}
	return nil
}

func (materializer *Materializer) Materialize(
	ctx context.Context,
	root string,
	input *controlplanev1.RoleImageBuildInput,
	beforeContextValidation func() error,
) (string, error) {
	if input == nil || !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		!materializer.allowedRef(input.GetContextRef()) {
		return "INPUT_FETCH_REJECTED", ErrMaterialization
	}
	archive := filepath.Join(root, "context.tar")
	if err := materializer.downloadExact(ctx, input.GetContextRef(), archive, input.GetContextSha256(), maximumContextBytes); err != nil {
		return "INPUT_DIGEST_MISMATCH", err
	}
	if beforeContextValidation == nil {
		return "CONTEXT_REPORT_REJECTED", ErrMaterialization
	}
	if err := beforeContextValidation(); err != nil {
		return "CONTEXT_REPORT_REJECTED", err
	}
	contextDirectory := filepath.Join(root, "context")
	if err := os.MkdirAll(contextDirectory, 0o700); err != nil {
		return "ARCHIVE_REJECTED", ErrMaterialization
	}
	if err := ExtractContext(archive, contextDirectory, input.GetContextSha256(), input.GetSourceSha256()); err != nil {
		return "ARCHIVE_REJECTED", err
	}
	if err := os.Remove(archive); err != nil {
		return "ARCHIVE_REJECTED", ErrMaterialization
	}
	for _, item := range input.GetPackages() {
		digest := strings.TrimPrefix(item.GetDigest(), "sha256:")
		if !materializer.allowedRef(item.GetSourceRef()) || !plainSHA256(digest) {
			return "INPUT_FETCH_REJECTED", ErrMaterialization
		}
		destination := filepath.Join(contextDirectory, ".kodex", "packages", digest)
		if err := materializer.downloadExact(ctx, item.GetSourceRef(), destination, digest, maximumFileBytes); err != nil {
			return "INPUT_DIGEST_MISMATCH", err
		}
	}
	for _, item := range input.GetTools() {
		if !materializer.allowedRef(item.GetSourceRef()) || !plainSHA256(item.GetSha256()) {
			return "INPUT_FETCH_REJECTED", ErrMaterialization
		}
		destination := filepath.Join(contextDirectory, ".kodex", "tools", item.GetSha256())
		if err := materializer.downloadExact(ctx, item.GetSourceRef(), destination, item.GetSha256(), maximumFileBytes); err != nil {
			return "INPUT_DIGEST_MISMATCH", err
		}
	}
	return "", nil
}

func (materializer *Materializer) downloadExact(
	ctx context.Context,
	reference, destination, expectedSHA256 string,
	maximumBytes int64,
) error {
	manifestDigest, ok := materializer.referenceDigest(reference)
	if !ok || !plainSHA256(expectedSHA256) {
		return ErrMaterialization
	}
	manifestURL := "https://" + materializer.host + "/v2/" + materializer.path + "/manifests/" + manifestDigest
	request, err := materializer.request(ctx, http.MethodGet, manifestURL)
	if err != nil {
		return ErrMaterialization
	}
	request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	response, err := materializer.client.Do(request)
	if err != nil {
		return ErrMaterialization
	}
	manifest, readErr := io.ReadAll(io.LimitReader(response.Body, maximumManifestBytes+1))
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK || readErr != nil || closeErr != nil || int64(len(manifest)) > maximumManifestBytes ||
		"sha256:"+sha256Hex(manifest) != manifestDigest {
		return ErrMaterialization
	}
	var document struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
		Layers        []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		} `json:"layers"`
	}
	if json.Unmarshal(manifest, &document) != nil || document.SchemaVersion != 2 ||
		document.MediaType != "application/vnd.oci.image.manifest.v1+json" || len(document.Layers) != 1 ||
		document.Layers[0].MediaType != "application/vnd.kodex.role-image-input.v1" ||
		document.Layers[0].Digest != "sha256:"+expectedSHA256 || document.Layers[0].Size < 1 ||
		document.Layers[0].Size > maximumBytes {
		return ErrMaterialization
	}
	blobURL := "https://" + materializer.host + "/v2/" + materializer.path + "/blobs/" + document.Layers[0].Digest
	request, err = materializer.request(ctx, http.MethodGet, blobURL)
	if err != nil {
		return ErrMaterialization
	}
	response, err = materializer.client.Do(request)
	if err != nil {
		return ErrMaterialization
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maximumBytes ||
		(response.ContentLength >= 0 && response.ContentLength != document.Layers[0].Size) {
		return ErrMaterialization
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return ErrMaterialization
	}
	temporary, err := os.OpenFile(destination+".part", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ErrMaterialization
	}
	digest := sha256.New()
	limited := &boundedWriter{writer: io.MultiWriter(temporary, digest), remaining: maximumBytes}
	written, copyErr := io.Copy(limited, response.Body)
	closeErr = temporary.Close()
	if copyErr != nil || closeErr != nil || limited.exceeded || written != document.Layers[0].Size ||
		hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 {
		_ = os.Remove(destination + ".part")
		return fmt.Errorf("%w: immutable payload mismatch", ErrMaterialization)
	}
	if err := os.Rename(destination+".part", destination); err != nil {
		_ = os.Remove(destination + ".part")
		return ErrMaterialization
	}
	return nil
}

func (materializer *Materializer) request(ctx context.Context, method, target string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(materializer.username, materializer.password)
	request.Header.Set("User-Agent", "kodex-role-image-materializer/1")
	return request, nil
}

func (materializer *Materializer) referenceDigest(reference string) (string, bool) {
	if !materializer.allowedRef(reference) {
		return "", false
	}
	_, digest, _ := strings.Cut(strings.TrimPrefix(reference, "oci://"), "@")
	return digest, true
}

func (materializer *Materializer) allowedRef(reference string) bool {
	value := strings.TrimPrefix(reference, "oci://")
	return strings.HasPrefix(reference, "oci://") && strings.HasPrefix(value, materializer.repository+"@") &&
		strings.Count(value, "@") == 1 && digestPattern.MatchString(strings.SplitN(value, "@", 2)[1]) &&
		!strings.ContainsAny(value, "?# \r\n\t")
}

func readRegistryCredential(path, host string) (string, string, error) {
	value, err := os.ReadFile(path)
	if err != nil || len(value) == 0 || len(value) > 1<<20 {
		return "", "", ErrMaterialization
	}
	var document struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
		CredsStore  string            `json:"credsStore"`
		CredHelpers map[string]string `json:"credHelpers"`
	}
	if json.Unmarshal(value, &document) != nil || document.CredsStore != "" || len(document.CredHelpers) != 0 || len(document.Auths) != 1 {
		return "", "", ErrMaterialization
	}
	entry, exists := document.Auths[host]
	if !exists || entry.Auth == "" {
		return "", "", ErrMaterialization
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil || len(decoded) > 8192 {
		return "", "", ErrMaterialization
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found || username == "" || password == "" || strings.ContainsAny(username, "\r\n") || strings.ContainsAny(password, "\r\n") {
		return "", "", ErrMaterialization
	}
	return username, password, nil
}

type boundedWriter struct {
	writer             io.Writer
	remaining, written int64
	exceeded           bool
}

func (writer *boundedWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > writer.remaining {
		writer.exceeded = true
		return 0, ErrMaterialization
	}
	n, err := writer.writer.Write(value)
	writer.remaining -= int64(n)
	writer.written += int64(n)
	return n, err
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// netDialer is kept private so callers cannot replace the destination path.
type netDialer struct{ timeout time.Duration }

func (dialer *netDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address == "" {
		return nil, ErrMaterialization
	}
	return (&net.Dialer{Timeout: dialer.timeout, KeepAlive: -1}).DialContext(ctx, network, address)
}
