package callback

import (
	"encoding/json"
	"errors"
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
	if len(oneOf) != 12 {
		t.Fatalf("unexpected specialized operation count: %d", len(oneOf))
	}
	byType := make(map[string]map[string]any, len(oneOf))
	operationByType := make(map[string]map[string]any, len(oneOf))
	schemaByType := make(map[string]map[string]any, len(oneOf))
	for _, operation := range oneOf {
		properties := operation["properties"].(map[string]any)
		operationType := properties["type"].(map[string]any)["const"].(string)
		byType[operationType] = properties["parameters"].(map[string]any)
		operationByType[operationType] = properties
		schemaByType[operationType] = operation
	}
	createProject := operationByType["CREATE_PROJECT"]
	createBefore := createProject["before"].(map[string]any)
	createAfter := createProject["after"].(map[string]any)
	if createBefore["additionalProperties"] != false || createAfter["additionalProperties"] != false ||
		!reflect.DeepEqual(createAfter["required"], byType["CREATE_PROJECT"]["required"]) {
		t.Fatalf("create operation schema is not canonical: before=%#v after=%#v parameters=%#v", createBefore, createAfter, byType["CREATE_PROJECT"])
	}
	createRequired := schemaByType["CREATE_PROJECT"]["required"].([]string)
	if !reflect.DeepEqual(createRequired, []string{"type", "title", "summary", "parameters"}) {
		t.Fatalf("create operation must leave server-owned envelope optional: %#v", createRequired)
	}
	updateProject := operationByType["UPDATE_PROJECT"]
	if updateProject == nil || !reflect.DeepEqual(schemaByType["UPDATE_PROJECT"]["required"].([]string), []string{"type", "title", "summary", "parameters"}) {
		t.Fatalf("project update must leave authority fields to the server: %#v", updateProject)
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

func TestAssistantPlanInputErrorsKeepAClosedFailureClass(t *testing.T) {
	t.Parallel()
	server := &Server{}
	_, err := server.proposeAssistantPlan(t.Context(), runtimecontract.RunnerInput{SystemAssistant: true}, map[string]any{
		"summary": "Create one agent",
		"operations": []any{map[string]any{
			"action":     "DELETE_PROJECT",
			"parameters": map[string]any{"name": "Analyst"},
		}}}, json.RawMessage(`1`))
	var inputErr *assistantPlanInputError
	if !errors.As(err, &inputErr) {
		t.Fatalf("expected a typed assistant plan input error, got %v", err)
	}
	if inputErr.reason != "operation_type" {
		t.Fatalf("unexpected safe failure class: %q", inputErr.reason)
	}
}

func TestNormalizeServerHydratedAssistantOperationAcceptsBoundedModelShorthand(t *testing.T) {
	t.Parallel()
	parameters := map[string]any{
		"project_ref":      "prj_12345678",
		"name":             "Analyst",
		"role_description": "Sales analyst",
	}
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"action":     "CREATE_AGENT",
		"parameters": parameters,
	}, "Create one analyst", "prj_12345678", "Sales")
	if err != nil {
		t.Fatalf("normalize model shorthand: %v", err)
	}
	if operation["type"] != "CREATE_AGENT" || operation["title"] != "Создать ИИ-сотрудника «Analyst»" ||
		operation["summary"] != "Create one analyst" {
		t.Fatalf("unexpected normalized envelope: %#v", operation)
	}
	normalized := operation["parameters"].(map[string]any)
	if normalized["projectRef"] != "prj_12345678" || normalized["roleDescription"] != "Sales analyst" {
		t.Fatalf("parameter aliases were not normalized: %#v", normalized)
	}
	if _, exists := normalized["project_ref"]; exists {
		t.Fatalf("snake_case alias survived normalization: %#v", normalized)
	}
}

func TestNormalizeServerHydratedAssistantOperationRejectsAliasCollision(t *testing.T) {
	t.Parallel()
	_, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"type": "CREATE_AGENT",
		"parameters": map[string]any{
			"projectRef":  "prj_12345678",
			"project_ref": "prj_87654321",
		},
	}, "Create one analyst", "prj_12345678", "Sales")
	var inputErr *assistantPlanInputError
	if !errors.As(err, &inputErr) || inputErr.reason != "operation_parameter_alias" {
		t.Fatalf("expected a closed alias collision, got %v", err)
	}
}

func TestNormalizeServerHydratedAssistantOperationPinsCurrentProject(t *testing.T) {
	t.Parallel()
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"action":     "UPDATE_PROJECT",
		"parameters": map[string]any{"purpose": "Updated purpose"},
	}, "Update current project", "prj_current1", "Sales")
	if err != nil {
		t.Fatalf("normalize project update: %v", err)
	}
	parameters := operation["parameters"].(map[string]any)
	if parameters["projectRef"] != "prj_current1" {
		t.Fatalf("current project was not server-pinned: %#v", parameters)
	}
	if operation["title"] != "Изменить Проект «Sales»" || operation["summary"] != "Изменить Проект «Sales» — назначение: «Updated purpose»." {
		t.Fatalf("project update is not explicit: %#v", operation)
	}
}
