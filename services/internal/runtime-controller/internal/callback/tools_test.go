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
	if len(oneOf) != 9 {
		t.Fatalf("unexpected specialized operation count: %d", len(oneOf))
	}
	byType := make(map[string]map[string]any, len(oneOf))
	for _, operation := range oneOf {
		properties := operation["properties"].(map[string]any)
		operationType := properties["type"].(map[string]any)["const"].(string)
		byType[operationType] = properties["input"].(map[string]any)
	}
	for _, operationType := range []string{"CREATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION"} {
		if byType[operationType] == nil {
			t.Fatalf("assistant tool lost specialized operation %q", operationType)
		}
	}
	grant := byType["CHANGE_INTEGRATION_GRANT"]
	if len(grant["oneOf"].([]map[string]any)) != 2 || !containsString(grant["required"].([]string), "expectedVersion") {
		t.Fatalf("integration grant schema lost target exclusivity or OCC: %#v", grant)
	}
	scheduleProperties := byType["CREATE_SCHEDULE"]["properties"].(map[string]any)
	if scheduleProperties["timeOfDay"] == nil || scheduleProperties["cronExpression"] != nil {
		t.Fatalf("assistant schedule schema diverged from owner schedule contract: %#v", scheduleProperties)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestDelegationToolPinsWorkflowTargetsAndStepKeys(t *testing.T) {
	t.Parallel()
	targets := []runtimecontract.RunnerDelegationTarget{
		{Ref: "agt_12345678", Name: "Researcher", WorkflowStepKey: "research"},
		{Ref: "agt_87654321", Name: "Writer", WorkflowStepKey: "draft"},
	}
	tool := delegationTool(targets)
	schema := tool["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatal("delegation tool schema must reject unknown fields")
	}
	required := schema["required"].([]string)
	if len(required) != 3 || required[2] != "workflow_step_key" {
		t.Fatalf("workflow step key is not mandatory: %#v", required)
	}
	properties := schema["properties"].(map[string]any)
	targetEnum := properties["target_agent_ref"].(map[string]any)["enum"].([]string)
	stepEnum := properties["workflow_step_key"].(map[string]any)["enum"].([]string)
	if len(targetEnum) != 2 || targetEnum[0] != targets[0].Ref || len(stepEnum) != 2 || stepEnum[1] != targets[1].WorkflowStepKey {
		t.Fatalf("delegation schema lost server-owned enum: targets=%#v steps=%#v", targetEnum, stepEnum)
	}
}
