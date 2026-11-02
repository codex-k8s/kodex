package app

import "testing"

// TestModelProviderDefaultAcceptsNamespacedAliases keeps gateway model aliases independent of legacy runtime identifiers.
// TestModelProviderDefaultAcceptsNamespacedAliases отделяет gateway model aliases от legacy runtime identifiers.
func TestModelProviderDefaultAcceptsNamespacedAliases(t *testing.T) {
	for _, test := range []struct {
		name, path, model string
		valid             bool
	}{
		{"disabled", "", "", true}, {"selection only", "/operator/provider.json", "", true},
		{"namespaced model", "/operator/provider.json", "vendor/model-v1", true},
		{"missing configuration", "", "vendor/model-v1", false}, {"relative file", "provider.json", "vendor/model-v1", false},
		{"invalid alias", "/operator/provider.json", "vendor/model v1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if validModelProviderInput(Config{ModelProviderConfigurationFile: test.path, ModelProviderDefaultModel: test.model}) != test.valid {
				t.Fatal("model provider input validation changed")
			}
		})
	}
}
