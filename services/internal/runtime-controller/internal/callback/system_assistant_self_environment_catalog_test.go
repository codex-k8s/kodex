package callback

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/google/jsonschema-go/jsonschema"
)

func selfEnvironmentCatalogSchema(t *testing.T, input runtimecontract.RunnerInput) *jsonschema.Resolved {
	t.Helper()
	var parameters map[string]any
	for _, operation := range assistantPlanOperationSchemas(input) {
		if assistantSchemaType(operation) == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			if parameters != nil {
				t.Fatal("duplicate environment operation kind")
			}
			parameters = operation["properties"].(map[string]any)["parameters"].(map[string]any)
		}
	}
	if parameters == nil {
		t.Fatal("own environment operation is unreachable")
	}
	raw, err := json.Marshal(parameters)
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestSystemAssistantSelfEnvironmentCatalogPreservesClosedScreenUnion(t *testing.T) {
	for _, screenRef := range []string{"renv_foreign123", "renv_own12345"} {
		for _, screenAllowed := range []bool{false, true} {
			t.Run(screenRef+map[bool]string{false: "/self-only", true: "/screen-allowed"}[screenAllowed], func(t *testing.T) {
				input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
				input.RuntimeEnvironmentRef = "renv_own12345"
				input.AssistantContext.EntityKind, input.AssistantContext.EntityRef = "ENVIRONMENT", screenRef
				if screenAllowed {
					input.AssistantContext.AllowedOperations = []string{"PREPARE_RUNTIME_ENVIRONMENT_REVISION"}
				}
				resolved := selfEnvironmentCatalogSchema(t, input)
				own := map[string]any{"systemAssistantRef": input.AgentRef, "environmentRef": input.RuntimeEnvironmentRef, "description": "Own organization environment"}
				if err := resolved.Validate(own); err != nil {
					t.Fatalf("own organization environment disappeared on an environment screen: %v", err)
				}
				screen := map[string]any{"environmentRef": screenRef, "description": "Exact screen environment"}
				if err := resolved.Validate(screen); (err == nil) != screenAllowed {
					t.Fatal("screen branch differs from the existing allowed operation")
				}
				for _, bad := range []map[string]any{
					{"systemAssistantRef": "agt_foreign123", "environmentRef": input.RuntimeEnvironmentRef, "description": "Invalid self"},
					{"systemAssistantRef": input.AgentRef, "environmentRef": "renv_other123", "description": "Invalid binding"},
					{"systemAssistantRef": nil, "environmentRef": screenRef, "description": "Invalid discriminator"},
					{"systemAssistantRef": input.AgentRef, "projectAssistantRef": "agt_project123", "environmentRef": input.RuntimeEnvironmentRef, "description": "Mixed locators"},
					{"systemAssistantRef": input.AgentRef, "environmentRef": input.RuntimeEnvironmentRef, "organizationRef": "org_untrusted123", "description": "Caller authority"},
					{"systemAssistantRef": input.AgentRef, "environmentRef": input.RuntimeEnvironmentRef},
				} {
					if resolved.Validate(bad) == nil {
						t.Fatal("closed environment union admitted invalid locators or authority")
					}
				}
				if screenRef != input.RuntimeEnvironmentRef {
					if resolved.Validate(map[string]any{"systemAssistantRef": input.AgentRef, "environmentRef": screenRef, "description": "Mixed screen and self"}) == nil {
						t.Fatal("screen environment was accepted as the own organization binding")
					}
				}
				client := &scopedAssistantPlanClient{}
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
				arguments := map[string]any{"summary": "Prepare own environment", "operations": []any{map[string]any{"type": "PREPARE_RUNTIME_ENVIRONMENT_REVISION", "parameters": own}}}
				if _, err := server.proposeAssistantPlan(t.Context(), input, arguments, json.RawMessage(`18`)); err != nil || len(client.requests) != 1 {
					t.Fatalf("own environment cannot reach the owner draft RPC: %v", err)
				}
				actual := client.requests[0].GetOperations()[0].GetParameters().AsMap()
				if actual["systemAssistantRef"] != input.AgentRef || actual["environmentRef"] != input.RuntimeEnvironmentRef {
					t.Fatal("screen context overwrote the self environment locator")
				}
				input.AssistantScope = runtimecontract.AssistantScopeProject
				if selfEnvironmentCatalogSchema(t, input).Validate(own) == nil {
					t.Fatal("PROJECT gained a SYSTEM environment branch")
				}
			})
		}
	}
}
