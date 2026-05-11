package secretresolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

func TestSecretValueRedactsAndBlocksSerialization(t *testing.T) {
	raw := "ghp_should_not_leak"
	value := NewSecretValue([]byte(raw))

	if got := string(value.Bytes()); got != raw {
		t.Fatalf("value bytes = %q, want original", got)
	}
	copied := value.Bytes()
	copied[0] = 'X'
	if got := string(value.Bytes()); got != raw {
		t.Fatalf("value bytes changed through copy = %q", got)
	}
	for _, rendered := range []string{fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value)} {
		if strings.Contains(rendered, raw) {
			t.Fatalf("rendered value leaked secret: %q", rendered)
		}
		if !strings.Contains(rendered, redactedSecret) {
			t.Fatalf("rendered value = %q, want redaction marker", rendered)
		}
	}
	if _, err := json.Marshal(value); !errors.Is(err, ErrSecretSerialization) {
		t.Fatalf("json.Marshal(value) err = %v, want %v", err, ErrSecretSerialization)
	}
	wrapped := struct {
		Token SecretValue `json:"token"`
	}{Token: value}
	if _, err := json.Marshal(wrapped); !errors.Is(err, ErrSecretSerialization) {
		t.Fatalf("json.Marshal(wrapped) err = %v, want %v", err, ErrSecretSerialization)
	}
	if text, err := value.MarshalText(); !errors.Is(err, ErrSecretSerialization) || text != nil {
		t.Fatalf("MarshalText() = %q, %v, want serialization error", string(text), err)
	}
}

func TestSecretValueClearZeroesInternalBuffer(t *testing.T) {
	value := newSecretValueOwned([]byte("token-to-clear"))
	alias := value.raw

	value.Clear()

	if value.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after Clear", value.Len())
	}
	if len(value.Bytes()) != 0 {
		t.Fatalf("Bytes() returned data after Clear")
	}
	for i, b := range alias {
		if b != 0 {
			t.Fatalf("alias[%d] = %d, want zero after Clear", i, b)
		}
	}
}

func TestMuxRoutesSecretOperations(t *testing.T) {
	backend := &fakeBackend{value: NewSecretValue([]byte("token")), status: SecretStatus{Present: true, Version: "v1"}}
	resolver, err := NewMux(map[string]Backend{StoreTypeKubernetesMountedSecret: backend})
	if err != nil {
		t.Fatalf("NewMux(): %v", err)
	}

	value, err := resolver.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/github#token"})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if got := string(value.Bytes()); got != "token" {
		t.Fatalf("value = %q, want token", got)
	}
	status, err := resolver.Check(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/github#token"})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if !status.Present || status.Version != "v1" {
		t.Fatalf("status = %+v, want present v1", status)
	}
}

func TestMuxRejectsUnknownBackend(t *testing.T) {
	resolver, err := NewMux(map[string]Backend{StoreTypeKubernetesMountedSecret: &fakeBackend{}})
	if err != nil {
		t.Fatalf("NewMux(): %v", err)
	}
	_, err = resolver.Resolve(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "kv/kodex/github"})
	if !errors.Is(err, ErrUnsupportedStoreType) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrUnsupportedStoreType)
	}
}

func TestMountedKubernetesBackendResolvesMountedSecret(t *testing.T) {
	root := t.TempDir()
	secretPath := filepath.Join(root, "default", "github-token")
	if err := os.MkdirAll(secretPath, 0o700); err != nil {
		t.Fatalf("mkdir secret path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretPath, "token"), []byte("mounted-token"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	backend, err := NewMountedKubernetesBackend(MountedKubernetesBackendConfig{Root: root})
	if err != nil {
		t.Fatalf("NewMountedKubernetesBackend(): %v", err)
	}

	value, err := backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/github-token#token"})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if got := string(value.Bytes()); got != "mounted-token" {
		t.Fatalf("value = %q, want mounted-token", got)
	}
	status, err := backend.Check(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/github-token#token"})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if !status.Present || status.Version == "" {
		t.Fatalf("status = %+v, want present with version", status)
	}
}

func TestMountedKubernetesBackendRejectsMissingSecret(t *testing.T) {
	backend, err := NewMountedKubernetesBackend(MountedKubernetesBackendConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewMountedKubernetesBackend(): %v", err)
	}
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/missing#token"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrSecretNotFound)
	}
	_, err = backend.Check(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/missing#token"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Check() err = %v, want %v", err, ErrSecretNotFound)
	}
}

