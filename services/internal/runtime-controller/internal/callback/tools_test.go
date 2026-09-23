package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rejectedIntegrationClient struct {
	controlplanev1.RuntimeWorkServiceClient
}

type unavailableIntegrationClient struct {
	controlplanev1.RuntimeWorkServiceClient
}

func (*rejectedIntegrationClient) ResolveIntegrationInvocation(_ context.Context, _ *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: "inv_rejected1", State: "WAITING_APPROVAL"}, nil
}

func (*rejectedIntegrationClient) GetIntegrationInvocation(_ context.Context, _ *controlplanev1.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	return &controlplanev1.GetIntegrationInvocationResponse{State: "REJECTED", SafeErrorCode: "INTEGRATION_REJECTED_BY_OWNER"}, nil
}

func (*unavailableIntegrationClient) ResolveIntegrationInvocation(_ context.Context, _ *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	return nil, status.Error(codes.Unavailable, "transient control-plane failure")
}

func integrationGrantFixture() runtimecontract.RunnerIntegrationGrant {
	inputSchema := `{"additionalProperties":false,"properties":{"value":{"maxLength":4096,"minLength":1,"type":"string"}},"required":["value"],"type":"object"}`
	digest := sha256.Sum256([]byte(inputSchema))
	return runtimecontract.RunnerIntegrationGrant{
		Ref: "igr_12345678", ConnectionRef: "icon_12345678", DefinitionKey: "synthetic",
		ConnectionName: "Synthetic", CapabilityKey: "synthetic.journal.write", CapabilityName: "Write journal",
		CapabilityDescription: "Write one bounded journal value.", Risk: "WRITE", DefinitionVersion: "3.1.0",
		DefinitionDigest: strings.Repeat("a", 64), Operation: "synthetic.journal.write", InputSchema: inputSchema,
		InputSchemaSHA256: hex.EncodeToString(digest[:]),
	}
}

func integrationArguments(grant runtimecontract.RunnerIntegrationGrant, value string) map[string]any {
	return map[string]any{
		"connection_ref": grant.ConnectionRef, "capability_key": grant.CapabilityKey,
		"definition_version": grant.DefinitionVersion, "definition_digest": grant.DefinitionDigest,
		"input_schema_sha256": grant.InputSchemaSHA256, "input": map[string]any{"value": value},
	}
}

func TestInvokeReturnsRejectedIntegrationAsTerminalResult(t *testing.T) {
	t.Parallel()
	server := &Server{
		config:  Config{RequestTimeout: time.Second},
		control: &controlplaneclient.Client{Runtime: &rejectedIntegrationClient{}},
	}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{
		RunRef: "run_12345678", NodeRef: "nod_12345678", LeaseRef: "lse_12345678",
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant},
	}
	result, err := server.invoke(t.Context(), input, integrationArguments(grant, "rejected"), json.RawMessage(`"call-1"`))
	if err != nil {
		t.Fatalf("invoke rejected integration: %v", err)
	}
	values, ok := result.(integrationToolResult)
	if !ok || values.OK || values.ErrorCode != "INTEGRATION_REJECTED_BY_OWNER" || values.InvocationRef != "inv_rejected1" {
		t.Fatalf("unexpected rejected integration result: %#v", result)
	}
}

func TestInvokePreservesIntegrationResolutionStatus(t *testing.T) {
	t.Parallel()
	server := &Server{
		config:  Config{RequestTimeout: time.Second},
		control: &controlplaneclient.Client{Runtime: &unavailableIntegrationClient{}},
	}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{
		RunRef: "run_12345678", NodeRef: "nod_12345678", LeaseRef: "lse_12345678",
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant},
	}
	_, err := server.invoke(t.Context(), input, integrationArguments(grant, "unavailable"), json.RawMessage(`"call-1"`))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("integration resolution status was lost: %v", err)
	}
}

