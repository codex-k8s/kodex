package secretresolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestMuxRoutesSecretOperations(t *testing.T) {
	backend := &fakeBackend{value: NewSecretValue([]byte("token")), status: SecretStatus{Present: true, Version: "v1"}}
	resolver, err := NewMux(map[string]Backend{StoreTypeKubernetesSecret: backend})
	if err != nil {
		t.Fatalf("NewMux(): %v", err)
	}

	value, err := resolver.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/github#token"})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if got := string(value.Bytes()); got != "token" {
		t.Fatalf("value = %q, want token", got)
	}
	status, err := resolver.Check(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/github#token"})
	if err != nil {
		t.Fatalf("Check(): %v", err)
	}
	if !status.Present || status.Version != "v1" {
		t.Fatalf("status = %+v, want present v1", status)
	}
}

func TestMuxRejectsUnknownBackend(t *testing.T) {
	resolver, err := NewMux(map[string]Backend{StoreTypeKubernetesSecret: &fakeBackend{}})
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

	value, err := backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/github-token#token"})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if got := string(value.Bytes()); got != "mounted-token" {
		t.Fatalf("value = %q, want mounted-token", got)
	}
	status, err := backend.Check(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/github-token#token"})
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
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/missing#token"})
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrSecretNotFound)
	}
	_, err = backend.Check(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/missing#token"})
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

	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "default/oversized#token"})
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
	_, err = backend.Resolve(context.Background(), SecretRef{StoreType: StoreTypeKubernetesSecret, StoreRef: "../secret#token"})
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("Resolve() err = %v, want %v", err, ErrInvalidRef)
	}
}

type fakeBackend struct {
	value  SecretValue
	status SecretStatus
	err    error
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
