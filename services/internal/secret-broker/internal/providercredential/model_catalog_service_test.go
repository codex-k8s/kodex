package providercredential

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// catalogStoreFixture replaces only exact credential reads with a programmable callback while embedding
// the broader synthetic store stub; it provides no real Kubernetes or owner authorization.
// catalogStoreFixture подменяет только точное credential чтение программируемым callback при встроенной
// общей synthetic store подмене; реальных Kubernetes и owner авторизации нет.
type catalogStoreFixture struct {
	providerCredentialStoreStub
	read func(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error)
}

// ReadProviderCredentialExact forwards account and full descriptor unchanged to the fixture callback so
// tests can verify the service performs the intended exact read.
// ReadProviderCredentialExact передаёт account и полный descriptor без изменений в fixture callback для
// проверки нужного точного чтения сервисом.
func (store *catalogStoreFixture) ReadProviderCredentialExact(ctx context.Context, account string, descriptor kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	return store.read(ctx, account, descriptor)
}

// catalogAppFixture injects one model-observation callback over the synthetic app-server stub to model
// success, invalid results and lifecycle cancellation without provider access.
// catalogAppFixture внедряет один model-observation callback поверх synthetic app-server подмены для
// моделирования успеха, неверных результатов и lifecycle cancellation без provider доступа.
type catalogAppFixture struct {
	providerAppServerStub
	observe func(context.Context, []byte, string) (ModelCatalog, error)
}

// ObserveModelCatalog relays context, credential bytes and method to the programmed fixture callback
// without copying or performing network operations.
// ObserveModelCatalog передаёт context, credential байты и method в запрограммированный fixture callback
// без копирования и сетевых операций.
func (server *catalogAppFixture) ObserveModelCatalog(ctx context.Context, raw []byte, method string) (ModelCatalog, error) {
	return server.observe(ctx, raw, method)
}

// TestCatalogServiceExactCredentialAndDiscardedMaterial checks one exact read before observation,
// rejection of invalid capabilities/source/failure/time, shutdown cancellation and clearing of the
// returned synthetic credential buffer.
// TestCatalogServiceExactCredentialAndDiscardedMaterial проверяет одно точное чтение до наблюдения,
// отказ неверным capabilities/source/failure/time, shutdown cancellation и обнуление возвращённого
// synthetic credential буфера.
func TestCatalogServiceExactCredentialAndDiscardedMaterial(t *testing.T) {
	for _, mode := range []string{"allowed", "foreign_descriptor", "malformed_capability", "source_mismatch", "failed_with_models", "old_observation", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			lifecycle, cancel := context.WithCancel(t.Context())
			defer cancel()
			descriptor := kubernetesstore.ProviderCredentialDescriptor{SecretName: "fixture", SecretUID: "fixture-uid", SecretResourceVersion: "13", ContentSHA256: "fixture-digest"}
			material := []byte("synthetic-credential-material")
			reads, observations := 0, 0
			store := &catalogStoreFixture{read: func(_ context.Context, account string, got kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
				reads++
				if account != "pacc_fixture01" || got != descriptor || mode == "foreign_descriptor" {
					return nil, errors.New("fixture exact credential rejected")
				}
				return material, nil
			}}
			app := &catalogAppFixture{observe: func(ctx context.Context, raw []byte, method string) (ModelCatalog, error) {
				observations++
				if !bytes.Equal(raw, material) || method != CatalogMethodAPIKey {
					t.Fatal("credential binding changed")
				}
				result := ModelCatalog{ObservedAt: time.Now().UTC(), Source: CatalogRemoteAPI, Models: catalogCapabilityFixture(), Failure: CatalogFailureNone}
				switch mode {
				case "malformed_capability":
					result.Models[0].DefaultReasoningEffort = "unsupported"
				case "source_mismatch":
					result.Source = CatalogRemoteCodex
				case "failed_with_models":
					result.Failure = CatalogFailureUnavailable
				case "old_observation":
					result.ObservedAt = time.Now().Add(-time.Hour)
				case "shutdown":
					cancel()
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
						t.Fatal("observer did not inherit shutdown")
					}
				}
				return result, nil
			}}
			service, err := New(lifecycle, store, app, DefaultDeviceAuthorizationTTL())
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.ObserveModelCatalog(t.Context(), "pacc_fixture01", descriptor, CatalogMethodAPIKey)
			if mode == "allowed" {
				if err != nil || result.Failure != CatalogFailureNone || len(result.Models) != 2 {
					t.Fatal("valid catalog was rejected")
				}
			} else if len(result.Models) != 0 || err == nil && result.Failure == CatalogFailureNone {
				t.Fatal("invalid catalog escaped service")
			}
			if reads != 1 || mode == "foreign_descriptor" && observations != 0 {
				t.Fatal("credential read was repeated or bypassed")
			}
			if observations > 0 && !bytes.Equal(material, make([]byte, len(material))) {
				t.Fatal("credential material was retained")
			}
		})
	}
}
