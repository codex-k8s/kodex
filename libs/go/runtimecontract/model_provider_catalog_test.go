package runtimecontract_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// providerCatalogFixture keeps identical model aliases under separate synthetic endpoints and definition identities.
// providerCatalogFixture сохраняет одинаковые aliases моделей у отдельных synthetic endpoints и definition identities.
func providerCatalogFixture(t *testing.T) runtimecontract.ModelProviderCatalog {
	t.Helper()
	first := cacheProviderFixture()
	second, err := first.Clone()
	if err != nil {
		t.Fatal(err)
	}
	second.ProviderID = "second-gateway"
	second.BaseURL = "https://second.example.invalid/v1"
	second.AddressCIDRs = []string{"192.0.2.3/32"}
	second.Models[0].CachePolicy = nil
	return runtimecontract.ModelProviderCatalog{Schema: runtimecontract.ModelProviderCatalogSchema, ManagedBy: "git", SourceRevision: first.SourceRevision, DefaultProvider: first.DefinitionKey(), DefaultModel: "tiny", Providers: []runtimecontract.ModelProviderConfiguration{first, second}}
}

// TestModelProviderCatalogSelectsExactDefinition proves a shared alias cannot redirect credential selection and returned configuration does not alias the startup catalog.
// TestModelProviderCatalogSelectsExactDefinition проверяет отсутствие выбора credentials по общему alias и независимость возвращённой конфигурации.
func TestModelProviderCatalogSelectsExactDefinition(t *testing.T) {
	catalog := providerCatalogFixture(t)
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := runtimecontract.DecodeModelProviderCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	first, found := parsed.Provider("opencode-test-gateway")
	if !found {
		t.Fatal("first definition missing")
	}
	second, found := parsed.Provider("opencode-second-gateway")
	if !found || first.BaseURL == second.BaseURL || first.Models[0].ID != second.Models[0].ID {
		t.Fatal("independent route selection collapsed")
	}
	first.AddressCIDRs[0] = "192.0.2.99/32"
	first.Models[0].CachePolicy.Required = true
	again, _ := parsed.Provider("opencode-test-gateway")
	if again.AddressCIDRs[0] == first.AddressCIDRs[0] || again.Models[0].CachePolicy.Required {
		t.Fatal("selection changed shared catalog")
	}
	if _, found := parsed.Provider("tiny"); found {
		t.Fatal("model alias selected a provider")
	}
	if _, found := parsed.Provider("unknown"); found {
		t.Fatal("unknown provider fell back to default")
	}
}

// TestModelProviderCatalogRejectsAmbiguousOrUnpinnedInput covers exact default/source, unique identities, shared-host address authority and bounded closed decoding.
// TestModelProviderCatalogRejectsAmbiguousOrUnpinnedInput проверяет exact default/source, уникальность identities, адреса общего host и закрытый ограниченный разбор.
func TestModelProviderCatalogRejectsAmbiguousOrUnpinnedInput(t *testing.T) {
	for name, mutate := range map[string]func(*runtimecontract.ModelProviderCatalog){
		"missing default":            func(c *runtimecontract.ModelProviderCatalog) { c.DefaultProvider = "" },
		"wrong model":                func(c *runtimecontract.ModelProviderCatalog) { c.DefaultModel = "foreign" },
		"source mismatch":            func(c *runtimecontract.ModelProviderCatalog) { c.Providers[1].SourceRevision = strings.Repeat("d", 40) },
		"duplicate definition":       func(c *runtimecontract.ModelProviderCatalog) { c.Providers[1].ProviderID = c.Providers[0].ProviderID },
		"shared host different pins": func(c *runtimecontract.ModelProviderCatalog) { c.Providers[1].BaseURL = c.Providers[0].BaseURL },
		"empty catalog":              func(c *runtimecontract.ModelProviderCatalog) { c.Providers = nil },
		"unknown ownership":          func(c *runtimecontract.ModelProviderCatalog) { c.ManagedBy = "ui" },
	} {
		t.Run(name, func(t *testing.T) {
			catalog := providerCatalogFixture(t)
			mutate(&catalog)
			raw, _ := json.Marshal(catalog)
			if _, err := runtimecontract.DecodeModelProviderCatalog(raw); err == nil {
				t.Fatal("untrusted catalog accepted")
			}
		})
	}
	catalog := providerCatalogFixture(t)
	raw, _ := json.Marshal(catalog)
	single, _ := json.Marshal(catalog.Providers[0])
	for _, invalid := range [][]byte{single, append(append([]byte{}, raw...), raw...), []byte(strings.Replace(string(raw), `"managed_by":"git"`, `"managed_by":"git","managed_by":"git"`, 1)), []byte(strings.Replace(string(raw), `"providers":`, `"arbitrary":true,"providers":`, 1))} {
		if _, err := runtimecontract.DecodeModelProviderCatalog(invalid); err == nil {
			t.Fatal("old, duplicate, unknown or trailing document accepted")
		}
	}
	catalog.Providers[1].BaseURL = catalog.Providers[0].BaseURL + "/alternate"
	catalog.Providers[1].AddressCIDRs = []string{"192.0.2.2/32"}
	if catalog.Validate() != nil {
		t.Fatal("shared TLS host with identical pins should retain distinct routes")
	}
}
