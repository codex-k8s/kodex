package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	manifestMediaType = "application/vnd.oci.image.manifest.v1+json"
	configMediaType   = "application/vnd.kodex.role-image-input.config.v1+json"
	payloadMediaType  = "application/vnd.kodex.role-image-input.v1"
	maximumResponse   = 1 << 20
)

var (
	repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.:-]+/[a-z0-9][a-z0-9._/-]*$`)
	sourcePattern     = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        descriptor   `json:"config"`
	Layers        []descriptor `json:"layers"`
}

type dockerConfig struct {
	Auths map[string]struct {
		Auth string `json:"auth"`
	} `json:"auths"`
	CredsStore  string            `json:"credsStore"`
	CredHelpers map[string]string `json:"credHelpers"`
}

type publisher struct {
	client             *http.Client
	host, repository   string
	username, password string
}

func main() {
	var repository, sourceSHA, dockerConfigFile string
	flag.StringVar(&repository, "repository", "", "target OCI repository")
	flag.StringVar(&sourceSHA, "source-sha", "", "exact source commit")
	flag.StringVar(&dockerConfigFile, "docker-config", "", "Docker config.json path")
	flag.Parse()

	if !repositoryPattern.MatchString(repository) || strings.HasSuffix(repository, "/") ||
		strings.Contains(repository, "//") || !sourcePattern.MatchString(sourceSHA) ||
		flag.NArg() != 0 {
		fatal("publisher arguments are invalid")
	}
	if dockerConfigFile == "" {
		var err error
		dockerConfigFile, err = defaultDockerConfigFile()
		if err != nil {
			fatal("resolve Docker configuration")
		}
	}
	instance, err := newPublisher(repository, dockerConfigFile)
	if err != nil {
		fatal("construct publisher")
	}
	payload, sourceDigest, err := roleImageInput(sourceSHA)
	if err != nil {
		fatal("create role image input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	manifestDigest, err := instance.publish(ctx, sourceSHA, payload)
	if err != nil {
		fatal("publish role image input")
	}
	result := struct {
		ManifestDigest string `json:"manifestDigest"`
		PayloadSHA256  string `json:"payloadSha256"`
		SourceSHA256   string `json:"sourceSha256"`
	}{
		ManifestDigest: manifestDigest,
		PayloadSHA256:  sha256Hex(payload),
		SourceSHA256:   sourceDigest,
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal("encode publisher result")
	}
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "Role image input publisher failed: "+message)
	os.Exit(1)
}

func defaultDockerConfigFile() (string, error) {
	if directory := os.Getenv("DOCKER_CONFIG"); directory != "" {
		if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
			return "", errors.New("Docker configuration directory is invalid")
		}
		return filepath.Join(directory, "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", errors.New("Docker configuration home is invalid")
	}
	return filepath.Join(home, ".docker", "config.json"), nil
}

func newPublisher(repository, configFile string) (*publisher, error) {
	host, path, found := strings.Cut(repository, "/")
	if !found || host == "" || path == "" || !filepath.IsAbs(configFile) ||
		filepath.Clean(configFile) != configFile {
		return nil, errors.New("publisher configuration is invalid")
	}
	value, err := os.ReadFile(configFile)
	if err != nil || len(value) == 0 || len(value) > maximumResponse {
		return nil, errors.New("read Docker configuration")
	}
	var config dockerConfig
	if err := json.Unmarshal(value, &config); err != nil || config.CredsStore != "" ||
		len(config.CredHelpers) != 0 {
		return nil, errors.New("unsupported Docker credential configuration")
	}
	encoded := ""
	for _, key := range []string{host, "https://" + host, "https://" + host + "/v1/"} {
		if current, ok := config.Auths[key]; ok {
			encoded = current.Auth
			break
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > 4096 {
		return nil, errors.New("decode Docker credential")
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found || username == "" || password == "" {
		return nil, errors.New("Docker credential is invalid")
	}
	client := &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("registry redirect is forbidden")
		},
	}
	return &publisher{
		client: client, host: host, repository: path,
		username: username, password: password,
	}, nil
}

func roleImageInput(sourceSHA string) ([]byte, string, error) {
	sourceDigest := sha256Hex([]byte(sourceSHA))
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	header := &tar.Header{
		Name: ".kodex/source.sha256", Mode: 0o600, Size: int64(len(sourceDigest)),
		ModTime: time.Unix(0, 0).UTC(), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
	}
	if err := writer.WriteHeader(header); err != nil {
		return nil, "", err
	}
	if _, err := writer.Write([]byte(sourceDigest)); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return output.Bytes(), sourceDigest, nil
}

func (instance *publisher) publish(ctx context.Context, tag string, payload []byte) (string, error) {
	config := []byte("{}")
	configDigest := "sha256:" + sha256Hex(config)
	payloadDigest := "sha256:" + sha256Hex(payload)
	if err := instance.putBlob(ctx, configDigest, config); err != nil {
		return "", err
	}
	if err := instance.putBlob(ctx, payloadDigest, payload); err != nil {
		return "", err
	}
	document, err := json.Marshal(manifest{
		SchemaVersion: 2, MediaType: manifestMediaType,
		Config: descriptor{MediaType: configMediaType, Digest: configDigest, Size: int64(len(config))},
		Layers: []descriptor{{MediaType: payloadMediaType, Digest: payloadDigest, Size: int64(len(payload))}},
	})
	if err != nil {
		return "", err
	}
	manifestDigest := "sha256:" + sha256Hex(document)
	target := "https://" + instance.host + "/v2/" + instance.repository + "/manifests/" + tag
	response, err := instance.do(ctx, http.MethodPut, target, manifestMediaType, document)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusAccepted {
		return "", errors.New("registry rejected manifest")
	}
	if actual := response.Header.Get("Docker-Content-Digest"); actual != "" && actual != manifestDigest {
		return "", errors.New("registry manifest digest mismatch")
	}
	return manifestDigest, nil
}

func (instance *publisher) putBlob(ctx context.Context, digest string, value []byte) error {
	target := "https://" + instance.host + "/v2/" + instance.repository + "/blobs/" + digest
	response, err := instance.do(ctx, http.MethodHead, target, "", nil)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	if response.StatusCode == http.StatusOK {
		return nil
	}
	if response.StatusCode != http.StatusNotFound {
		return errors.New("registry blob lookup failed")
	}
	target = "https://" + instance.host + "/v2/" + instance.repository + "/blobs/uploads/"
	response, err = instance.do(ctx, http.MethodPost, target, "", nil)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return errors.New("registry upload initialization failed")
	}
	location, err := instance.uploadLocation(response.Header.Get("Location"))
	if err != nil {
		return err
	}
	query := location.Query()
	query.Set("digest", digest)
	location.RawQuery = query.Encode()
	response, err = instance.do(ctx, http.MethodPut, location.String(), "application/octet-stream", value)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return errors.New("registry blob upload failed")
	}
	if actual := response.Header.Get("Docker-Content-Digest"); actual != "" && actual != digest {
		return errors.New("registry blob digest mismatch")
	}
	return nil
}

func (instance *publisher) uploadLocation(value string) (*url.URL, error) {
	base, _ := url.Parse("https://" + instance.host)
	location, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("registry upload location is invalid")
	}
	location = base.ResolveReference(location)
	if location.Scheme != "https" || location.Host != instance.host ||
		!strings.HasPrefix(location.Path, "/v2/"+instance.repository+"/blobs/uploads/") {
		return nil, errors.New("registry upload location escaped repository")
	}
	return location, nil
}

func (instance *publisher) do(ctx context.Context, method, target, mediaType string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(instance.username, instance.password)
	request.Header.Set("User-Agent", "kodex-role-image-input-publisher/1")
	if mediaType != "" {
		request.Header.Set("Content-Type", mediaType)
	}
	response, err := instance.client.Do(request)
	if err != nil {
		return nil, errors.New("registry request failed")
	}
	if response.ContentLength > maximumResponse {
		_ = response.Body.Close()
		return nil, errors.New("registry response is oversized")
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(response.Body, maximumResponse+1), Closer: response.Body}
	return response, nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
