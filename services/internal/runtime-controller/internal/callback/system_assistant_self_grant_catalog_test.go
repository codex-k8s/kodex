package callback

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestSystemAssistantSelfGrantCatalogAcrossScreenContexts(t *testing.T) {
	const operation = "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"
	for _, screen := range []string{"PROJECT", "AGENT", "ENVIRONMENT", "INTEGRATION_CONNECTION"} {
		t.Run(screen, func(t *testing.T) {
			input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
			input.AssistantContext.EntityKind, input.AssistantContext.EntityRef = screen, "res_foreign123"
			input.AssistantContext.AllowedOperations = []string{"UPDATE_PROJECT"}
			catalog, err := configurationCatalog(input, map[string]any{"operation_types": []any{operation}})
			if err != nil {
				t.Fatalf("self grant schema is unreachable from %s: %v", screen, err)
			}
			schemas := catalog.(map[string]any)["operation_schemas"].([]map[string]any)
			if len(schemas) != 1 || assistantSchemaType(schemas[0]) != operation {
				t.Fatal("self grant catalog lost the exact schema")
			}
			client := &scopedAssistantPlanClient{}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			parameters := map[string]any{"connectionRef": "icn_fixture123", "capabilityKey": "context7.docs.query", "enabled": true, "approvalPolicy": "NONE"}
			arguments := map[string]any{"summary": "Prepare own managed MCP grant", "operations": []any{map[string]any{"type": operation, "parameters": parameters}}}
			if _, err := server.proposeAssistantPlan(t.Context(), input, arguments, json.RawMessage(`17`)); err != nil || len(client.requests) != 1 {
				t.Fatalf("self grant cannot reach the owner draft RPC: %v", err)
			}
			input.AssistantScope = runtimecontract.AssistantScopeProject
			for _, schema := range assistantPlanOperationSchemas(input) {
				if assistantSchemaType(schema) == operation {
					t.Fatal("PROJECT source received a SYSTEM self grant schema")
				}
			}
			if _, err := configurationCatalog(input, map[string]any{"operation_types": []any{operation}}); err == nil {
				t.Fatal("PROJECT source discovered a SYSTEM self grant schema")
			}
		})
	}
}
