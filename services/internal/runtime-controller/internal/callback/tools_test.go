package callback

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestAssistantPlanToolIsSystemOnlyAndBounded(t *testing.T) {
	t.Parallel()
	if encoded, _ := json.Marshal(tools(runtimecontract.RunnerInput{})); strings.Contains(string(encoded), "propose_configuration_plan") {
		t.Fatal("ordinary runtime must not receive the system assistant tool")
	}
	input := runtimecontract.RunnerInput{SystemAssistant: true, ProjectRef: "prj_12345678", DelegationTargets: []runtimecontract.RunnerDelegationTarget{{Ref: "agt_12345678", Name: "Analyst"}}}
	available := tools(input)
	if len(available) != 5 {
		t.Fatalf("unexpected assistant tool catalog: %#v", available)
	}
	var planTool map[string]any
	for _, tool := range available {
		if tool["name"] == "propose_configuration_plan" {
			planTool = tool
		}
	}
	if planTool == nil {
		t.Fatal("assistant plan tool is absent")
	}
	schema := planTool["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatal("assistant tool schema must reject unknown top-level fields")
	}
	operations := schema["properties"].(map[string]any)["operations"].(map[string]any)
	if operations["maxItems"] != 32 {
		t.Fatalf("assistant plan must be bounded, got %#v", operations["maxItems"])
	}
	oneOf := operations["items"].(map[string]any)["oneOf"].([]map[string]any)
	if len(oneOf) != 11 {
		t.Fatalf("unexpected specialized operation count: %d", len(oneOf))
	}
	byType := make(map[string]map[string]any, len(oneOf))
	operationByType := make(map[string]map[string]any, len(oneOf))
	for _, operation := range oneOf {
		properties := operation["properties"].(map[string]any)
		operationType := properties["type"].(map[string]any)["const"].(string)
		byType[operationType] = properties["parameters"].(map[string]any)
		operationByType[operationType] = properties
	}
	createProject := operationByType["CREATE_PROJECT"]
	createBefore := createProject["before"].(map[string]any)
	createAfter := createProject["after"].(map[string]any)
	if createBefore["additionalProperties"] != false || createAfter["additionalProperties"] != false ||
		!reflect.DeepEqual(createAfter["required"], byType["CREATE_PROJECT"]["required"]) {
		t.Fatalf("create operation schema is not canonical: before=%#v after=%#v parameters=%#v", createBefore, createAfter, byType["CREATE_PROJECT"])
	}
	workflowProperties := byType["CREATE_WORKFLOW"]["properties"].(map[string]any)
	if workflowProperties["projectRef"].(map[string]any)["enum"].([]string)[0] != input.ProjectRef ||
		workflowProperties["coordinatorAgentRef"].(map[string]any)["enum"].([]string)[0] != input.DelegationTargets[0].Ref {
		t.Fatalf("workflow schema is not bound to the server catalog: %#v", workflowProperties)
	}
	stepProperties := workflowProperties["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if len(stepProperties["parallelGroup"].(map[string]any)["oneOf"].([]map[string]any)) != 2 {
		t.Fatalf("workflow schema must admit numeric and named parallel groups: %#v", stepProperties["parallelGroup"])
	}
	for _, operationType := range []string{"CREATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION"} {
		if byType[operationType] == nil {
			t.Fatalf("assistant tool lost specialized operation %q", operationType)
		}
	}
	grant := byType["CHANGE_INTEGRATION_GRANT"]
	if len(grant["oneOf"].([]map[string]any)) != 2 {
		t.Fatalf("integration grant schema lost target exclusivity: %#v", grant)
	}
	scheduleProperties := byType["CREATE_SCHEDULE"]["properties"].(map[string]any)
	if scheduleProperties["timeOfDay"] == nil || scheduleProperties["cronExpression"] != nil {
		t.Fatalf("assistant schedule schema diverged from owner schedule contract: %#v", scheduleProperties)
	}
}

func TestConfigurationCatalogReturnsOnlyServerOwnedBindings(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{SystemAssistant: true, ProjectRef: "prj_12345678", DelegationTargets: []runtimecontract.RunnerDelegationTarget{
		{Ref: "agt_writer01", Name: "Writer", Purpose: "Write"},
		{Ref: "agt_analyst1", Name: "Analyst", Purpose: "Analyze"},
	}}
	result, err := configurationCatalog(input, map[string]any{})
	if err != nil {
		t.Fatalf("configuration catalog: %v", err)
	}
	catalog := result.(map[string]any)
	agents := catalog["agents"].([]map[string]string)
	if catalog["current_project_ref"] != input.ProjectRef || len(agents) != 2 || agents[0]["ref"] != "agt_analyst1" {
		t.Fatalf("unexpected configuration catalog: %#v", catalog)
	}
	if _, err := configurationCatalog(input, map[string]any{"projectRef": "untrusted"}); err == nil {
		t.Fatal("configuration catalog accepted caller input")
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

func TestDecodeMCPToolCallParamsAcceptsStandardMetadata(t *testing.T) {
	t.Parallel()
	params, err := decodeMCPToolCallParams(json.RawMessage(`{
		"name":"propose_configuration_plan",
		"arguments":{"summary":"Create one project","operations":[]},
		"_meta":{"progressToken":"opaque"}
	}`))
	if err != nil {
		t.Fatalf("decode tool call with MCP metadata: %v", err)
	}
	if params.Name != "propose_configuration_plan" || params.Arguments["summary"] != "Create one project" {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestDecodeMCPToolCallParamsRejectsUnknownAuthorityFields(t *testing.T) {
	t.Parallel()
	_, err := decodeMCPToolCallParams(json.RawMessage(`{
		"name":"propose_configuration_plan",
		"arguments":{},
		"actor":"owner"
	}`))
	if err == nil {
		t.Fatal("unknown authority-like field was accepted")
	}
}
