package prototypematerial

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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const (
	maximumSecretResponseBytes = 2 << 20
	maximumDeliveryFileBytes   = 1 << 20
)

// KubernetesConfig задаёт один exact namespace Kubernetes API boundary.
type KubernetesConfig struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	Timeout       time.Duration
}

type secretMetadata struct {
	Name            string
	Namespace       string
	ResourceVersion string
	Annotations     map[string]string
	raw             map[string]json.RawMessage
}

func (metadata *secretMetadata) UnmarshalJSON(raw []byte) error {
	var preserved map[string]json.RawMessage
	if err := json.Unmarshal(raw, &preserved); err != nil {
		return err
	}
	var known struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		ResourceVersion string            `json:"resourceVersion"`
		Annotations     map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(raw, &known); err != nil {
		return err
	}
	metadata.Name = known.Name
	metadata.Namespace = known.Namespace
	metadata.ResourceVersion = known.ResourceVersion
	metadata.Annotations = known.Annotations
	metadata.raw = preserved
	return nil
}

func (metadata secretMetadata) MarshalJSON() ([]byte, error) {
	preserved := make(map[string]json.RawMessage, len(metadata.raw)+4)
	for key, value := range metadata.raw {
		preserved[key] = value
	}
	for key, value := range map[string]string{
		"name": metadata.Name, "namespace": metadata.Namespace,
		"resourceVersion": metadata.ResourceVersion,
	} {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		preserved[key] = raw
	}
	if metadata.Annotations != nil {
		raw, err := json.Marshal(metadata.Annotations)
		if err != nil {
			return nil, err
		}
		preserved["annotations"] = raw
	}
	return json.Marshal(preserved)
}

type secretDocument struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   secretMetadata    `json:"metadata"`
	Immutable  *bool             `json:"immutable,omitempty"`
	Type       string            `json:"type"`
	Data       map[string][]byte `json:"data"`
}

type materialDocument struct {
	Version uint64            `json:"version"`
	Digest  string            `json:"digest_sha256"`
	Data    map[string]string `json:"data"`
}

// KubernetesDelivery реализует SecretDelivery только для закрытого registry.
type KubernetesDelivery struct {
	config   KubernetesConfig
	registry DeliveryRegistry
	client   *http.Client
}

// NewKubernetesDelivery создаёт writer/readback для precreated exact Secrets.
func NewKubernetesDelivery(
	config KubernetesConfig,
	registry DeliveryRegistry,
) (*KubernetesDelivery, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != Namespace ||
		config.CAFile != KubernetesCAFile || config.TokenFile != KubernetesTokenFile ||
		config.Timeout < time.Second || config.Timeout > 10*time.Second ||
		len(registry.targets) == 0 {
		return nil, errors.New("prototype Kubernetes delivery configuration is invalid")
	}
	client, err := newKubernetesHTTPClient(config)
	if err != nil {
		return nil, err
	}
	return &KubernetesDelivery{
		config: config, registry: registry,
		client: client,
	}, nil
}

func newKubernetesHTTPClient(config KubernetesConfig) (*http.Client, error) {
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read prototype Kubernetes API CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("prototype Kubernetes API CA is invalid")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    roots, ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
		MaxIdleConns:      4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("prototype Kubernetes API redirect is forbidden")
		},
	}, nil
}

func (delivery *KubernetesDelivery) ReadKV2(
	ctx context.Context,
	path string,
) (repository.SecretMaterial, bool, error) {
	target, err := delivery.registry.target(path)
	if err != nil {
		return repository.SecretMaterial{}, false, err
	}
	secret, found, err := delivery.readSecret(ctx, target.resourceName)
	if err != nil || !found {
		if !found && err == nil {
			return repository.SecretMaterial{}, false, errors.New("prototype delivery Secret is absent")
		}
		return repository.SecretMaterial{}, false, err
	}
	return materialFromSecret(secret, target)
}

