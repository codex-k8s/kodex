package callback

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIntegrationCatalogIsPagedAndBoundToExactGrant(t *testing.T) {
	input := runtimecontract.RunnerInput{}
	for index := range 20 {
		input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{
			ConnectionRef: fmt.Sprintf("int_%08d", index), ConnectionName: fmt.Sprintf("Connection %02d", index),
			DefinitionKey: "test", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64),
			CapabilityKey: "test.read", CapabilityName: "Read", CapabilityDescription: "Read test data",
			Operation: "READ", Risk: "READ", InputSchemaSHA256: strings.Repeat("b", 64),
			InputSchema: `{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"}}}`,
		})
	}
	listed, err := integrationCatalog(input, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	first := listed.(map[string]any)
	if len(first["grants"].([]map[string]any)) != maximumIntegrationCatalogPage || first["next_offset"] != maximumIntegrationCatalogPage {
		t.Fatalf("integration catalog first page is invalid: %#v", first)
	}
	if _, exposed := first["grants"].([]map[string]any)[0]["input_schema"]; exposed {
		t.Fatal("integration index exposed a full input schema")
	}
	page, err := integrationCatalog(input, map[string]any{"offset": float64(16)})
	if err != nil || len(page.(map[string]any)["grants"].([]map[string]any)) != 4 {
		t.Fatalf("integration catalog last page is invalid: %v", err)
	}
	selected, err := integrationCatalog(input, map[string]any{"connection_ref": "int_00000003", "capability_key": "test.read"})
	if err != nil || len(selected.(map[string]any)["grants"].([]map[string]any)) != 1 ||
		selected.(map[string]any)["grants"].([]map[string]any)[0]["input_schema"] == nil {
		t.Fatalf("exact integration schema is unavailable: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"connection_ref": "int_00000003"}, {"connection_ref": "int_00000003", "capability_key": "other"},
		{"connection_ref": 123, "capability_key": "test.read"}, {"offset": 257}, {"offset": 1.5},
		{"connection_ref": "int_00000003", "capability_key": "test.read", "offset": 1},
		{"connection_ref": "int_00000003", "capability_key": "test.read", "offset": 0},
		{"connection_ref": "int_00000003", "capability_key": "test.read", "query": ""},
		{"query": strings.Repeat("x", 81)}, {"other": "untrusted"},
	} {
		if _, err := integrationCatalog(input, invalid); err == nil || status.Code(err) != codes.InvalidArgument {
			t.Fatalf("integration catalog accepted invalid selection: %#v", invalid)
		}
	}
	if _, err := integrationCatalog(runtimecontract.RunnerInput{}, nil); err == nil {
		t.Fatal("integration catalog accepted a runtime without grants")
	}
}

func TestIntegrationCatalogSchemaSeparatesSearchAndExactSelection(t *testing.T) {
	schema := integrationCatalogTool()["inputSchema"].(map[string]any)
	branches, ok := schema["oneOf"].([]map[string]any)
	if !ok || len(branches) != 2 || schema["additionalProperties"] != false {
		t.Fatalf("integration catalog schema does not separate call modes: %#v", schema)
	}
}

func TestIntegrationToolListDoesNotEmbedGrantSchemas(t *testing.T) {
	input := runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{{
		ConnectionRef: "int_12345678", CapabilityKey: "test.read", InputSchema: `{"type":"object","additionalProperties":false,"properties":{"secret-marker":{"type":"string"}}}`,
	}}}
	encoded, err := json.Marshal(tools(input))
	if err != nil || strings.Contains(string(encoded), "secret-marker") || len(encoded) > 6000 {
		t.Fatalf("integration tools/list leaked a grant schema: bytes=%d err=%v", len(encoded), err)
	}
	parameters, capability, grant, ok := safeToolCallParameters(input, "get_integration_catalog", map[string]any{"query": "secret-marker"})
	if !ok || len(parameters) != 0 || capability != "platform.integration.catalog" || grant != "" {
		t.Fatal("integration discovery leaked input or gained invocation authority")
	}
}