func TestAssistantPlanToolIsSystemOnlyAndBounded(t *testing.T) {
	t.Parallel()
	if encoded, _ := json.Marshal(tools(runtimecontract.RunnerInput{})); strings.Contains(string(encoded), "propose_configuration_plan") {
		t.Fatal("ordinary runtime must not receive the system assistant tool")
	}
	input := runtimecontract.RunnerInput{SystemAssistant: true, ProjectRef: "prj_12345678", DelegationTargets: []runtimecontract.RunnerDelegationTarget{{Ref: "agt_12345678", Name: "Analyst"}}}
	available := tools(input)
	if len(available) != 6 {
		t.Fatalf("unexpected assistant tool catalog: %#v", available)
	}
	if encoded, err := json.Marshal(available); err != nil || len(encoded) > 8000 {
		t.Fatalf("assistant tools/list is not compact: bytes=%d err=%v", len(encoded), err)
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
	items := operations["items"].(map[string]any)
	if _, expanded := items["oneOf"]; expanded || items["additionalProperties"] != false {
		t.Fatal("assistant tools/list must keep the plan envelope compact and closed")
	}
	if !reflect.DeepEqual(items["properties"].(map[string]any)["type"].(map[string]any)["enum"], assistantOperationTypes(input)) {
		t.Fatal("assistant plan envelope lost the allowed operation types")
	}
	oneOf := assistantPlanOperationSchemas(input)
	if len(oneOf) != 14 {
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
	environmentProperties := byType["CREATE_RUNTIME_ENVIRONMENT_DRAFT"]["properties"].(map[string]any)
	if environmentProperties["projectRef"].(map[string]any)["enum"].([]string)[0] != input.ProjectRef ||
		environmentProperties["imageArtifactRef"] == nil {
		t.Fatalf("environment draft schema lost project binding or artifact pointer: %#v", environmentProperties)
	}
	imageProperties := byType["CREATE_ROLE_IMAGE_RECIPE"]["properties"].(map[string]any)
	if imageProperties["projectRef"].(map[string]any)["enum"].([]string)[0] != input.ProjectRef ||
		imageProperties["agentRef"] == nil || imageProperties["name"] == nil ||
		imageProperties["agentVersion"] != nil || imageProperties["secretValue"] != nil {
		t.Fatalf("role image schema exposed owner fields or lost project binding: %#v", imageProperties)
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
	if scheduleProperties["timeOfDay"] == nil || scheduleProperties["cronExpression"] == nil ||
		scheduleProperties["automationText"] == nil ||
		scheduleProperties["preset"].(map[string]any)["enum"].([]string)[4] != "CUSTOM" {
		t.Fatalf("assistant schedule schema diverged from owner schedule contract: %#v", scheduleProperties)
	}
	if !containsString(byType["CREATE_SCHEDULE"]["required"].([]string), "automationText") {
		t.Fatalf("assistant schedule must require explicit task: %#v", byType["CREATE_SCHEDULE"])
	}
	for _, operationType := range []string{"CREATE_SCHEDULE", "LAUNCH_RUN"} {
		parameters := byType[operationType]
		branches := parameters["oneOf"].([]map[string]any)
		if len(branches) != 2 || parameters["properties"].(map[string]any)["targetType"].(map[string]any)["enum"].([]string)[1] != "WORKFLOW" {
			t.Fatalf("assistant %s schema lost workflow launch target: %#v", operationType, parameters)
		}
		agent := branches[0]["properties"].(map[string]any)
		workflow := branches[1]["properties"].(map[string]any)
		if agent["targetType"].(map[string]any)["const"] != "AGENT" ||
			agent["targetRef"].(map[string]any)["enum"].([]string)[0] != input.DelegationTargets[0].Ref ||
			workflow["targetType"].(map[string]any)["const"] != "WORKFLOW" ||
			workflow["targetRef"].(map[string]any)["pattern"] == nil {
			t.Fatalf("assistant %s schema lost distinct target boundaries: %#v", operationType, branches)
		}
	}
	runProperties := byType["LAUNCH_RUN"]["properties"].(map[string]any)
	if runProperties["attachmentSetRef"] == nil || runProperties["artifactRefs"] != nil {
		t.Fatalf("assistant run schema diverged from accepted attachment contract: %#v", runProperties)
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
	schemas := catalog["operation_schemas"].([]map[string]any)
	if catalog["current_project_ref"] != input.ProjectRef || len(agents) != 2 || agents[0]["ref"] != "agt_analyst1" || len(schemas) != 14 {
		t.Fatalf("unexpected configuration catalog: %#v", catalog)
	}
	workflowFound := false
	for _, schema := range schemas {
		properties := schema["properties"].(map[string]any)
		if properties["type"].(map[string]any)["const"] != "CREATE_WORKFLOW" {
			continue
		}
		workflowFound = properties["parameters"].(map[string]any)["properties"].(map[string]any)["steps"] != nil
	}
	if !workflowFound {
		t.Fatal("configuration catalog does not expose the exact workflow contract")
	}
	if _, err := configurationCatalog(input, map[string]any{"projectRef": "untrusted"}); err == nil {
		t.Fatal("configuration catalog accepted caller input")
	}
	compact, err := configurationCatalog(input, map[string]any{"operation_types": []any{}})
	if err != nil || len(compact.(map[string]any)["operation_schemas"].([]map[string]any)) != 0 ||
		len(compact.(map[string]any)["operation_types"].([]string)) != len(schemas) {
		t.Fatalf("compact configuration catalog is invalid: %v", err)
	}
	selected, err := configurationCatalog(input, map[string]any{"operation_types": []any{"CREATE_AGENT", "LAUNCH_RUN"}})
	if err != nil || len(selected.(map[string]any)["operation_schemas"].([]map[string]any)) != 2 {
		t.Fatalf("selected configuration schemas are invalid: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"operation_types": []any{"UNKNOWN"}},
		{"operation_types": []any{"CREATE_AGENT", "CREATE_AGENT"}},
		{"operation_types": []any{1}},
		{"operation_types": []any{"CREATE_PROJECT", "CREATE_AGENT", "CREATE_WORKFLOW", "LAUNCH_RUN", "CREATE_SCHEDULE"}},
	} {
		if _, err := configurationCatalog(input, invalid); err == nil {
			t.Fatalf("configuration catalog accepted invalid selection: %#v", invalid)
		}
	}
	restricted := input
	restricted.AssistantContext = &runtimecontract.RunnerAssistantContext{AllowedOperations: []string{"CREATE_AGENT"}}
	if _, err := configurationCatalog(restricted, map[string]any{"operation_types": []any{"CREATE_PROJECT"}}); err == nil {
		t.Fatal("configuration catalog exposed an operation outside the current context")
	}
}

func TestConfigurationCatalogPagesAgentsWithoutExhaustingContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{SystemAssistant: true, ProjectRef: "prj_current"}
	for index := range 45 {
		input.DelegationTargets = append(input.DelegationTargets, runtimecontract.RunnerDelegationTarget{
			Ref: fmt.Sprintf("agt_%08d", index), Name: fmt.Sprintf("Сотрудник %03d", index),
			Purpose: strings.Repeat("З", 1000), RoleDescription: strings.Repeat("Р", 1000),
		})
	}
	compact := map[string]any{"operation_types": []any{}}
	first, err := configurationCatalog(input, compact)
	if err != nil {
		t.Fatalf("first catalog page: %v", err)
	}
	firstPage := first.(map[string]any)
	if got := len(firstPage["agents"].([]map[string]string)); got != maximumAssistantCatalogAgents ||
		firstPage["agent_total"] != 45 || firstPage["agent_next_offset"] != 20 ||
		len(firstPage["operation_schemas"].([]map[string]any)) != 0 {
		t.Fatalf("compact catalog is not bounded: count=%d total=%v next=%v", got, firstPage["agent_total"], firstPage["agent_next_offset"])
	}
	if len([]rune(firstPage["agents"].([]map[string]string)[0]["purpose"])) != 240 ||
		len([]rune(firstPage["agents"].([]map[string]string)[0]["role_description"])) != 240 {
		t.Fatal("catalog exposed unbounded agent descriptions")
	}
	second, err := configurationCatalog(input, map[string]any{"operation_types": []any{}, "agent_offset": float64(20)})
	if err != nil || second.(map[string]any)["agents"].([]map[string]string)[0]["ref"] != "agt_00000020" ||
		second.(map[string]any)["agent_next_offset"] != 40 {
		t.Fatalf("second catalog page is invalid: %v", err)
	}
	last, err := configurationCatalog(input, map[string]any{"operation_types": []any{}, "agent_offset": 40})
	if err != nil || len(last.(map[string]any)["agents"].([]map[string]string)) != 5 || last.(map[string]any)["agent_next_offset"] != nil {
		t.Fatalf("last catalog page is invalid: %v", err)
	}
	filtered, err := configurationCatalog(input, map[string]any{"operation_types": []any{}, "agent_query": "СОТРУДНИК 042"})
	if err != nil || filtered.(map[string]any)["agent_total"] != 1 ||
		filtered.(map[string]any)["agents"].([]map[string]string)[0]["ref"] != "agt_00000042" {
		t.Fatalf("filtered catalog page is invalid: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"operation_types": []any{}, "agent_offset": float64(1.5)},
		{"operation_types": []any{}, "agent_offset": 129},
		{"operation_types": []any{}, "agent_offset": -1},
		{"operation_types": []any{}, "agent_offset": "20"},
		{"operation_types": []any{}, "agent_query": strings.Repeat("я", 81)},
	} {
		if _, err := configurationCatalog(input, invalid); err == nil {
			t.Fatalf("catalog accepted invalid pagination: %#v", invalid)
		}
	}
}

func TestConfigurationCatalogPinsAgentUpdateToExactContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{SystemAssistant: true, ProjectRef: "prj_current", AssistantContext: &runtimecontract.RunnerAssistantContext{
		EntityKind: "AGENT", EntityRef: "agt_current", EntityName: "Coordinator", AllowedOperations: []string{"UPDATE_AGENT"},
	}}
	compact, err := configurationCatalog(input, map[string]any{"operation_types": []any{}})
	if err != nil || !reflect.DeepEqual(compact.(map[string]any)["operation_types"], []string{"UPDATE_AGENT"}) {
		t.Fatalf("unexpected exact-context operation index: result=%#v err=%v", compact, err)
	}
	selected, err := configurationCatalog(input, map[string]any{"operation_types": []any{"UPDATE_AGENT"}})
	if err != nil {
		t.Fatal(err)
	}
	schemas := selected.(map[string]any)["operation_schemas"].([]map[string]any)
	if len(schemas) != 1 || !reflect.DeepEqual(schemas[0]["required"], []string{"type", "title", "summary", "parameters"}) {
		t.Fatalf("agent update must be server hydrated: %#v", schemas)
	}
	parameters := schemas[0]["properties"].(map[string]any)["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	if !reflect.DeepEqual(properties["agentRef"].(map[string]any)["enum"], []string{"agt_current"}) ||
		properties["avatarUrl"] != nil || len(parameters["anyOf"].([]map[string]any)) != 3 {
		t.Fatalf("agent update leaked another target or immutable field: %#v", parameters)
	}
	if target := assistantServerTarget("UPDATE_AGENT", map[string]any{"agentRef": "agt_current", "purpose": "Coordinate releases"}, input.AssistantContext); target == nil || target["name"] != "Coordinator" {
		t.Fatalf("server target lost current agent name: %#v", target)
	}
	if target := assistantServerTarget("UPDATE_AGENT", map[string]any{"agentRef": "agt_other", "purpose": "Coordinate releases"}, input.AssistantContext); target != nil {
		t.Fatalf("server accepted a different agent target: %#v", target)
	}
	input.AssistantContext.AllowedOperations = nil
	if result, err := configurationCatalog(input, map[string]any{"operation_types": []any{}}); err != nil || len(result.(map[string]any)["operation_types"].([]string)) != 0 {
		t.Fatalf("empty context exposed operations: result=%#v err=%v", result, err)
	}
}

func TestConfigurationCatalogPinsConnectionUpdateToExactContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{SystemAssistant: true, AssistantContext: &runtimecontract.RunnerAssistantContext{
		EntityKind: "INTEGRATION_CONNECTION", EntityRef: "con_current", EntityName: "Source", AllowedOperations: []string{"UPDATE_INTEGRATION_CONNECTION"},
	}}
	selected, err := configurationCatalog(input, map[string]any{"operation_types": []any{"UPDATE_INTEGRATION_CONNECTION"}})
	if err != nil {
		t.Fatal(err)
	}
	schemas := selected.(map[string]any)["operation_schemas"].([]map[string]any)
	if len(schemas) != 1 || !reflect.DeepEqual(schemas[0]["required"], []string{"type", "title", "summary", "parameters"}) {
		t.Fatalf("connection update must be server hydrated: %#v", schemas)
	}
	parameters := schemas[0]["properties"].(map[string]any)["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	if !reflect.DeepEqual(properties["connectionRef"].(map[string]any)["enum"], []string{"con_current"}) ||
		properties["credential"] != nil || properties["definitionKey"] != nil || len(parameters["anyOf"].([]map[string]any)) != 2 {
		t.Fatalf("connection update schema leaked target or secret fields: %#v", parameters)
	}
	if target := assistantServerTarget("UPDATE_INTEGRATION_CONNECTION", map[string]any{"connectionRef": "con_current", "name": "Source code"}, input.AssistantContext); target == nil || target["name"] != "Source" {
		t.Fatalf("server target lost exact connection: %#v", target)
	}
	if target := assistantServerTarget("UPDATE_INTEGRATION_CONNECTION", map[string]any{"connectionRef": "con_other", "name": "Source code"}, input.AssistantContext); target != nil {
		t.Fatalf("server accepted a different connection: %#v", target)
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

func TestAssistantPlanControlValidationRemainsRetryableInputError(t *testing.T) {
	t.Parallel()
	err := assistantPlanControlError(status.Error(codes.InvalidArgument, "request is invalid"))
	var inputErr *assistantPlanInputError
	if !errors.As(err, &inputErr) || inputErr.reason != "server_validation" {
		t.Fatalf("expected closed server validation error, got %v", err)
	}
	if code := status.Code(assistantPlanControlError(status.Error(codes.Unavailable, "down"))); code != codes.Unavailable {
		t.Fatalf("transient control error code changed: %s", code)
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

func TestNormalizeEnvironmentDraftPinsCurrentProjectAndKeepsOnlyMetadata(t *testing.T) {
	t.Parallel()
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"type": "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
		"parameters": map[string]any{
			"project_ref": "prj_untrusted", "name": "Developer environment",
			"image_artifact_ref": "imgart_selected1",
		},
	}, "Prepare environment", "prj_current1", "Marketplace")
	if err != nil {
		t.Fatal(err)
	}
	parameters := operation["parameters"].(map[string]any)
	if parameters["projectRef"] != "prj_current1" || parameters["imageArtifactRef"] != "imgart_selected1" ||
		operation["title"] != "Создать черновик среды «Developer environment»" ||
		assistantServerTarget("CREATE_RUNTIME_ENVIRONMENT_DRAFT", parameters, nil)["kind"] != "RUNTIME_ENVIRONMENT_DRAFT" {
		t.Fatalf("environment draft was not server-bound: %#v", operation)
	}
}

func TestNormalizeRoleImageRecipePinsCurrentProjectAndAgentReference(t *testing.T) {
	t.Parallel()
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"type": "CREATE_ROLE_IMAGE_RECIPE",
		"parameters": map[string]any{"project_ref": "prj_untrusted", "agent_ref": "agt_selected1",
			"name": "Developer image", "environment_key": "standard"},
	}, "Create image", "prj_current1", "Marketplace")
	if err != nil {
		t.Fatal(err)
	}
	parameters := operation["parameters"].(map[string]any)
	if parameters["projectRef"] != "prj_current1" || parameters["agentRef"] != "agt_selected1" ||
		parameters["environmentKey"] != "standard" || operation["title"] != "Создать рецепт образа «Developer image»" ||
		assistantServerTarget("CREATE_ROLE_IMAGE_RECIPE", parameters, nil)["kind"] != "ROLE_IMAGE_RECIPE" {
		t.Fatalf("role image recipe was not server-bound: %#v", operation)
	}
}
