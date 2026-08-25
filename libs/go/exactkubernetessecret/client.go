// Package exactkubernetessecret предоставляет только CAS/readback одного
// заранее зарегистрированного namespaced Secret. Вызовы не принимают namespace,
// resource name или key и поэтому не являются универсальным Secret API.
package exactkubernetessecret

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	KubernetesAPIAddress = "https://kubernetes.default.svc:443"
	KubernetesServerName = "kubernetes.default.svc"
	KubernetesNamespace  = "kodex-system"
	KubernetesCAFile     = "/var/run/config/kubernetes.io/serviceaccount/ca.crt"
	KubernetesTokenFile  = "/var/run/secrets/tokens/kubernetes-api/token"
	maximumResponseBytes = 2 << 20
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
var dataKey = regexp.MustCompile(`^[A-Za-z0-9](?:[-._A-Za-z0-9]{0,252}[A-Za-z0-9])?$`)

type Config struct {
	ResourceName string
	DataKey      string
	Timeout      time.Duration
}

type Snapshot struct {
	ResourceVersion string
	Data            []byte
}

type metadata struct {
	Name            string
	Namespace       string
	ResourceVersion string
	raw             map[string]json.RawMessage
}

func (value *metadata) UnmarshalJSON(raw []byte) error {
	var preserved map[string]json.RawMessage
	if err := json.Unmarshal(raw, &preserved); err != nil {
		return err
	}
	var known struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		ResourceVersion string `json:"resourceVersion"`
	}
	if err := json.Unmarshal(raw, &known); err != nil {
		return err
	}
	value.Name, value.Namespace, value.ResourceVersion = known.Name, known.Namespace, known.ResourceVersion
	value.raw = preserved
	return nil
}

func (value metadata) MarshalJSON() ([]byte, error) {
	preserved := make(map[string]json.RawMessage, len(value.raw)+3)
	for key, raw := range value.raw {
		preserved[key] = raw
	}
	for key, field := range map[string]string{
		"name": value.Name, "namespace": value.Namespace, "resourceVersion": value.ResourceVersion,
	} {
		raw, err := json.Marshal(field)
		if err != nil {
			return nil, err
		}
		preserved[key] = raw
	}
	return json.Marshal(preserved)
}

type secretDocument struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   metadata          `json:"metadata"`
	Immutable  *bool             `json:"immutable,omitempty"`
	Type       string            `json:"type"`
	Data       map[string][]byte `json:"data"`
}

type Client struct {
	config Config
	client *http.Client
}

func New(config Config) (*Client, error) {
	if !validConfig(config) {
		return nil, errors.New("exact Kubernetes Secret configuration is invalid")
	}
	caRaw, err := os.ReadFile(KubernetesCAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read exact Kubernetes API CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse exact Kubernetes API CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: KubernetesServerName, RootCAs: roots,
	}, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	return &Client{config: config, client: &http.Client{
		Transport: transport, Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("exact Kubernetes API redirect is forbidden")
		},
	}}, nil
}

func validConfig(config Config) bool {
	return dnsLabel.MatchString(config.ResourceName) && dataKey.MatchString(config.DataKey) &&
		config.Timeout >= time.Second && config.Timeout <= 10*time.Second
}

// Read получает только зарегистрированный Secret и отклоняет лишние data keys.
func (client *Client) Read(ctx context.Context) (Snapshot, error) {
	secret, err := client.read(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ResourceVersion: secret.Metadata.ResourceVersion, Data: bytes.Clone(secret.Data[client.config.DataKey])}, nil
}

// CompareAndSwap обновляет только зарегистрированный data key с exact resourceVersion.
func (client *Client) CompareAndSwap(ctx context.Context, expectedResourceVersion string, data []byte) (Snapshot, error) {
	if expectedResourceVersion == "" || len(data) == 0 || len(data) > 1<<20 {
		return Snapshot{}, errors.New("exact Kubernetes Secret CAS input is invalid")
	}
	secret, err := client.read(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if secret.Metadata.ResourceVersion != expectedResourceVersion {
		return Snapshot{}, errors.New("exact Kubernetes Secret resourceVersion CAS rejected")
	}
	secret.Data[client.config.DataKey] = bytes.Clone(data)
	body, err := json.Marshal(secret)
	if err != nil {
		return Snapshot{}, errors.New("encode exact Kubernetes Secret update")
	}
	request, err := client.request(ctx, http.MethodPut, bytes.NewReader(body))
	if err != nil {
		return Snapshot{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return Snapshot{}, errors.New("update exact Kubernetes Secret")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
	if response.StatusCode == http.StatusConflict {
		return Snapshot{}, errors.New("exact Kubernetes Secret resourceVersion CAS rejected")
	}
	if response.StatusCode != http.StatusOK {
		return Snapshot{}, errors.New("exact Kubernetes Secret update rejected")
	}
	served, err := client.Read(ctx)
	if err != nil || served.ResourceVersion == expectedResourceVersion || !bytes.Equal(served.Data, data) {
		return Snapshot{}, errors.New("exact Kubernetes Secret served readback mismatch")
	}
	return served, nil
}

func (client *Client) Check(ctx context.Context) error {
	_, err := client.Read(ctx)
	return err
}

func (client *Client) read(ctx context.Context) (secretDocument, error) {
	request, err := client.request(ctx, http.MethodGet, nil)
	if err != nil {
		return secretDocument{}, err
	}
	response, err := client.client.Do(request)
	if err != nil {
		return secretDocument{}, errors.New("read exact Kubernetes Secret")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
		return secretDocument{}, errors.New("exact Kubernetes Secret read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumResponseBytes {
		return secretDocument{}, errors.New("exact Kubernetes Secret response is invalid")
	}
	var secret secretDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&secret) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		secret.APIVersion != "v1" || secret.Kind != "Secret" || secret.Metadata.Name != client.config.ResourceName ||
		secret.Metadata.Namespace != KubernetesNamespace || secret.Metadata.ResourceVersion == "" ||
		secret.Type != "Opaque" || (secret.Immutable != nil && *secret.Immutable) || len(secret.Data) != 1 ||
		len(secret.Data[client.config.DataKey]) == 0 || len(secret.Data[client.config.DataKey]) > 1<<20 {
		return secretDocument{}, errors.New("exact Kubernetes Secret binding is invalid")
	}
	return secret, nil
}

func (client *Client) request(ctx context.Context, method string, body io.Reader) (*http.Request, error) {
	tokenRaw, err := os.ReadFile(KubernetesTokenFile)
	if err != nil {
		return nil, errors.New("read exact Kubernetes API token")
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" || len(token) > 16<<10 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("exact Kubernetes API token is invalid")
	}
	endpoint := KubernetesAPIAddress + "/api/v1/namespaces/" + url.PathEscape(KubernetesNamespace) +
		"/secrets/" + url.PathEscape(client.config.ResourceName)
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("construct exact Kubernetes Secret request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func (client *Client) Close() { client.client.CloseIdleConnections() }