func TestMountedKubernetesBackendRejectsOversizedSecretWithoutLeak(t *testing.T) {
	root := t.TempDir()
	secretPath := filepath.Join(root, "default", "oversized")
	if err := os.MkdirAll(secretPath, 0o700); err != nil {
		t.Fatalf("mkdir secret path: %v", err)
	}
	raw := "oversized-secret-value"
	if err := os.WriteFile(filepath.Join(secretPath, "token"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	backend, err := NewMountedKubernetesBackend(MountedKubernetesBackendConfig{Root: root, MaxSecretBytes: 4})
	if err != nil {
		t.Fatalf("NewMountedKubernetesBackend(): %v", err)
	}

	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "default/oversized#token"})
	if !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrSecretUnavailable)
	}
	if strings.Contains(err.Error(), raw) {
		t.Fatalf("Resolve() error leaked secret: %q", err.Error())
	}
}

func TestMountedKubernetesBackendRejectsInvalidRef(t *testing.T) {
	backend, err := NewMountedKubernetesBackend(MountedKubernetesBackendConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewMountedKubernetesBackend(): %v", err)
	}
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesMountedSecret, StoreRef: "../secret#token"})
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrInvalidRef)
	}
}

func TestEnvBackendResolvesEnvSecret(t *testing.T) {
	t.Setenv("KODEX_TEST_SECRET", "env-token")
	backend := NewEnvBackend()

	value, err := backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeEnv, StoreRef: "KODEX_TEST_SECRET"})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	defer value.Clear()
	if got := string(value.Bytes()); got != "env-token" {
		t.Fatalf("value = %q, want env-token", got)
	}
	status, err := backend.Check(context.Background(), SecretRef{StoreType: StoreTypeEnv, StoreRef: "KODEX_TEST_SECRET"})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if !status.Present || status.Version != "process-env" {
		t.Fatalf("status = %+v, want process-env", status)
	}
}

func TestEnvBackendRejectsMissingOrInvalidSecret(t *testing.T) {
	backend := NewEnvBackend()
	_, err := backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeEnv, StoreRef: "KODEX_MISSING_SECRET"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrSecretNotFound)
	}
	_, err = backend.Check(context.Background(), SecretRef{StoreType: StoreTypeEnv, StoreRef: "1BAD"})
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("Check() err = %v, want %v", err, ErrInvalidRef)
	}
}

func TestVaultBackendResolvesKVv2Secret(t *testing.T) {
	kv := &fakeVaultKVv2{
		secret: &vaultapi.KVSecret{
			Data:            map[string]interface{}{"token": "vault-token"},
			VersionMetadata: &vaultapi.KVVersionMetadata{Version: 7},
		},
	}
	backend, err := NewVaultBackend(VaultBackendConfig{KVv2Factory: func(mountPath string) VaultKVv2Client {
		if mountPath != "secret" {
			t.Fatalf("mountPath = %q, want secret", mountPath)
		}
		return kv
	}})
	if err != nil {
		t.Fatalf("NewVaultBackend(): %v", err)
	}

	value, err := backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	defer value.Clear()
	if got := string(value.Bytes()); got != "vault-token" {
		t.Fatalf("value = %q, want vault-token", got)
	}
	if kv.lastGetPath != "provider/github" {
		t.Fatalf("Get path = %q, want provider/github", kv.lastGetPath)
	}
	if kv.secret.Data != nil {
		t.Fatalf("Vault secret data was not scrubbed after Resolve")
	}

	kv.metadata = &vaultapi.KVMetadata{
		CurrentVersion: 7,
		Versions: map[string]vaultapi.KVVersionMetadata{
			"7": {Version: 7},
		},
	}
	getCallsBeforeCheck := kv.getCalls
	status, err := backend.Check(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if !status.Present || status.Version != "7" || kv.lastMetadataPath != "provider/github" {
		t.Fatalf("status = %+v metadataPath = %q, want present version 7", status, kv.lastMetadataPath)
	}
	if kv.getCalls != getCallsBeforeCheck {
		t.Fatalf("Check() called data Get: before=%d after=%d", getCallsBeforeCheck, kv.getCalls)
	}
	if kv.metadataCalls != 1 {
		t.Fatalf("metadataCalls = %d, want 1", kv.metadataCalls)
	}
}

func TestVaultBackendRejectsMissingOrUnsupportedSecret(t *testing.T) {
	kv := &fakeVaultKVv2{
		secret: &vaultapi.KVSecret{Data: map[string]interface{}{"token": map[string]string{"nested": "unsupported"}}},
	}
	backend, err := NewVaultBackend(VaultBackendConfig{KVv2Factory: func(string) VaultKVv2Client { return kv }})
	if err != nil {
		t.Fatalf("NewVaultBackend(): %v", err)
	}
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#missing"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve(missing key) err = %v, want %v", err, ErrSecretNotFound)
	}
	kv.secret = &vaultapi.KVSecret{Data: map[string]interface{}{"token": map[string]string{"nested": "unsupported"}}}
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("Resolve(unsupported type) err = %v, want %v", err, ErrSecretUnavailable)
	}
}

