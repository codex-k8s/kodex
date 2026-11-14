package platform

import (
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestSentryFieldsMatchToolSchema checks the contract from shipped typed tool inputs through owner projections used by generic UI, including Russian labels/help and actual integer/boolean types.
// TestSentryFieldsMatchToolSchema проверяет контракт от shipped typed inputs до owner projections generic UI, включая русские labels/help и реальные integer/boolean types.
func TestSentryFieldsMatchToolSchema(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sentry", "sentry-self-hosted", "sentry-organization", "sentry-self-hosted-organization"} {
		definition := definitions[key]
		for _, capability := range definition.Spec.Capabilities {
			raw, err := capability.InputSchema()
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			}
			if json.Unmarshal(raw, &schema) != nil {
				t.Fatal("tool schema unavailable")
			}
			for _, field := range integrationConfigurationFields(capability.InputFields) {
				if field.Label == "" || field.Label == field.Key || field.Help == "" {
					t.Fatal("generic UI received technical-only field metadata")
				}
				want := map[string]string{"string": "TEXT", "integer": "INTEGER", "boolean": "BOOLEAN"}[schema.Properties[field.Key].Type]
				if field.ValueType != want {
					t.Fatalf("UI and tool input types differ for %s: %s/%s", field.Key, field.ValueType, want)
				}
			}
		}
	}
}
