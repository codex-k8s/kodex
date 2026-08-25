package exactkubernetessecret

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	generationAnnotation = "kodex.dev/secret-generation"
	generationDataKey    = "_generation"
)

// MapConfig связывает клиент с одним Secret и закрытым набором data keys.
type MapConfig struct {
	ResourceName    string
	AllowedDataKeys []string
	Timeout         time.Duration
}

// MapSnapshot содержит монотонное поколение и exact readback Secret.
type MapSnapshot struct {
	ResourceVersion string
	Generation      uint64
	Data            map[string][]byte
}

// MapClient не предоставляет list, create или delete и не принимает resource
// name во время вызова.
type MapClient struct {
	config  MapConfig
	allowed map[string]struct{}
	client  *http.Client
}

// NewMap создаёт клиент одного заранее созданного Secret.
func NewMap(config MapConfig) (*MapClient, error) {
	allowed := make(map[string]struct{}, len(config.AllowedDataKeys))
	for _, key := range config.AllowedDataKeys {
		if key == generationDataKey || !dataKey.MatchString(key) {
			return nil, errors.New("exact Kubernetes Secret map key is invalid")
		}
		allowed[key] = struct{}{}
	}
	allowed[generationDataKey] = struct{}{}
	if !validResourceName(config.ResourceName) || len(allowed) == 0 ||
		len(allowed) > 33 || len(allowed) != len(config.AllowedDataKeys)+1 ||
		config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("exact Kubernetes Secret map configuration is invalid")
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
	return &MapClient{config: config, allowed: allowed, client: &http.Client{
		Transport: transport, Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("exact Kubernetes API redirect is forbidden")
		},
	}}, nil
}

// Read читает только зарегистрированный Secret. Поколение 0 разрешено только
// для заранее созданного пустого bootstrap-ресурса.
func (client *MapClient) Read(ctx context.Context) (MapSnapshot, error) {
	secret, generation, err := client.read(ctx)
	if err != nil {
		return MapSnapshot{}, err
	}
	return MapSnapshot{
		ResourceVersion: secret.Metadata.ResourceVersion,
		Generation:      generation,
		Data:            cloneData(secret.Data),
	}, nil
}

// CompareAndSwap обновляет весь закрытый набор keys и увеличивает generation
// ровно на единицу.
func (client *MapClient) CompareAndSwap(
	ctx context.Context,
	expectedResourceVersion string,
	expectedGeneration uint64,
	data map[string][]byte,
) (MapSnapshot, error) {
	if expectedResourceVersion == "" || data[generationDataKey] != nil || !client.validData(data) {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map CAS input is invalid")
	}
	secret, generation, err := client.read(ctx)
	if err != nil {
		return MapSnapshot{}, err
	}
	if secret.Metadata.ResourceVersion != expectedResourceVersion || generation != expectedGeneration {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map CAS rejected")
	}
	annotations := map[string]string{}
	if raw := secret.Metadata.raw["annotations"]; len(raw) > 0 {
		if json.Unmarshal(raw, &annotations) != nil {
			return MapSnapshot{}, errors.New("exact Kubernetes Secret annotations are invalid")
		}
	}
	annotations[generationAnnotation] = strconv.FormatUint(generation+1, 10)
	rawAnnotations, err := json.Marshal(annotations)
	if err != nil {
		return MapSnapshot{}, errors.New("encode exact Kubernetes Secret annotations")
	}
	secret.Metadata.raw["annotations"] = rawAnnotations
	expectedData := cloneData(data)
	expectedData[generationDataKey] = []byte(strconv.FormatUint(generation+1, 10))
	secret.Data = expectedData
	body, err := json.Marshal(secret)
	if err != nil {
		return MapSnapshot{}, errors.New("encode exact Kubernetes Secret map update")
	}
	request, err := client.request(ctx, http.MethodPut, bytes.NewReader(body))
	if err != nil {
		return MapSnapshot{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return MapSnapshot{}, errors.New("update exact Kubernetes Secret map")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
	if response.StatusCode == http.StatusConflict {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map CAS rejected")
	}
	if response.StatusCode != http.StatusOK {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map update rejected")
	}
	served, err := client.Read(ctx)
	if err != nil || served.ResourceVersion == expectedResourceVersion ||
		served.Generation != expectedGeneration+1 || !equalData(served.Data, expectedData) {
		return MapSnapshot{}, errors.New("exact Kubernetes Secret map served readback mismatch")
	}
	return served, nil
}

// Check подтверждает доступность и форму exact Secret.
func (client *MapClient) Check(ctx context.Context) error {
	_, err := client.Read(ctx)
	return err
}

// Close освобождает простаивающие connections.
func (client *MapClient) Close() { client.client.CloseIdleConnections() }

func (client *MapClient) read(ctx context.Context) (secretDocument, uint64, error) {
	request, err := client.request(ctx, http.MethodGet, nil)
	if err != nil {
		return secretDocument{}, 0, err
	}
	response, err := client.client.Do(request)
	if err != nil {
		return secretDocument{}, 0, errors.New("read exact Kubernetes Secret map")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumResponseBytes {
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map response is invalid")
	}
	var secret secretDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&secret) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		secret.APIVersion != "v1" || secret.Kind != "Secret" ||
		secret.Metadata.Name != client.config.ResourceName ||
		secret.Metadata.Namespace != KubernetesNamespace || secret.Metadata.ResourceVersion == "" ||
		secret.Type != "Opaque" || (secret.Immutable != nil && *secret.Immutable) {
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map binding is invalid")
	}
	generation, err := mapGeneration(secret.Metadata.raw["annotations"])
	servedGeneration, servedGenerationErr := dataGeneration(secret.Data)
	if err != nil || servedGenerationErr != nil || generation != servedGeneration ||
		(generation == 0 && len(secret.Data) != 0) ||
		(generation > 0 && !client.validData(secret.Data)) {
		return secretDocument{}, 0, errors.New("exact Kubernetes Secret map generation is invalid")
	}
	return secret, generation, nil
}

func (client *MapClient) request(ctx context.Context, method string, body io.Reader) (*http.Request, error) {
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
		return nil, errors.New("construct exact Kubernetes Secret map request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func (client *MapClient) validData(values map[string][]byte) bool {
	if len(values) == 0 || len(values) > len(client.allowed) {
		return false
	}
	for key, value := range values {
		if _, ok := client.allowed[key]; !ok || len(value) == 0 || len(value) > 1<<20 {
			return false
		}
	}
	return true
}

func mapGeneration(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var annotations map[string]string
	if json.Unmarshal(raw, &annotations) != nil {
		return 0, errors.New("decode exact Kubernetes Secret annotations")
	}
	value, ok := annotations[generationAnnotation]
	if !ok {
		return 0, nil
	}
	generation, err := strconv.ParseUint(value, 10, 64)
	if err != nil || generation == 0 {
		return 0, errors.New("parse exact Kubernetes Secret generation")
	}
	return generation, nil
}

func dataGeneration(data map[string][]byte) (uint64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	raw, ok := data[generationDataKey]
	if !ok {
		return 0, errors.New("exact Kubernetes Secret data generation is absent")
	}
	generation, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || generation == 0 {
		return 0, errors.New("parse exact Kubernetes Secret data generation")
	}
	return generation, nil
}

func cloneData(values map[string][]byte) map[string][]byte {
	clone := make(map[string][]byte, len(values))
	for key, value := range values {
		clone[key] = bytes.Clone(value)
	}
	return clone
}

func equalData(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	keys := make([]string, 0, len(left))
	for key := range left {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !bytes.Equal(left[key], right[key]) {
			return false
		}
	}
	return true
}
