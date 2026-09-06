package providercredential

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

type catalogStoreFixture struct {
	providerCredentialStoreStub
	read func(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error)
}

func (store *catalogStoreFixture) ReadProviderCredentialExact(ctx context.Context, account string, descriptor kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	return store.read(ctx, account, descriptor)
}

type catalogAppFixture struct {
	providerAppServerStub
	observe func(context.Context, []byte, string) (ModelCatalog, error)
}

func (server *catalogAppFixture) ObserveModelCatalog(ctx context.Context, raw []byte, method string) (ModelCatalog, error) {
	return server.observe(ctx, raw, method)
}

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
