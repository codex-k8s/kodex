package runtimecontract_test

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// modelProviderFixture returns synthetic deployment-owned configuration with no live destinations.
// modelProviderFixture возвращает синтетическую конфигурацию без действующих destinations.
func modelProviderFixture() runtimecontract.ModelProviderConfiguration {
	return runtimecontract.ModelProviderConfiguration{
		Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git",
		SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp",
		ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1",
		AddressCIDRs: []string{"192.0.2.40/32"},
		Models:       []runtimecontract.ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}},
	}
}

// TestModelProviderConfigurationClosedDecoder rejects ambiguous keys, credentials and extra documents.
// TestModelProviderConfigurationClosedDecoder отклоняет неоднозначные keys, credentials и лишние documents.
func TestModelProviderConfigurationClosedDecoder(t *testing.T) {
	config := modelProviderFixture()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := runtimecontract.DecodeModelProviderConfiguration(raw)
	if err != nil || decoded.BaseURL != config.BaseURL {
		t.Fatal("valid nonsecret configuration rejected", err)
	}
	for name, input := range map[string][]byte{
		"unknown credential": []byte(strings.Replace(string(raw), "{", `{"api_key":"synthetic-do-not-accept",`, 1)),
		"duplicate engine":   []byte(strings.Replace(string(raw), "{", `{"engine":"codex",`, 1)),
		"extra document":     append(append([]byte{}, raw...), []byte(" {}")...),
		"oversized":          []byte(strings.Repeat(" ", runtimecontract.MaximumModelProviderConfigurationBytes+1)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runtimecontract.DecodeModelProviderConfiguration(input)
			if !errors.Is(err, runtimecontract.ErrModelProviderConfiguration) {
				t.Fatal("invalid configuration did not return safe contract error")
			}
		})
	}
}

// TestModelProviderConfigurationRejectsEndpointAliases preserves the exact HTTPS authority/route.
// TestModelProviderConfigurationRejectsEndpointAliases сохраняет exact HTTPS authority/route.
func TestModelProviderConfigurationRejectsEndpointAliases(t *testing.T) {
	for _, endpoint := range []string{
		"http://gateway.example.invalid/v1", "https://gateway.example.invalid:8443/v1",
		"https://gateway.example.invalid:/v1", "https://user:synthetic@gateway.example.invalid/v1",
		"https://127.0.0.1/v1", "https://*.example.invalid/v1", "https://GATEWAY.example.invalid/v1",
		"https://gateway.example.invalid./v1", "https://gateway.example.invalid/v1/",
		"https://gateway.example.invalid/v1?key=synthetic", "https://gateway.example.invalid/v1?",
		"https://gateway.example.invalid/v1#fragment", "https://gateway.example.invalid/v1%2fother",
		"https://gateway.example.invalid/v1/../other", "https://gateway.example.invalid//v1",
	} {
		t.Run(endpoint, func(t *testing.T) {
			config := modelProviderFixture()
			config.BaseURL = endpoint
			if !errors.Is(config.Validate(), runtimecontract.ErrModelProviderConfiguration) {
				t.Fatal("endpoint alias accepted")
			}
		})
	}
}

// TestModelProviderConfigurationRejectsUnpinnedInputs closes unsupported ownership/version and bounds.
// TestModelProviderConfigurationRejectsUnpinnedInputs отклоняет неподдержанные ownership/version и bounds.
func TestModelProviderConfigurationRejectsUnpinnedInputs(t *testing.T) {
	for name, mutate := range map[string]func(*runtimecontract.ModelProviderConfiguration){
		"UI-owned":      func(c *runtimecontract.ModelProviderConfiguration) { c.ManagedBy = "ui" },
		"moving source": func(c *runtimecontract.ModelProviderConfiguration) { c.SourceRevision = "main" },
		"other engine":  func(c *runtimecontract.ModelProviderConfiguration) { c.Engine = "codex" },
		"other version": func(c *runtimecontract.ModelProviderConfiguration) { c.EngineVersion = "latest" },
		"broad CIDR":    func(c *runtimecontract.ModelProviderConfiguration) { c.AddressCIDRs = []string{"192.0.2.0/24"} },
		"loopback":      func(c *runtimecontract.ModelProviderConfiguration) { c.AddressCIDRs = []string{"127.0.0.1/32"} },
		"metadata":      func(c *runtimecontract.ModelProviderConfiguration) { c.AddressCIDRs = []string{"169.254.169.254/32"} },
		"duplicate IP": func(c *runtimecontract.ModelProviderConfiguration) {
			c.AddressCIDRs = append(c.AddressCIDRs, c.AddressCIDRs[0])
		},
		"mapped IPv4": func(c *runtimecontract.ModelProviderConfiguration) {
			c.AddressCIDRs = []string{"::ffff:192.0.2.40/128"}
		},
		"duplicate model": func(c *runtimecontract.ModelProviderConfiguration) {
			c.Models = append(c.Models, c.Models[0])
		},
		"unbounded output": func(c *runtimecontract.ModelProviderConfiguration) { c.Models[0].MaximumOutputTokens = 0 },
		"excessive output": func(c *runtimecontract.ModelProviderConfiguration) { c.Models[0].MaximumOutputTokens = 32769 },
	} {
		t.Run(name, func(t *testing.T) {
			config := modelProviderFixture()
			mutate(&config)
			if !errors.Is(config.Validate(), runtimecontract.ErrModelProviderConfiguration) {
				t.Fatal("unbound provider input accepted")
			}
		})
	}
}

