package providercredential

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// TestGatewayCatalogServiceIntersectsApprovedModelsBeforeAliasValidation exercises the real credential service and HTTP parser with unrelated aliases, approved duplicates and malformed/partial bounded responses; it never calls the Codex adapter or invents capabilities.
// TestGatewayCatalogServiceIntersectsApprovedModelsBeforeAliasValidation проверяет реальный credential service и HTTP parser с посторонними aliases, повторами approved модели и неверными/неполными bounded ответами; Codex adapter не вызывается, capabilities не выдумываются.
func TestGatewayCatalogServiceIntersectsApprovedModelsBeforeAliasValidation(t *testing.T) {
	for _, test := range []struct {
		name, body string
		failure    ModelCatalogFailure
		models     int
	}{
		{"unrelated-aliases", `{"data":[{"id":"allowed-model"},{"id":"unrelated alias with spaces"},{"id":"unrelated alias with spaces"},{"id":"unrelated@alias"}]}`, CatalogFailureNone, 1},
		{"approved-missing", `{"data":[{"id":"unrelated alias"}]}`, CatalogFailureNone, 0},
		{"approved-duplicate", `{"data":[{"id":"allowed-model"},{"id":"allowed-model"}]}`, CatalogFailureUnverified, 0},
		{"malformed-id", `{"data":[{"id":"allowed-model"},{"id":12}]}`, CatalogFailureUnverified, 0},
		{"partial", `{"data":[{"id":"allowed-model"}],"has_more":true}`, CatalogFailureUnverified, 0},
		{"null-data", `{"data":null}`, CatalogFailureUnverified, 0},
		{"record-budget", `{"data":[` + strings.Repeat(`{"id":"unrelated"},`, maximumAPIModelRecords) + `{"id":"allowed-model"}]}`, CatalogFailureUnverified, 0},
		{"body-budget", strings.Repeat(" ", maximumModelCatalogBytes+1), CatalogFailureUnverified, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := gatewayCatalogFixture()
			material, err := runtimecontract.EncodeGatewayProviderAuthentication(config.DefinitionKey(), []byte("synthetic-gateway-key"))
			if err != nil {
				t.Fatal(err)
			}
			descriptor := kubernetesstore.ProviderCredentialDescriptor{SecretName: "fixture", SecretUID: "fixture-uid", SecretResourceVersion: "13", ContentSHA256: "fixture-digest"}
			reads, requests := 0, 0
			store := &catalogStoreFixture{read: func(_ context.Context, account string, got kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
				reads++
				if account != "pacc_fixture01" || got != descriptor {
					t.Fatal("exact credential affinity changed")
				}
				return material, nil
			}}
			app := &catalogAppFixture{observe: func(context.Context, []byte, string) (ModelCatalog, error) {
				t.Fatal("gateway catalog fell back to Codex")
				return ModelCatalog{}, nil
			}}
			service, err := New(t.Context(), store, app, DefaultDeviceAuthorizationTTL())
			if err != nil {
				t.Fatal(err)
			}
			if err := service.ConfigureModelProvider(config); err != nil {
				t.Fatal(err)
			}
			service.gatewayHTTP = catalogHTTPFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.String() != config.BaseURL+"/models" || request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer synthetic-gateway-key" {
					t.Fatal("gateway request boundary changed")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			result, err := service.ObserveModelCatalogForDefinition(t.Context(), "pacc_fixture01", descriptor, CatalogMethodAPIKey, config.DefinitionKey())
			if err != nil || result.Failure != test.failure || len(result.Models) != test.models || reads != 1 || requests != 1 {
				t.Fatal("gateway catalog outcome or single-call boundary changed")
			}
			for _, model := range result.Models {
				if model.ID != "allowed-model" || len(model.ReasoningEfforts) != 0 || model.DefaultReasoningEffort != "" || model.IsDefault {
					t.Fatal("unapproved model or unsupported capability escaped the allowlist")
				}
			}
			if !bytes.Equal(material, make([]byte, len(material))) {
				t.Fatal("credential material survived observation")
			}
		})
	}
}