func TestVaultBackendCheckUsesMetadataOnly(t *testing.T) {
	kv := &fakeVaultKVv2{
		secret: &vaultapi.KVSecret{
			Data:            map[string]interface{}{"token": "vault-token"},
			VersionMetadata: &vaultapi.KVVersionMetadata{Version: 3},
		},
		metadata: &vaultapi.KVMetadata{
			CurrentVersion: 3,
			Versions: map[string]vaultapi.KVVersionMetadata{
				"3": {Version: 3},
			},
		},
	}
	backend, err := NewVaultBackend(VaultBackendConfig{KVv2Factory: func(string) VaultKVv2Client { return kv }})
	if err != nil {
		t.Fatalf("NewVaultBackend(): %v", err)
	}

	status, err := backend.Check(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if !status.Present || status.Version != "3" {
		t.Fatalf("status = %+v, want present version 3", status)
	}
	if kv.getCalls != 0 {
		t.Fatalf("Check() called data Get %d times, want 0", kv.getCalls)
	}
	if kv.metadataCalls != 1 {
		t.Fatalf("metadataCalls = %d, want 1", kv.metadataCalls)
	}
	if got := kv.secret.Data["token"]; got != "vault-token" {
		t.Fatalf("Check() mutated data secret = %v, want untouched", got)
	}
}

func TestVaultBackendCheckRejectsDeletedCurrentVersion(t *testing.T) {
	kv := &fakeVaultKVv2{
		metadata: &vaultapi.KVMetadata{
			CurrentVersion: 4,
			Versions: map[string]vaultapi.KVVersionMetadata{
				"4": {Version: 4, DeletionTime: time.Now()},
			},
		},
	}
	backend, err := NewVaultBackend(VaultBackendConfig{KVv2Factory: func(string) VaultKVv2Client { return kv }})
	if err != nil {
		t.Fatalf("NewVaultBackend(): %v", err)
	}

	_, err = backend.Check(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Check() err = %v, want %v", err, ErrSecretNotFound)
	}
}

func TestVaultBackendMapsNotFound(t *testing.T) {
	kv := &fakeVaultKVv2{err: &vaultapi.ResponseError{StatusCode: http.StatusNotFound}}
	backend, err := NewVaultBackend(VaultBackendConfig{KVv2Factory: func(string) VaultKVv2Client { return kv }})
	if err != nil {
		t.Fatalf("NewVaultBackend(): %v", err)
	}
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrSecretNotFound)
	}
	_, err = backend.Check(context.Background(), SecretRef{StoreType: StoreTypeVault, StoreRef: "secret/provider/github#token"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Check() err = %v, want %v", err, ErrSecretNotFound)
	}
}

type fakeBackend struct {
	value  SecretValue
	status SecretStatus
	err    error
}

type fakeVaultKVv2 struct {
	secret           *vaultapi.KVSecret
	metadata         *vaultapi.KVMetadata
	err              error
	metadataErr      error
	lastGetPath      string
	lastMetadataPath string
	getCalls         int
	metadataCalls    int
}

func (f *fakeVaultKVv2) Get(_ context.Context, secretPath string) (*vaultapi.KVSecret, error) {
	f.lastGetPath = secretPath
	f.getCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.secret, nil
}

func (f *fakeVaultKVv2) GetMetadata(_ context.Context, secretPath string) (*vaultapi.KVMetadata, error) {
	f.lastMetadataPath = secretPath
	f.metadataCalls++
	if f.metadataErr != nil {
		return nil, f.metadataErr
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.metadata, nil
}

func (b *fakeBackend) Resolve(context.Context, SecretRef) (SecretValue, error) {
	if b.err != nil {
		return SecretValue{}, b.err
	}
	return b.value, nil
}

func (b *fakeBackend) Check(context.Context, SecretRef) (SecretStatus, error) {
	if b.err != nil {
		return SecretStatus{}, b.err
	}
	return b.status, nil
}