func (delivery *KubernetesDelivery) CreateKV2(
	ctx context.Context,
	path string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	return delivery.write(ctx, path, 0, data, true)
}

func (delivery *KubernetesDelivery) WriteKV2CAS(
	ctx context.Context,
	path string,
	expectedVersion uint64,
	data map[string]string,
) (repository.SecretMaterial, error) {
	if expectedVersion == 0 {
		return repository.SecretMaterial{}, errors.New("prototype delivery CAS version is invalid")
	}
	return delivery.write(ctx, path, expectedVersion, data, false)
}

func (delivery *KubernetesDelivery) write(
	ctx context.Context,
	path string,
	expectedVersion uint64,
	data map[string]string,
	create bool,
) (repository.SecretMaterial, error) {
	target, err := delivery.registry.target(path)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if err := target.validateData(data); err != nil {
		return repository.SecretMaterial{}, err
	}
	secret, found, err := delivery.readSecret(ctx, target.resourceName)
	if err != nil || !found {
		return repository.SecretMaterial{}, errors.New("read precreated prototype delivery Secret")
	}
	current, materialFound, err := materialFromSecret(secret, target)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if create && materialFound {
		return current, nil
	}
	if !create && (!materialFound || current.Version != expectedVersion) {
		return repository.SecretMaterial{}, errors.New("prototype delivery semantic CAS rejected")
	}
	nextVersion := uint64(1)
	if materialFound {
		nextVersion = current.Version + 1
		if nextVersion <= current.Version {
			return repository.SecretMaterial{}, errors.New("prototype delivery version overflow")
		}
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, errors.New("digest prototype delivery material")
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	if secret.Metadata.Annotations == nil {
		secret.Metadata.Annotations = make(map[string]string)
	}
	switch target.mode {
	case storageModeDocument:
		raw, marshalErr := json.Marshal(materialDocument{
			Version: nextVersion, Digest: digest, Data: cloneData(data),
		})
		if marshalErr != nil {
			return repository.SecretMaterial{}, errors.New("encode prototype delivery document")
		}
		secret.Data[target.storageKey] = raw
	case storageModeDirect:
		for logical, rule := range target.fields {
			delete(secret.Data, rule.physical)
			if value := data[logical]; value != "" {
				secret.Data[rule.physical] = []byte(value)
			}
		}
		versionKey, digestKey := metadataKeys(target.path)
		secret.Metadata.Annotations[versionKey] = strconv.FormatUint(nextVersion, 10)
		secret.Metadata.Annotations[digestKey] = digest
	default:
		return repository.SecretMaterial{}, errors.New("prototype delivery storage mode is invalid")
	}
	oldResourceVersion := secret.Metadata.ResourceVersion
	if err := delivery.putSecret(ctx, secret); err != nil {
		return repository.SecretMaterial{}, err
	}
	servedSecret, servedFound, err := delivery.readSecret(ctx, target.resourceName)
	if err != nil || !servedFound || servedSecret.Metadata.ResourceVersion == oldResourceVersion {
		return repository.SecretMaterial{}, errors.New("prototype delivery Kubernetes readback is invalid")
	}
	served, servedMaterialFound, err := materialFromSecret(servedSecret, target)
	if err != nil || !servedMaterialFound || served.Version != nextVersion ||
		served.Digest != digest || !equalData(served.Data, data) {
		return repository.SecretMaterial{}, errors.New("prototype delivery material readback mismatch")
	}
	return served, nil
}

func materialFromSecret(
	secret secretDocument,
	target deliveryTarget,
) (repository.SecretMaterial, bool, error) {
	if secret.APIVersion != "v1" || secret.Kind != "Secret" ||
		secret.Metadata.Name != target.resourceName ||
		secret.Metadata.Namespace != Namespace ||
		secret.Metadata.ResourceVersion == "" || secret.Type != "Opaque" {
		return repository.SecretMaterial{}, false, errors.New("prototype delivery Secret binding is invalid")
	}
	if len(target.allowedKeys) == 0 || len(secret.Data) > len(target.allowedKeys) {
		return repository.SecretMaterial{}, false, errors.New("prototype delivery Secret key set is invalid")
	}
	for key := range secret.Data {
		if _, ok := target.allowedKeys[key]; !ok {
			return repository.SecretMaterial{}, false, errors.New("prototype delivery Secret key set is invalid")
		}
	}
	var document materialDocument
	switch target.mode {
	case storageModeDocument:
		raw := secret.Data[target.storageKey]
		if len(raw) == 0 {
			return repository.SecretMaterial{}, false, nil
		}
		if err := decodeStrictJSON(raw, &document); err != nil {
			return repository.SecretMaterial{}, false, errors.New("prototype delivery document is invalid")
		}
	case storageModeDirect:
		versionKey, digestKey := metadataKeys(target.path)
		versionRaw := secret.Metadata.Annotations[versionKey]
		digest := secret.Metadata.Annotations[digestKey]
		if versionRaw == "" && digest == "" {
			return repository.SecretMaterial{}, false, nil
		}
		version, err := strconv.ParseUint(versionRaw, 10, 64)
		if err != nil {
			return repository.SecretMaterial{}, false, errors.New("prototype delivery version is invalid")
		}
		data := make(map[string]string, len(target.fields))
		for logical, rule := range target.fields {
			if raw, ok := secret.Data[rule.physical]; ok && len(raw) > 0 {
				data[logical] = string(raw)
			}
		}
		document = materialDocument{Version: version, Digest: digest, Data: data}
	default:
		return repository.SecretMaterial{}, false, errors.New("prototype delivery storage mode is invalid")
	}
	if document.Version == 0 || len(document.Digest) != 64 ||
		target.validateData(document.Data) != nil {
		return repository.SecretMaterial{}, false, errors.New("prototype delivery material metadata is invalid")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(document.Data)
	if err != nil || digest != document.Digest {
		return repository.SecretMaterial{}, false, errors.New("prototype delivery digest mismatch")
	}
	return repository.SecretMaterial{
		Version: document.Version, Digest: document.Digest, Data: cloneData(document.Data),
	}, true, nil
}

func (delivery *KubernetesDelivery) readSecret(
	ctx context.Context,
	name string,
) (secretDocument, bool, error) {
	if !dnsLabel(name) {
		return secretDocument{}, false, errors.New("prototype Secret resource is not registered")
	}
	request, err := delivery.request(ctx, http.MethodGet, name, nil)
	if err != nil {
		return secretDocument{}, false, err
	}
	response, err := delivery.client.Do(request)
	if err != nil {
		return secretDocument{}, false, errors.New("read prototype Kubernetes Secret")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumSecretResponseBytes))
		return secretDocument{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumSecretResponseBytes))
		return secretDocument{}, false, errors.New("prototype Kubernetes Secret read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumSecretResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumSecretResponseBytes {
		return secretDocument{}, false, errors.New("prototype Kubernetes Secret response is invalid")
	}
	var secret secretDocument
	if err := decodeStrictJSON(raw, &secret); err != nil {
		return secretDocument{}, false, errors.New("decode prototype Kubernetes Secret")
	}
	return secret, true, nil
}