// gatewayCatalogFixture supplies synthetic Git-owned routing without any real provider credential.
// gatewayCatalogFixture задаёт synthetic Git-owned routing без real provider credential.
func gatewayCatalogFixture() runtimecontract.ModelProviderConfiguration {
	return runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.4/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "allowed-model", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
}

// TestGatewayCatalogKeepsCredentialAndModelAffinity rejects other definitions and never invents reasoning capabilities.
// TestGatewayCatalogKeepsCredentialAndModelAffinity отклоняет другие definitions и не выдумывает reasoning capabilities.
func TestGatewayCatalogKeepsCredentialAndModelAffinity(t *testing.T) {
	config := gatewayCatalogFixture()
	service := &Service{}
	if err := service.ConfigureModelProvider(config); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service.gatewayHTTP = catalogHTTPFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() != config.BaseURL+"/models" || request.Method != "GET" || request.Header.Get("Authorization") != "Bearer synthetic-gateway-key" {
			t.Fatal("gateway request boundary changed")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"allowed-model"},{"id":"other-model"}]}`))}, nil
	})
	raw, err := runtimecontract.EncodeGatewayProviderAuthentication(config.DefinitionKey(), []byte("synthetic-gateway-key"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.observeGatewayCatalog(t.Context(), raw)
	if err != nil || result.Failure != CatalogFailureNone || len(result.Models) != 1 || result.Models[0].ID != "allowed-model" || len(result.Models[0].ReasoningEfforts) != 0 {
		t.Fatal("gateway intersection failed")
	}
	wrong, _ := runtimecontract.EncodeGatewayProviderAuthentication("opencode-other-gateway", []byte("synthetic-gateway-key"))
	rejected, err := service.observeGatewayCatalog(t.Context(), wrong)
	if err != nil || rejected.Failure == CatalogFailureNone || calls != 1 {
		t.Fatal("foreign definition reached gateway")
	}
}

// TestGatewayCatalogRejectsAuthorizationAndPartialResponses maps safe failures without retry or fallback.
// TestGatewayCatalogRejectsAuthorizationAndPartialResponses отображает safe failures без retry или fallback.
func TestGatewayCatalogRejectsAuthorizationAndPartialResponses(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		failure    ModelCatalogFailure
	}{
		{"revoked", `{}`, 401, CatalogFailureAuthorization}, {"forbidden", `{}`, 403, CatalogFailureAuthorization},
		{"redirect", `{}`, 302, CatalogFailureUnavailable}, {"partial", `{"data":[],"has_more":true}`, 200, CatalogFailureUnverified},
		{"duplicate", `{"data":[{"id":"allowed-model"},{"id":"allowed-model"}]}`, 200, CatalogFailureUnverified},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := gatewayCatalogFixture()
			service := &Service{}
			if err := service.ConfigureModelProvider(config); err != nil {
				t.Fatal(err)
			}
			calls := 0
			service.gatewayHTTP = catalogHTTPFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			raw, _ := runtimecontract.EncodeGatewayProviderAuthentication(config.DefinitionKey(), []byte("synthetic-gateway-key"))
			result, err := service.observeGatewayCatalog(t.Context(), raw)
			if err != nil || result.Failure != test.failure || len(result.Models) != 0 || calls != 1 {
				t.Fatal("gateway failure boundary changed")
			}
		})
	}
}

// TestGatewayMaterializationRejectsWrongDefinitionBeforeStorage prevents a provider switch through a key upload.
// TestGatewayMaterializationRejectsWrongDefinitionBeforeStorage запрещает provider switch через key upload.
func TestGatewayMaterializationRejectsWrongDefinitionBeforeStorage(t *testing.T) {
	service := &Service{}
	if err := service.ConfigureModelProvider(gatewayCatalogFixture()); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.MaterializeGatewayAPIKey(t.Context(), "pauth_fixture0001", "pacc_fixture0001", "openai-codex", []byte("synthetic-gateway-key"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal("foreign definition accepted")
	}
}
