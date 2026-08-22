package callback

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
)

func TestAssistantPlanToolIsSystemOnlyAndBounded(t *testing.T) {
	t.Parallel()
	if encoded, _ := json.Marshal(tools(runtimecontract.RunnerInput{})); strings.Contains(string(encoded), "propose_configuration_plan") {
		t.Fatal("ordinary runtime must not receive the system assistant tool")
	}
	available := tools(runtimecontract.RunnerInput{SystemAssistant: true})
	if len(available) != 1 || available[0]["name"] != "propose_configuration_plan" {
		t.Fatalf("unexpected assistant tool catalog: %#v", available)
	}
	schema := available[0]["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatal("assistant tool schema must reject unknown top-level fields")
	}
	operations := schema["properties"].(map[string]any)["operations"].(map[string]any)
	if operations["maxItems"] != 32 {
		t.Fatalf("assistant plan must be bounded, got %#v", operations["maxItems"])
	}
	oneOf := operations["items"].(map[string]any)["oneOf"].([]map[string]any)
	if len(oneOf) != 7 {
		t.Fatalf("unexpected specialized operation count: %d", len(oneOf))
	}
}