func (delivery *KubernetesDelivery) putSecret(ctx context.Context, secret secretDocument) error {
	body, err := json.Marshal(secret)
	if err != nil {
		return errors.New("encode prototype Kubernetes Secret update")
	}
	request, err := delivery.request(ctx, http.MethodPut, secret.Metadata.Name, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := delivery.client.Do(request)
	if err != nil {
		return errors.New("update prototype Kubernetes Secret")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumSecretResponseBytes))
	if response.StatusCode == http.StatusConflict {
		return errors.New("prototype Kubernetes Secret resourceVersion CAS rejected")
	}
	if response.StatusCode != http.StatusOK {
		return errors.New("prototype Kubernetes Secret update rejected")
	}
	return nil
}

func (delivery *KubernetesDelivery) request(
	ctx context.Context,
	method string,
	name string,
	body io.Reader,
) (*http.Request, error) {
	tokenRaw, err := os.ReadFile(delivery.config.TokenFile)
	if err != nil {
		return nil, errors.New("read prototype Kubernetes API token")
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" || len(token) > 16<<10 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("prototype Kubernetes API token is invalid")
	}
	endpoint := delivery.config.Address + "/api/v1/namespaces/" +
		url.PathEscape(delivery.config.Namespace) + "/secrets/" + url.PathEscape(name)
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("construct prototype Kubernetes Secret request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

// Close освобождает только idle HTTP connections; credential values не кэшируются.
func (delivery *KubernetesDelivery) Close() {
	delivery.client.CloseIdleConnections()
}

// FileDelivery читает exact mounted documents без Kubernetes API authority.
type FileDelivery struct {
	registry DeliveryRegistry
}

func NewFileDelivery(registry DeliveryRegistry) (*FileDelivery, error) {
	if len(registry.targets) == 0 {
		return nil, errors.New("prototype file delivery registry is empty")
	}
	for _, target := range registry.targets {
		if target.mode != storageModeDocument || !filepath.IsAbs(target.filePath) ||
			!strings.HasPrefix(target.filePath, DeliveryMountRoot+string(os.PathSeparator)) {
			return nil, errors.New("prototype file delivery target is invalid")
		}
	}
	return &FileDelivery{registry: registry}, nil
}

func (delivery *FileDelivery) ReadKV2(
	_ context.Context,
	path string,
) (repository.SecretMaterial, bool, error) {
	target, err := delivery.registry.target(path)
	if err != nil {
		return repository.SecretMaterial{}, false, err
	}
	raw, found, err := readMountedDocument(target.filePath)
	if err != nil || !found {
		return repository.SecretMaterial{}, found, err
	}
	var document materialDocument
	if err := decodeStrictJSON(raw, &document); err != nil || document.Version == 0 ||
		len(document.Digest) != 64 || target.validateData(document.Data) != nil {
		return repository.SecretMaterial{}, false, errors.New("prototype mounted delivery document is invalid")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(document.Data)
	if err != nil || digest != document.Digest {
		return repository.SecretMaterial{}, false, errors.New("prototype mounted delivery digest mismatch")
	}
	return repository.SecretMaterial{Version: document.Version, Digest: digest, Data: cloneData(document.Data)}, true, nil
}

func (*FileDelivery) CreateKV2(context.Context, string, map[string]string) (repository.SecretMaterial, error) {
	return repository.SecretMaterial{}, errors.New("prototype mounted delivery is read-only")
}

func (*FileDelivery) WriteKV2CAS(context.Context, string, uint64, map[string]string) (repository.SecretMaterial, error) {
	return repository.SecretMaterial{}, errors.New("prototype mounted delivery is read-only")
}

func (*FileDelivery) Close() {}

func readMountedDocument(path string) ([]byte, bool, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("resolve prototype mounted delivery document")
	}
	relative, err := filepath.Rel(filepath.Dir(path), resolved)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, "../") {
		return nil, false, errors.New("prototype mounted delivery document escapes its directory")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumDeliveryFileBytes {
		return nil, false, errors.New("prototype mounted delivery document is unsafe")
	}
	if info.Size() == 0 {
		return nil, false, nil
	}
	raw, err := os.ReadFile(resolved)
	if err != nil || len(raw) == 0 || len(raw) > maximumDeliveryFileBytes {
		return nil, false, errors.New("read prototype mounted delivery document")
	}
	return raw, true, nil
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func cloneData(data map[string]string) map[string]string {
	result := make(map[string]string, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}

func equalData(first, second map[string]string) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if second[key] != value {
			return false
		}
	}
	return true
}

var (
	_ repository.SecretDelivery = (*KubernetesDelivery)(nil)
	_ repository.SecretDelivery = (*FileDelivery)(nil)
)
