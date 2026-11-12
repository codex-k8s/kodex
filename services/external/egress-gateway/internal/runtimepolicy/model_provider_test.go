package runtimepolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// TestRuntimeGatewayGrantRequiresExactDeploymentConfiguration rejects a validly signed route with different pins or configuration.
// TestRuntimeGatewayGrantRequiresExactDeploymentConfiguration отклоняет validly signed route с другими pins или configuration.
func TestRuntimeGatewayGrantRequiresExactDeploymentConfiguration(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "deploy/k8s/base/egress-gateway/policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc policy.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("fixture policy invalid")
	}
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.4/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "test-model", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	doc.Spec.ModelProviderCatalog = &runtimecontract.ModelProviderCatalog{Schema: runtimecontract.ModelProviderCatalogSchema, ManagedBy: "git", SourceRevision: config.SourceRevision, DefaultProvider: config.DefinitionKey(), DefaultModel: config.Models[0].ID, Providers: []runtimecontract.ModelProviderConfiguration{config}}
	second, err := config.Clone()
	if err != nil {
		t.Fatal(err)
	}
	second.ProviderID = "second-gateway"
	second.BaseURL = "https://gateway.example.invalid/second/v1"
	second.API = runtimecontract.ModelAPIResponses
	doc.Spec.ModelProviderCatalog.Providers = append(doc.Spec.ModelProviderCatalog.Providers, second)
	doc.Spec.Destinations = append(doc.Spec.Destinations, policy.Destination{Hostname: "gateway.example.invalid", Port: 443})
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write fixture")
	}
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.Load(raw, doc.Metadata.Revision, digest)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("synthetic", 8))
	active, err := New(base, key)
	if err != nil {
		t.Fatal(err)
	}
	web := runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone, Rules: []runtimecontract.RuntimeWebAccessRule{}}
	grant, err := runtimecontract.SignRuntimeProxyAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), web, &config)
	if err != nil {
		t.Fatal(err)
	}
	access, ok := active.AuthorizeAuthenticated("gateway.example.invalid", 443, grant)
	if !ok || !access.ProviderAccess || access.ModelProvider == nil || !access.ProviderAllowsRequest("gateway.example.invalid", "/v1/chat/completions", "POST") {
		t.Fatal("approved gateway rejected")
	}
	for _, host := range []string{"api.openai.com", "other.example.invalid"} {
		if _, ok := active.AuthorizeAuthenticated(host, 443, grant); ok {
			t.Fatal("gateway grant fell back")
		}
	}
	secondGrant, err := runtimecontract.SignRuntimeProxyAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), web, &second)
	if err != nil {
		t.Fatal(err)
	}
	secondAccess, ok := active.AuthorizeAuthenticated("gateway.example.invalid", 443, secondGrant)
	if !ok || !secondAccess.ProviderAllowsRequest("gateway.example.invalid", "/second/v1/responses", "POST") || secondAccess.ProviderAllowsRequest("gateway.example.invalid", "/v1/chat/completions", "POST") || access.ProviderAllowsRequest("gateway.example.invalid", "/second/v1/responses", "POST") {
		t.Fatal("same-host routes lost exact API/base-path authority")
	}
	forgedRoute, _ := second.Route()
	forgedRoute.API = runtimecontract.ModelAPIChatCompletions
	if base.MatchesModelProviderRoute("gateway.example.invalid", &forgedRoute) {
		t.Fatal("API substitution accepted under unchanged digest")
	}
	changed := config
	changed.AddressCIDRs = []string{"192.0.2.5/32"}
	wrong, err := runtimecontract.SignRuntimeProxyAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), web, &changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active.AuthorizeAuthenticated("gateway.example.invalid", 443, wrong); ok {
		t.Fatal("different configuration accepted")
	}
}