// TestModelProviderConfigurationAllowsOnlyChatAndCatalog prevents provider traffic from granting web access.
// TestModelProviderConfigurationAllowsOnlyChatAndCatalog не превращает provider traffic в web access.
func TestModelProviderConfigurationAllowsOnlyChatAndCatalog(t *testing.T) {
	config := modelProviderFixture()
	for _, request := range []struct {
		host, path, method string
		allowed            bool
	}{
		{"gateway.example.invalid", "/v1/chat/completions", "POST", true},
		{"gateway.example.invalid", "/v1/models", "GET", true},
		{"gateway.example.invalid", "/v1/chat/completions", "GET", false},
		{"gateway.example.invalid", "/v1/responses", "POST", false},
		{"gateway.example.invalid", "/v1/models/other", "GET", false},
		{"other.example.invalid", "/v1/chat/completions", "POST", false},
		{"api.openai.com", "/v1/chat/completions", "POST", false},
		{"gateway.example.invalid", "/v1/../chat/completions", "POST", false},
	} {
		if config.AllowsRequest(request.host, request.path, request.method) != request.allowed {
			t.Fatal("provider request allowance differs from closed route contract")
		}
	}
}

// TestModelProviderConfigurationDigestPinsMetadata detects changes in source, network or model ceilings.
// TestModelProviderConfigurationDigestPinsMetadata обнаруживает изменение source, сети и model ceilings.
func TestModelProviderConfigurationDigestPinsMetadata(t *testing.T) {
	config := modelProviderFixture()
	original, err := config.Digest()
	if err != nil {
		t.Fatal(err)
	}
	config.SourceRevision = strings.Repeat("b", 40)
	changed, err := config.Digest()
	if err != nil || changed == original {
		t.Fatal("source revision was not pinned")
	}
	if _, found := config.Model("unconfigured-model"); found {
		t.Fatal("unconfigured model selected")
	}
}

// TestRuntimeRevisionDigestPinsModelProvider prevents transport changes from reusing a revision digest.
// TestRuntimeRevisionDigestPinsModelProvider запрещает смену transport при прежнем revision digest.
func TestRuntimeRevisionDigestPinsModelProvider(t *testing.T) {
	config := modelProviderFixture()
	input := runtimecontract.RunnerInput{RuntimeRevisionRef: "rrv_synthetic", RuntimeRevisionVersion: 1,
		Provider: "opencode", Model: "model-a", ModelProvider: &config}
	source := runtimecontract.RuntimeRevisionCredentialSource{SecretName: "synthetic", SecretUID: "uid", SecretResourceVersion: "1"}
	original, err := runtimecontract.RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	config.BaseURL = "https://other.example.invalid/v1"
	changed, err := runtimecontract.RuntimeRevisionDigest(input, source)
	if err != nil || changed == original {
		t.Fatal("provider transport is not bound to the runtime revision")
	}
}

// TestRuntimeProxyGrantPinsGatewayOnly preserves legacy bytes and confines alternate provider access.
// TestRuntimeProxyGrantPinsGatewayOnly сохраняет legacy bytes и ограничивает alternate provider access.
func TestRuntimeProxyGrantPinsGatewayOnly(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	web := runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone}
	digest := strings.Repeat("a", 64)
	legacy, err := runtimecontract.SignRuntimeWebAccessGrant(key, "synthetic-workload", digest, web)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := runtimecontract.SignRuntimeProxyAccessGrant(key, "synthetic-workload", digest, web, nil)
	if err != nil || unchanged != legacy {
		t.Fatal("default proxy grant changed")
	}
	config := modelProviderFixture()
	token, err := runtimecontract.SignRuntimeProxyAccessGrant(key, "synthetic-workload", digest, web, &config)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := runtimecontract.VerifyRuntimeWebAccessGrant(key, token)
	if err != nil || claims.ModelProvider == nil {
		t.Fatal("signed model gateway route lost", err)
	}
	access := runtimecontract.RuntimeProxyAccess{ProviderAccess: true, ModelProvider: claims.ModelProvider, WebAccess: web}
	if !access.ProviderAllowsRequest("gateway.example.invalid", "/v1/chat/completions", "POST") ||
		access.ProviderAllowsRequest("api.openai.com", "/v1/responses", "POST") ||
		runtimecontract.RuntimeWebAccessAllowsHost(claims.WebAccess, "gateway.example.invalid") {
		t.Fatal("gateway grant expanded provider or user-web authority")
	}
	if !claims.ModelProvider.AllowsAddress(netip.MustParseAddr("192.0.2.40")) ||
		claims.ModelProvider.AllowsAddress(netip.MustParseAddr("192.0.2.41")) {
		t.Fatal("gateway address pin not enforced")
	}
}
