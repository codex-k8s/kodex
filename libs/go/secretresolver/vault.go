package secretresolver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
)

// VaultKVv2Client is the subset of the official Vault Go SDK used by the backend.
type VaultKVv2Client interface {
	Get(ctx context.Context, secretPath string) (*vaultapi.KVSecret, error)
}

// VaultBackendConfig configures a Vault KV v2 secret resolver.
type VaultBackendConfig struct {
	Client      *vaultapi.Client
	KVv2Factory func(mountPath string) VaultKVv2Client
}

// VaultBackend resolves values from Vault KV v2 through the official Go SDK.
type VaultBackend struct {
	kvv2Factory func(mountPath string) VaultKVv2Client
}

type vaultKVv2Ref struct {
	mountPath  string
	secretPath string
	key        string
}

// NewVaultBackend creates a Vault KV v2 backend for refs in mount/path#key form.
func NewVaultBackend(cfg VaultBackendConfig) (*VaultBackend, error) {
	factory := cfg.KVv2Factory
	if factory == nil {
		if cfg.Client == nil {
			return nil, fmt.Errorf("%w: Vault client is required", ErrInvalidRef)
		}
		factory = func(mountPath string) VaultKVv2Client {
			return cfg.Client.KVv2(mountPath)
		}
	}
	return &VaultBackend{kvv2Factory: factory}, nil
}

// Resolve reads one Vault KV v2 field into an in-memory SecretValue.
func (b *VaultBackend) Resolve(ctx context.Context, ref SecretRef) (SecretValue, error) {
	if err := ctx.Err(); err != nil {
		return SecretValue{}, err
	}
	parsed, err := b.parse(ref)
	if err != nil {
		return SecretValue{}, err
	}
	kv := b.kvv2Factory(parsed.mountPath)
	if kv == nil {
		return SecretValue{}, fmt.Errorf("%w: nil Vault KV v2 client", ErrSecretUnavailable)
	}
	secret, err := kv.Get(ctx, parsed.secretPath)
	if vaultNotFound(err) {
		return SecretValue{}, ErrSecretNotFound
	}
	if err != nil {
		return SecretValue{}, fmt.Errorf("%w: Vault read failed", ErrSecretUnavailable)
	}
	if secret == nil || secret.Data == nil {
		return SecretValue{}, ErrSecretNotFound
	}
	raw, ok := secret.Data[parsed.key]
	if !ok {
		scrubVaultSecret(secret)
		return SecretValue{}, ErrSecretNotFound
	}
	value, err := vaultSecretValue(raw)
	scrubVaultSecret(secret)
	if err != nil {
		return SecretValue{}, err
	}
	return value, nil
}

// Check verifies Vault KV v2 field presence without returning the value.
func (b *VaultBackend) Check(ctx context.Context, ref SecretRef) (SecretStatus, error) {
	if err := ctx.Err(); err != nil {
		return SecretStatus{}, err
	}
	parsed, err := b.parse(ref)
	if err != nil {
		return SecretStatus{}, err
	}
	kv := b.kvv2Factory(parsed.mountPath)
	if kv == nil {
		return SecretStatus{}, fmt.Errorf("%w: nil Vault KV v2 client", ErrSecretUnavailable)
	}
	secret, err := kv.Get(ctx, parsed.secretPath)
	if vaultNotFound(err) {
		return SecretStatus{}, ErrSecretNotFound
	}
	if err != nil {
		return SecretStatus{}, fmt.Errorf("%w: Vault check failed", ErrSecretUnavailable)
	}
	if secret == nil || secret.Data == nil {
		return SecretStatus{}, ErrSecretNotFound
	}
	if _, ok := secret.Data[parsed.key]; !ok {
		scrubVaultSecret(secret)
		return SecretStatus{}, ErrSecretNotFound
	}
	version := vaultSecretVersion(secret)
	scrubVaultSecret(secret)
	return SecretStatus{Present: true, Version: version}, nil
}

func (b *VaultBackend) parse(ref SecretRef) (vaultKVv2Ref, error) {
	if b == nil || b.kvv2Factory == nil {
		return vaultKVv2Ref{}, fmt.Errorf("%w: Vault backend is not configured", ErrInvalidRef)
	}
	normalized, err := normalizeRef(ref)
	if err != nil {
		return vaultKVv2Ref{}, err
	}
	if normalized.StoreType != StoreTypeVault {
		return vaultKVv2Ref{}, ErrUnsupportedStoreType
	}
	return parseVaultKVv2Ref(normalized.StoreRef)
}

func parseVaultKVv2Ref(raw string) (vaultKVv2Ref, error) {
	pathPart, key, ok := strings.Cut(strings.TrimSpace(raw), "#")
	if !ok {
		return vaultKVv2Ref{}, ErrInvalidRef
	}
	mountPath, secretPath, ok := strings.Cut(pathPart, "/")
	parsed := vaultKVv2Ref{
		mountPath:  strings.TrimSpace(mountPath),
		secretPath: strings.Trim(strings.TrimSpace(secretPath), "/"),
		key:        strings.TrimSpace(key),
	}
	if !ok || !safeVaultPath(parsed.mountPath) || !safeVaultPath(parsed.secretPath) || !safeVaultKey(parsed.key) {
		return vaultKVv2Ref{}, ErrInvalidRef
	}
	return parsed, nil
}

func safeVaultPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "\x00") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func safeVaultKey(key string) bool {
	return key != "" && !strings.Contains(key, "\x00")
}

func vaultSecretValue(raw any) (SecretValue, error) {
	switch typed := raw.(type) {
	case string:
		return NewSecretValue([]byte(typed)), nil
	case []byte:
		value := NewSecretValue(typed)
		clearBytes(typed)
		return value, nil
	default:
		return SecretValue{}, fmt.Errorf("%w: Vault secret field has unsupported type", ErrSecretUnavailable)
	}
}

func scrubVaultSecret(secret *vaultapi.KVSecret) {
	if secret == nil {
		return
	}
	for key, value := range secret.Data {
		if raw, ok := value.([]byte); ok {
			clearBytes(raw)
		}
		secret.Data[key] = nil
	}
	secret.Data = nil
}

func vaultNotFound(err error) bool {
	if err == nil {
		return false
	}
	var responseErr *vaultapi.ResponseError
	return errors.As(err, &responseErr) && responseErr.StatusCode == http.StatusNotFound
}

func vaultSecretVersion(secret *vaultapi.KVSecret) string {
	if secret == nil || secret.VersionMetadata == nil {
		return ""
	}
	if secret.VersionMetadata.Version > 0 {
		return strconv.Itoa(secret.VersionMetadata.Version)
	}
	return ""
}
