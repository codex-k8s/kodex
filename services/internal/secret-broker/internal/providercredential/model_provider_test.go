package providercredential

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

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
