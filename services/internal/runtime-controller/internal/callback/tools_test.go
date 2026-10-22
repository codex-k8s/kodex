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

// rejectedIntegrationClient is a local owner-RPC stub whose invocation transitions
// from pending resolution to rejection; embedded unused methods must not be called.
// rejectedIntegrationClient — локальный owner-RPC stub с переходом invocation
// от pending resolution к rejection; embedded unused methods не вызываются.
type rejectedIntegrationClient struct {
	controlplanev1.RuntimeWorkServiceClient
}

// unavailableIntegrationClient injects a transient resolve error without invoking
// a real owner, provider or mail transport.
// unavailableIntegrationClient подставляет transient resolve error без обращения
// к реальному owner, провайдеру или почтовому transport.
type unavailableIntegrationClient struct {
	controlplanev1.RuntimeWorkServiceClient
}

// pendingIntegrationClient keeps a synthetic invocation waiting for approval
// so tool-return behavior can be tested without a human decision or external effect.
// pendingIntegrationClient удерживает synthetic invocation в ожидании approval
// для проверки возврата tool без human decision и внешнего эффекта.
type pendingIntegrationClient struct {
	controlplanev1.RuntimeWorkServiceClient
}

// ResolveIntegrationInvocation returns one fixed pending locator; it does not
// validate request authority, persist an intent or perform an integration operation.
// ResolveIntegrationInvocation возвращает фиксированный pending locator, не проверяя
// request authority, не сохраняя intent и не исполняя integration operation.
func (*pendingIntegrationClient) ResolveIntegrationInvocation(_ context.Context, _ *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: "inv_pending1", State: "WAITING_APPROVAL"}, nil
}

// GetIntegrationInvocation always returns WAITING_APPROVAL, allowing the test
// to detect any attempt to wait for a terminal result inside the tool request.
// GetIntegrationInvocation всегда возвращает WAITING_APPROVAL, позволяя тесту
// обнаружить ожидание terminal result внутри tool request.
func (*pendingIntegrationClient) GetIntegrationInvocation(_ context.Context, _ *controlplanev1.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	return &controlplanev1.GetIntegrationInvocationResponse{State: "WAITING_APPROVAL"}, nil
}

// ResolveIntegrationInvocation supplies a fixed initially pending locator for
// the rejection scenario, without creating a server-owned approval decision.
// ResolveIntegrationInvocation выдаёт фиксированный initially pending locator
// сценария отказа, не создавая server-owned approval decision.
func (*rejectedIntegrationClient) ResolveIntegrationInvocation(_ context.Context, _ *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: "inv_rejected1", State: "WAITING_APPROVAL"}, nil
}

// GetIntegrationInvocation returns synthetic REJECTED with its safe owner error
// code; no real invitation is denied or sent by this stub.
// GetIntegrationInvocation возвращает synthetic REJECTED и safe owner error code;
// этот stub не отклоняет и не отправляет реальное приглашение.
func (*rejectedIntegrationClient) GetIntegrationInvocation(_ context.Context, _ *controlplanev1.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	return &controlplanev1.GetIntegrationInvocationResponse{State: "REJECTED", SafeErrorCode: "INTEGRATION_REJECTED_BY_OWNER"}, nil
}

// ResolveIntegrationInvocation always returns gRPC Unavailable to verify that
// the caller preserves transient status rather than inventing a terminal business result.
// ResolveIntegrationInvocation всегда возвращает gRPC Unavailable для проверки
// сохранения transient status вместо выдуманного terminal business result.
func (*unavailableIntegrationClient) ResolveIntegrationInvocation(_ context.Context, _ *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	return nil, status.Error(codes.Unavailable, "transient control-plane failure")
}

// integrationGrantFixture builds one synthetic reviewed-write grant with the exact
// digest of its closed value schema. Fixed revisions/digests are fixture data,
// not owner-issued credentials, a live connection or an approval decision.
// integrationGrantFixture создаёт synthetic reviewed-write grant с точным digest
// закрытой value schema. Фиксированные revisions/digests — fixture data,
// не owner-issued credentials, live connection и не approval decision.
func integrationGrantFixture() runtimecontract.RunnerIntegrationGrant {
	inputSchema := `{"additionalProperties":false,"properties":{"value":{"maxLength":4096,"minLength":1,"type":"string"}},"required":["value"],"type":"object"}`
	digest := sha256.Sum256([]byte(inputSchema))
	return runtimecontract.RunnerIntegrationGrant{
		Ref: "igr_12345678", GrantVersion: 1, ConnectionRef: "icon_12345678", ConnectionVersion: 1, ApprovalPolicy: "HUMAN_EACH_EFFECT", DefinitionKey: "synthetic",
		ConnectionName: "Synthetic", CapabilityKey: "synthetic.journal.write", CapabilityName: "Write journal",
		CapabilityDescription: "Write one bounded journal value.", Risk: "WRITE", DefinitionVersion: "3.1.0",
		DefinitionDigest: strings.Repeat("a", 64), Operation: "synthetic.journal.write", InputSchema: inputSchema,
		InputSchemaSHA256: hex.EncodeToString(digest[:]),
	}
}

// integrationArguments builds the single supported grant_ref/input shape with
// one value; all callers use fresh maps, and no owner/connection selector is injected.
// integrationArguments создаёт единственную поддерживаемую форму grant_ref/input
// с одним value; maps новые, owner/connection selector не подставляется.
func integrationArguments(grant runtimecontract.RunnerIntegrationGrant, value string) map[string]any {
	return map[string]any{"grant_ref": grant.Ref, "input": map[string]any{"value": value}}
}

// integrationInvocationFixture creates fresh timeout/client/input/grant state for
// the three local invocation outcome tests. It shares no mutable fixture between
// parallel tests and performs no owner RPC, external effect or approval decision.
// integrationInvocationFixture создаёт новые timeout/client/input/grant fixtures
// для трёх локальных outcome tests. Между parallel tests нет общей изменяемой
// fixture; owner RPC, внешние эффекты и approval decision не выполняются.
func integrationInvocationFixture(client controlplanev1.RuntimeWorkServiceClient) (*Server, runtimecontract.RunnerInput, runtimecontract.RunnerIntegrationGrant) {
	server := &Server{
		config:  Config{RequestTimeout: time.Second},
		control: &controlplaneclient.Client{Runtime: client},
	}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{
		RunRef: "run_12345678", NodeRef: "nod_12345678", LeaseRef: "lse_12345678",
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant},
	}
	return server, input, grant
}

// TestIntegrationInvocationFixtureKeepsScenarioStateIsolated checks fixed timeout,
// input pins and supplied stub identity, then mutates one returned fixture and
// argument map to prove a second call retains independent mutable state.
// TestIntegrationInvocationFixtureKeepsScenarioStateIsolated проверяет timeout,
// input pins и identity переданного stub, затем изменяет одну fixture и argument
// map, доказывая независимость изменяемого состояния второго вызова.
func TestIntegrationInvocationFixtureKeepsScenarioStateIsolated(t *testing.T) {
	t.Parallel()
	firstClient, secondClient := &rejectedIntegrationClient{}, &pendingIntegrationClient{}
	firstServer, firstInput, firstGrant := integrationInvocationFixture(firstClient)
	secondServer, secondInput, secondGrant := integrationInvocationFixture(secondClient)
	if firstServer == secondServer || firstServer.control == secondServer.control ||
		firstServer.control.Runtime != firstClient || secondServer.control.Runtime != secondClient ||
		firstServer.config.RequestTimeout != time.Second || secondServer.config.RequestTimeout != time.Second {
		t.Fatal("invocation fixture lost independent server, timeout or client identity")
	}
	expected := runtimecontract.RunnerInput{
		RunRef: "run_12345678", NodeRef: "nod_12345678", LeaseRef: "lse_12345678",
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{integrationGrantFixture()},
	}
	if !reflect.DeepEqual(firstInput, expected) || !reflect.DeepEqual(secondInput, expected) ||
		!reflect.DeepEqual(firstGrant, expected.IntegrationGrants[0]) || !reflect.DeepEqual(secondGrant, firstGrant) {
		t.Fatal("invocation fixture changed original input or grant")
	}
	firstInput.IntegrationGrants[0].Ref = "igr_changed"
	firstServer.config.RequestTimeout = 0
	firstArguments, secondArguments := integrationArguments(firstGrant, "first"), integrationArguments(secondGrant, "second")
	firstArguments["grant_ref"] = "igr_changed"
	firstArguments["input"].(map[string]any)["value"] = "changed"
	if !reflect.DeepEqual(secondInput, expected) || secondServer.config.RequestTimeout != time.Second ||
		secondArguments["grant_ref"] != secondGrant.Ref || secondArguments["input"].(map[string]any)["value"] != "second" {
		t.Fatal("parallel invocation fixtures or argument maps share mutable state")
	}
}

// TestInvokeReturnsRejectedIntegrationAsTerminalResult verifies rejection is a typed
// negative tool result with the original invocation ref and owner error code, not a
// transport failure. It uses only a synthetic owner stub and performs no external effect.
// TestInvokeReturnsRejectedIntegrationAsTerminalResult проверяет typed negative tool
// result с исходными invocation ref и owner error code, а не transport failure.
// Используется только synthetic owner stub, внешних эффектов нет.
func TestInvokeReturnsRejectedIntegrationAsTerminalResult(t *testing.T) {
	t.Parallel()
	server, input, grant := integrationInvocationFixture(&rejectedIntegrationClient{})
	result, err := server.invoke(t.Context(), input, integrationArguments(grant, "rejected"), json.RawMessage(`"call-1"`))
	if err != nil {
		t.Fatalf("invoke rejected integration: %v", err)
	}
	values, ok := result.(integrationToolResult)
	if !ok || values.OK || values.ErrorCode != "INTEGRATION_REJECTED_BY_OWNER" || values.InvocationRef != "inv_rejected1" {
		t.Fatalf("unexpected rejected integration result: %#v", result)
	}
}

// TestInvokeReturnsPendingApprovalWithoutHoldingToolRequest verifies pending approval
// returns immediately as a typed owner-decision-required result with a safe activity
// projection, rather than blocking the request or fabricating completed approval.
// TestInvokeReturnsPendingApprovalWithoutHoldingToolRequest проверяет немедленный
// typed owner-decision-required result и safe activity projection при pending approval
// вместо блокировки request или выдуманного завершённого approval.
func TestInvokeReturnsPendingApprovalWithoutHoldingToolRequest(t *testing.T) {
	t.Parallel()
	server, input, grant := integrationInvocationFixture(&pendingIntegrationClient{})
	result, err := server.invoke(t.Context(), input, integrationArguments(grant, "pending"), json.RawMessage(`"call-1"`))
	if err != nil {
		t.Fatalf("invoke pending integration: %v", err)
	}
	values, ok := result.(integrationToolResult)
	if !ok || values.OK || !values.OwnerDecisionRequired || values.ErrorCode != "INTEGRATION_APPROVAL_PENDING" || values.state != "WAITING_APPROVAL" {
		t.Fatalf("unexpected pending integration result: %#v", result)
	}
	if safeToolCallResult("invoke_integration", result, nil) == "TOOL_UNAVAILABLE" {
		t.Fatal("pending approval lost its safe tool-call result")
	}
}

// TestInvokePreservesIntegrationResolutionStatus verifies a fake owner's transient
// Unavailable survives invocation error propagation; it does not test real owner availability.
// TestInvokePreservesIntegrationResolutionStatus проверяет сохранение transient Unavailable
// fake owner при возврате ошибки invocation, а не доступность реального owner.
func TestInvokePreservesIntegrationResolutionStatus(t *testing.T) {
	t.Parallel()
	server, input, grant := integrationInvocationFixture(&unavailableIntegrationClient{})
	_, err := server.invoke(t.Context(), input, integrationArguments(grant, "unavailable"), json.RawMessage(`"call-1"`))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("integration resolution status was lost: %v", err)
	}
}

// TestAssistantPlanToolIsSystemOnlyAndBounded checks ordinary-runtime exclusion,
// compact assistant tools/list and the specialized create/update/launch/grant schema
// shapes, limits, target branches and absence of credential fields. The historical
// name does not claim PROJECT assistants are excluded; this fixture uses SYSTEM.
// TestAssistantPlanToolIsSystemOnlyAndBounded проверяет отсутствие tool у обычного
// runtime, компактность assistant tools/list и формы специализированных create/update/
// launch/grant schemas, limits, target branches и отсутствие credential fields.
// Историческое имя не доказывает запрет PROJECT: fixture использует SYSTEM.
func TestAssistantPlanToolIsSystemOnlyAndBounded(t *testing.T) {
	t.Parallel()
	if encoded, _ := json.Marshal(tools(runtimecontract.RunnerInput{})); strings.Contains(string(encoded), "propose_configuration_plan") {
		t.Fatal("ordinary runtime must not receive the system assistant tool")
	}
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_12345678", DelegationTargets: []runtimecontract.RunnerDelegationTarget{{Ref: "agt_12345678", Name: "Analyst"}}}
	available := tools(input)
	if len(available) != 7 {
		t.Fatalf("unexpected assistant tool catalog: %#v", available)
	}
	if encoded, err := json.Marshal(available); err != nil || len(encoded) > maximumAssistantCompactCatalogBytes {
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
	if len(oneOf) != 19 {
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
	if byType["CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"] == nil {
		t.Fatal("specialized system assistant integration grant operation is absent")
	}
	fileProperties := byType["CREATE_PROJECT_FILE"]["properties"].(map[string]any)
	if fileProperties["projectRef"] == nil || fileProperties["fileName"] == nil ||
		fileProperties["mediaType"] == nil || fileProperties["contentEncoding"] == nil || fileProperties["content"] == nil {
		t.Fatalf("project file schema is incomplete: %#v", fileProperties)
	}
	if !reflect.DeepEqual(fileProperties["contentEncoding"].(map[string]any)["enum"], []string{"UTF8", "BASE64"}) {
		t.Fatalf("project file encoding schema is not closed: %#v", fileProperties["contentEncoding"])
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
	agentProperties := byType["CREATE_AGENT"]["properties"].(map[string]any)
	for field, limit := range map[string]int{"name": 120, "purpose": 1000, "roleDescription": 1000} {
		if agentProperties[field].(map[string]any)["maxLength"] != limit {
			t.Fatalf("agent %s limit differs from the owner form: %#v", field, agentProperties[field])
		}
	}
	if agentProperties["runtimeRef"] == nil || agentProperties["secretValue"] != nil {
		t.Fatalf("agent schema lost runtime selection or exposed secret input: %#v", agentProperties)
	}
	workflowProperties := byType["CREATE_WORKFLOW"]["properties"].(map[string]any)
	if workflowProperties["projectRef"].(map[string]any)["enum"].([]string)[0] != input.ProjectRef ||
		workflowProperties["coordinatorAgentRef"].(map[string]any)["enum"].([]string)[0] != input.DelegationTargets[0].Ref {
		t.Fatalf("workflow schema is not bound to the server catalog: %#v", workflowProperties)
	}
	environmentProperties := byType["CREATE_RUNTIME_ENVIRONMENT_DRAFT"]["properties"].(map[string]any)
	if environmentProperties["projectRef"].(map[string]any)["enum"].([]string)[0] != input.ProjectRef ||
		environmentProperties["imageArtifactRef"] == nil || environmentProperties["publicValues"] == nil ||
		environmentProperties["secretBindings"] == nil || environmentProperties["secretSuggestions"] == nil || environmentProperties["secretValue"] != nil {
		t.Fatalf("environment draft schema lost project binding or artifact pointer: %#v", environmentProperties)
	}
	suggestion := environmentProperties["secretSuggestions"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if suggestion["sourceHelp"] == nil || suggestion["valueType"] == nil || suggestion["value"] != nil || suggestion["secretValue"] != nil {
		t.Fatalf("environment secret suggestion exposed a credential value: %#v", suggestion)
	}
	imageProperties := byType["CREATE_ROLE_IMAGE_RECIPE"]["properties"].(map[string]any)
	if imageProperties["projectRef"].(map[string]any)["enum"].([]string)[0] != input.ProjectRef ||
		imageProperties["agentRef"] == nil || imageProperties["name"] == nil || imageProperties["dockerfile"] == nil ||
		imageProperties["agentVersion"] != nil || imageProperties["secretValue"] != nil {
		t.Fatalf("role image schema exposed owner fields or lost project binding: %#v", imageProperties)
	}
	updateImageProperties := byType["UPDATE_ROLE_IMAGE_RECIPE"]["properties"].(map[string]any)
	if updateImageProperties["recipeRef"] == nil || updateImageProperties["name"] == nil || updateImageProperties["dockerfile"] == nil ||
		updateImageProperties["agentRef"] != nil || updateImageProperties["secretValue"] != nil {
		t.Fatalf("role image update schema exposed owner fields: %#v", updateImageProperties)
	}
	stepProperties := workflowProperties["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if len(stepProperties["parallelGroup"].(map[string]any)["oneOf"].([]map[string]any)) != 2 {
		t.Fatalf("workflow schema must admit numeric and named parallel groups: %#v", stepProperties["parallelGroup"])
	}
	for _, operationType := range []string{"CREATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION", "PUBLISH_INTEGRATION_DEFINITION"} {
		if byType[operationType] == nil {
			t.Fatalf("assistant tool lost specialized operation %q", operationType)
		}
	}
	publication := byType["PUBLISH_INTEGRATION_DEFINITION"]["properties"].(map[string]any)
	if len(publication) != 2 || publication["configurationRef"] == nil || publication["revisionRef"] == nil {
		t.Fatalf("publication schema exposed raw specification or authority fields: %#v", publication)
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

// TestConfigurationCatalogReturnsOnlyServerOwnedBindings checks sorted snapshot
// agent refs, compact type-only discovery, selected schema expansion and rejection of
// unknown, duplicate, excessive or context-disallowed kinds; no live eligibility read occurs.
// TestConfigurationCatalogReturnsOnlyServerOwnedBindings проверяет sorted snapshot
// agent refs, compact type-only discovery, выбор схем и отказ unknown, duplicate,
// excessive и context-disallowed kinds; live eligibility read не выполняется.
func TestConfigurationCatalogReturnsOnlyServerOwnedBindings(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_12345678", DelegationTargets: []runtimecontract.RunnerDelegationTarget{
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
	if catalog["current_project_ref"] != input.ProjectRef || len(agents) != 2 || agents[0]["ref"] != "agt_analyst1" || len(schemas) != 0 ||
		len(catalog["operation_types"].([]string)) != 19 {
		t.Fatalf("unexpected configuration catalog: %#v", catalog)
	}
	foundSystemGrant := false
	for _, operation := range catalog["operation_types"].([]string) {
		if operation == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			foundSystemGrant = true
		}
	}
	if !foundSystemGrant {
		t.Fatal("specialized system assistant grant operation missing")
	}
	if _, err := configurationCatalog(input, map[string]any{"projectRef": "untrusted"}); err == nil {
		t.Fatal("configuration catalog accepted caller input")
	}
	compact, err := configurationCatalog(input, map[string]any{"operation_types": []any{}})
	if err != nil || len(compact.(map[string]any)["operation_schemas"].([]map[string]any)) != 0 ||
		len(compact.(map[string]any)["operation_types"].([]string)) != 19 {
		t.Fatalf("compact configuration catalog is invalid: %v", err)
	}
	selected, err := configurationCatalog(input, map[string]any{"operation_types": []any{"CREATE_AGENT", "LAUNCH_RUN", "CREATE_WORKFLOW"}})
	if err != nil || len(selected.(map[string]any)["operation_schemas"].([]map[string]any)) != 3 {
		t.Fatalf("selected configuration schemas are invalid: %v", err)
	}
	workflowFound := false
	for _, schema := range selected.(map[string]any)["operation_schemas"].([]map[string]any) {
		properties := schema["properties"].(map[string]any)
		if properties["type"].(map[string]any)["const"] == "CREATE_WORKFLOW" {
			workflowFound = properties["parameters"].(map[string]any)["properties"].(map[string]any)["steps"] != nil
		}
	}
	if !workflowFound {
		t.Fatal("configuration catalog does not expose the selected workflow contract")
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

// TestWorkflowUpdateSchemaIsExactAndIncludesEditableGraph checks the exact workflow
// locator, editable graph identity fields and capability-ceiling guidance for both
// create/update shapes; it does not publish or execute a workflow DAG.
// TestWorkflowUpdateSchemaIsExactAndIncludesEditableGraph проверяет exact workflow
// locator, изменяемые graph identity fields и capability-ceiling guidance create/update
// форм; публикация и выполнение workflow DAG не проверяются.
func TestWorkflowUpdateSchemaIsExactAndIncludesEditableGraph(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_12345678",
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "WORKFLOW", EntityRef: "wfl_12345678",
			AllowedOperations: []string{"UPDATE_WORKFLOW"}}}
	schemas := assistantPlanOperationSchemas(input)
	if len(schemas) != 1 {
		t.Fatalf("unexpected workflow update schemas: %#v", schemas)
	}
	properties := schemas[0]["properties"].(map[string]any)
	if properties["type"].(map[string]any)["const"] != "UPDATE_WORKFLOW" {
		t.Fatalf("wrong workflow operation: %#v", properties)
	}
	parameters := properties["parameters"].(map[string]any)
	fields := parameters["properties"].(map[string]any)
	if fields["completionCriteria"].(map[string]any)["maxLength"] != 2000 {
		t.Fatal("workflow completion criteria exceeded the canonical character limit")
	}
	if fields["workflowRef"].(map[string]any)["enum"].([]string)[0] != "wfl_12345678" ||
		fields["steps"] == nil || fields["inputFields"] == nil || fields["coordinatorAgentRef"] == nil || fields["projectRef"] != nil {
		t.Fatalf("workflow update schema lost editable graph or exposed project authority: %#v", fields)
	}
	stepSchema := fields["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	fieldSchema := fields["inputFields"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if stepSchema["key"] == nil || fieldSchema["key"] == nil || stepSchema["requiredCapabilityKeys"] == nil {
		t.Fatalf("workflow update schema lost graph identity or capabilities: %#v %#v", stepSchema, fieldSchema)
	}
	create := workflowInputSchema(opaqueRefSchema(), opaqueRefSchema())["properties"].(map[string]any)["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	for _, schema := range []map[string]any{stepSchema, create} {
		capabilities := schema["requiredCapabilityKeys"].(map[string]any)
		description, _ := capabilities["description"].(string)
		if !strings.Contains(description, "Complete stage capability ceiling") || !strings.Contains(description, "existing enabled integration keys") || !strings.Contains(description, "never grants permissions") || capabilities["maxItems"] != 50 {
			t.Fatal("Workflow schema did not explain exact capability attenuation")
		}
	}
}

// TestEnvironmentRevisionSchemaIsExactAndSecretValueFree checks pinned environment
// proposal fields, sparse value edits, closed policy and bounded nonempty method subsets
// without secret values, hostPath or service-account selectors; no policy is deployed.
// TestEnvironmentRevisionSchemaIsExactAndSecretValueFree проверяет pinned environment
// proposal fields, sparse value edits, closed policy и bounded nonempty method subsets
// без secret values,hostPath/service-account selectors; policy не развёртывается.
func TestEnvironmentRevisionSchemaIsExactAndSecretValueFree(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_12345678",
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "ENVIRONMENT", EntityRef: "renv_12345678",
			AllowedOperations: []string{"PREPARE_RUNTIME_ENVIRONMENT_REVISION"}}}
	schemas := assistantPlanOperationSchemas(input)
	if len(schemas) != 1 {
		t.Fatalf("unexpected environment revision schemas: %#v", schemas)
	}
	properties := schemas[0]["properties"].(map[string]any)
	if properties["type"].(map[string]any)["const"] != "PREPARE_RUNTIME_ENVIRONMENT_REVISION" ||
		properties["action"].(map[string]any)["const"] != "UPDATE" {
		t.Fatalf("wrong environment revision operation: %#v", properties)
	}
	fields := properties["parameters"].(map[string]any)["properties"].(map[string]any)
	if fields["environmentRef"].(map[string]any)["enum"].([]string)[0] != "renv_12345678" ||
		fields["publicValues"] == nil || fields["publicValueUpdates"] == nil || fields["publicValueRemovals"] == nil ||
		fields["secretBindings"] == nil || fields["tools"] == nil || fields["policy"] == nil ||
		fields["secretValue"] != nil || fields["values"] != nil || fields["projectRef"] != nil {
		t.Fatalf("environment revision schema exposed protected fields: %#v", fields)
	}
	policy := fields["policy"].(map[string]any)
	policyFields := policy["properties"].(map[string]any)
	if policy["additionalProperties"] != false || len(policyFields) != 5 || policyFields["webAccess"] == nil ||
		policyFields["hostPath"] != nil || policyFields["serviceAccountName"] != nil || policyFields["secretValue"] != nil {
		t.Fatalf("environment policy schema escaped its closed boundary: %#v", policy)
	}
	webAccessFields := policyFields["webAccess"].(map[string]any)["properties"].(map[string]any)
	ruleFields := webAccessFields["rules"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	httpMethods := ruleFields["httpMethods"].(map[string]any)
	if httpMethods["minItems"] != 1 || httpMethods["maxItems"] != 7 || httpMethods["uniqueItems"] != true {
		t.Fatalf("environment policy schema does not allow an exact non-empty HTTP method subset: %#v", httpMethods)
	}
}

// TestSystemAssistantCanProposeOwnEnvironmentRevisionOutsideEnvironmentContext checks
// self environment/system-assistant pins remain discoverable from an unrelated project
// screen; schema presence is not permission to apply the proposal.
// TestSystemAssistantCanProposeOwnEnvironmentRevisionOutsideEnvironmentContext проверяет
// discovery собственных environment/system-assistant pins с другого project screen;
// наличие схемы не даёт разрешения применить предложение.
func TestSystemAssistantCanProposeOwnEnvironmentRevisionOutsideEnvironmentContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{
		AssistantScope: runtimecontract.AssistantScopeSystem, AgentRef: "agt_system123", RuntimeEnvironmentRef: "renv_system123",
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "PROJECT", EntityRef: "prj_12345678",
			AllowedOperations: []string{"CREATE_AGENT"}},
	}
	schemas := assistantPlanOperationSchemas(input)
	var parameters map[string]any
	for _, schema := range schemas {
		properties := schema["properties"].(map[string]any)
		if properties["type"].(map[string]any)["const"] == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			parameters = properties["parameters"].(map[string]any)
			break
		}
	}
	if parameters == nil {
		t.Fatal("system assistant environment operation is not discoverable")
	}
	fields := assistantOrdinaryParametersSchema(parameters)["properties"].(map[string]any)
	if fields["environmentRef"].(map[string]any)["enum"].([]string)[0] != input.RuntimeEnvironmentRef ||
		fields["systemAssistantRef"].(map[string]any)["enum"].([]string)[0] != input.AgentRef {
		t.Fatalf("system assistant environment schema is not pinned: %#v", fields)
	}
}

// TestSystemAssistantCanProposeOwnInstructionsFromAnyContext checks self instruction
// UPDATE shape, bound assistant ref, text limit and local SYSTEM_ASSISTANT target
// outside its own screen; it does not change persisted instructions.
// TestSystemAssistantCanProposeOwnInstructionsFromAnyContext проверяет self instruction
// UPDATE shape, bound assistant ref, text limit и локальный SYSTEM_ASSISTANT target
// вне собственного экрана, не меняя сохранённые instructions.
func TestSystemAssistantCanProposeOwnInstructionsFromAnyContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{
		AssistantScope: runtimecontract.AssistantScopeSystem,
		AgentRef:       "agt_system123",
		AssistantContext: &runtimecontract.RunnerAssistantContext{
			EntityKind: "PROJECT", EntityRef: "prj_12345678", EntityName: "Marketplace",
			AllowedOperations: []string{"CREATE_AGENT"},
		},
	}
	var schema map[string]any
	for _, candidate := range assistantPlanOperationSchemas(input) {
		properties := candidate["properties"].(map[string]any)
		if properties["type"].(map[string]any)["const"] == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
			schema = candidate
			break
		}
	}
	if schema == nil {
		t.Fatal("system assistant instruction operation is not discoverable")
	}
	properties := schema["properties"].(map[string]any)
	parameters := properties["parameters"].(map[string]any)["properties"].(map[string]any)
	if properties["action"].(map[string]any)["const"] != "UPDATE" ||
		parameters["systemAssistantRef"].(map[string]any)["enum"].([]string)[0] != input.AgentRef ||
		parameters["instructions"].(map[string]any)["maxLength"] != 20000 {
		t.Fatalf("system assistant instruction schema is not pinned: %#v", schema)
	}
	target := assistantServerTarget("UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", map[string]any{
		"systemAssistantRef": input.AgentRef,
	}, input.AssistantContext)
	if target == nil || target["kind"] != "SYSTEM_ASSISTANT" || target["name"] != "Kodex" {
		t.Fatalf("system assistant target is unavailable: %#v", target)
	}
}

// TestAgentEnvironmentBindingSchemaIsExact checks the current agent locator and
// environment-binding UPDATE shape, excluding caller versions and plaintext secrets.
// TestAgentEnvironmentBindingSchemaIsExact проверяет locator текущего agent
// и environment-binding UPDATE shape без caller versions и plaintext secrets.
func TestAgentEnvironmentBindingSchemaIsExact(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_12345678",
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: "agt_12345678",
			AllowedOperations: []string{"BIND_AGENT_RUNTIME_ENVIRONMENT"}}}
	schemas := assistantPlanOperationSchemas(input)
	if len(schemas) != 1 {
		t.Fatalf("unexpected agent environment binding schemas: %#v", schemas)
	}
	properties := schemas[0]["properties"].(map[string]any)
	if properties["type"].(map[string]any)["const"] != "BIND_AGENT_RUNTIME_ENVIRONMENT" ||
		properties["action"].(map[string]any)["const"] != "UPDATE" {
		t.Fatalf("wrong binding operation: %#v", properties)
	}
	fields := properties["parameters"].(map[string]any)["properties"].(map[string]any)
	if fields["agentRef"].(map[string]any)["enum"].([]string)[0] != "agt_12345678" ||
		fields["environmentRef"] == nil || fields["projectRef"] != nil ||
		fields["versionRef"] != nil || fields["secretValue"] != nil {
		t.Fatalf("binding schema exposed authority or secret fields: %#v", fields)
	}
}

// TestIntegrationGrantSchemaBindsRecipientToCurrentContext checks an AGENT screen
// pins only that recipient, while the generic form retains exclusive agent/workflow
// branches and approval-scope fields; it creates neither grant nor approval.
// TestIntegrationGrantSchemaBindsRecipientToCurrentContext проверяет pin только recipient
// AGENT screen, а для generic формы — exclusive agent/workflow branches и approval-scope
// fields; grant и approval не создаются.
func TestIntegrationGrantSchemaBindsRecipientToCurrentContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_12345678",
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: "agt_12345678",
			AllowedOperations: []string{"CHANGE_INTEGRATION_GRANT"}}}
	schemas := assistantPlanOperationSchemas(input)
	if len(schemas) != 1 {
		t.Fatalf("unexpected integration grant schemas: %#v", schemas)
	}
	properties := schemas[0]["properties"].(map[string]any)
	parameters := properties["parameters"].(map[string]any)
	fields := parameters["properties"].(map[string]any)
	if properties["action"].(map[string]any)["const"] != "UPDATE" ||
		fields["agentRef"].(map[string]any)["enum"].([]string)[0] != "agt_12345678" ||
		fields["workflowRef"] != nil || fields["approvalScopePaths"] == nil || parameters["oneOf"] != nil {
		t.Fatalf("integration grant schema is not bound to the current agent: %#v", parameters)
	}
	generic := integrationGrantInputSchema(nil)
	if len(generic["oneOf"].([]map[string]any)) != 2 {
		t.Fatalf("generic integration grant schema lost recipient exclusivity: %#v", generic)
	}
}

// TestConfigurationCatalogPagesAgentsWithoutExhaustingContext checks twenty-entry
// pages, final-page termination, Unicode preview truncation, case-folded search and
// invalid pagination rejection over forty-five local fixture targets.
// TestConfigurationCatalogPagesAgentsWithoutExhaustingContext проверяет страницы
// по двадцать, конец последней, Unicode preview truncation, поиск без учёта регистра
// и отказ неверной pagination на сорока пяти локальных fixture targets.
func TestConfigurationCatalogPagesAgentsWithoutExhaustingContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_current"}
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

// TestConfigurationCatalogPinsAgentUpdateToExactContext checks full descriptions
// only for the current agent, sparse server-hydrated UPDATE_AGENT and local target
// rejection for a different ref; empty allowed operations hide screen mutations.
// TestConfigurationCatalogPinsAgentUpdateToExactContext проверяет полные описания
// лишь текущего agent, sparse server-hydrated UPDATE_AGENT и локальный отказ target
// с другим ref; пустые allowed operations скрывают screen mutations.
func TestConfigurationCatalogPinsAgentUpdateToExactContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_current", AssistantContext: &runtimecontract.RunnerAssistantContext{
		EntityKind: "AGENT", EntityRef: "agt_current", EntityName: "Coordinator", AllowedOperations: []string{"UPDATE_AGENT"},
	}}
	input.DelegationTargets = []runtimecontract.RunnerDelegationTarget{
		{Ref: "agt_current", Name: "Coordinator", Purpose: strings.Repeat("Ц", 500), RoleDescription: strings.Repeat("Р", 500)},
		{Ref: "agt_other", Name: "Other", Purpose: strings.Repeat("Ц", 500), RoleDescription: strings.Repeat("Р", 500)},
	}
	compact, err := configurationCatalog(input, map[string]any{"operation_types": []any{}})
	if err != nil || !reflect.DeepEqual(compact.(map[string]any)["operation_types"], []string{"UPDATE_AGENT"}) {
		t.Fatalf("unexpected exact-context operation index: result=%#v err=%v", compact, err)
	}
	agents := compact.(map[string]any)["agents"].([]map[string]string)
	if len([]rune(agents[0]["purpose"])) != 500 || len([]rune(agents[0]["role_description"])) != 500 ||
		len([]rune(agents[1]["purpose"])) != 240 || len([]rune(agents[1]["role_description"])) != 240 {
		t.Fatal("catalog must expose complete current agent profile and bounded previews for other agents")
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

// TestConfigurationCatalogPinsConnectionUpdateToExactContext checks the exact
// connection locator, name/public-only edits and local mismatched-target rejection,
// without credentials or definition changes in discovery.
// TestConfigurationCatalogPinsConnectionUpdateToExactContext проверяет exact connection
// locator, name/public-only edits и локальный отказ чужого target, без credentials
// и definition changes в discovery.
func TestConfigurationCatalogPinsConnectionUpdateToExactContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, AssistantContext: &runtimecontract.RunnerAssistantContext{
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

// TestConfigurationCatalogPinsScheduleUpdateToExactContext checks a twelve-field
// sparse schedule UPDATE bound to its screen ref, excluding project/input-default
// fields, and rejects a different target in local server-target construction.
// TestConfigurationCatalogPinsScheduleUpdateToExactContext проверяет двенадцать sparse
// schedule UPDATE fields с screen ref, исключение project/input-default fields
// и отказ другого target при локальном построении server target.
func TestConfigurationCatalogPinsScheduleUpdateToExactContext(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem, ProjectRef: "prj_current", AssistantContext: &runtimecontract.RunnerAssistantContext{
		EntityKind: "SCHEDULE", EntityRef: "sch_current", EntityName: "Daily review", AllowedOperations: []string{"UPDATE_SCHEDULE"},
	}}
	selected, err := configurationCatalog(input, map[string]any{"operation_types": []any{"UPDATE_SCHEDULE"}})
	if err != nil {
		t.Fatal(err)
	}
	schemas := selected.(map[string]any)["operation_schemas"].([]map[string]any)
	if len(schemas) != 1 || !reflect.DeepEqual(schemas[0]["required"], []string{"type", "title", "summary", "parameters"}) {
		t.Fatalf("schedule update must be server hydrated: %#v", schemas)
	}
	parameters := schemas[0]["properties"].(map[string]any)["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	if !reflect.DeepEqual(properties["scheduleRef"].(map[string]any)["enum"], []string{"sch_current"}) ||
		properties["projectRef"] != nil || properties["promptInputs"] != nil || len(parameters["anyOf"].([]map[string]any)) != 12 {
		t.Fatalf("schedule update schema leaked another target or pinned fields: %#v", parameters)
	}
	if target := assistantServerTarget("UPDATE_SCHEDULE", map[string]any{"scheduleRef": "sch_current", "name": "Weekly review"}, input.AssistantContext); target == nil || target["name"] != "Daily review" {
		t.Fatalf("server target lost exact schedule: %#v", target)
	}
	if target := assistantServerTarget("UPDATE_SCHEDULE", map[string]any{"scheduleRef": "sch_other", "name": "Weekly review"}, input.AssistantContext); target != nil {
		t.Fatalf("server accepted a different schedule: %#v", target)
	}
}

// containsString performs exact membership search for schema assertions without
// normalizing case or modifying the supplied slice.
// containsString ищет точное совпадение для schema assertions без изменения
// регистра и переданного slice.
func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

// TestDelegationToolPinsWorkflowTargetsAndStepKeys checks a closed delegation shape
// with required workflow step and ordered server-snapshot target/step enums; it
// does not prove pair eligibility or launch a child run.
// TestDelegationToolPinsWorkflowTargetsAndStepKeys проверяет closed delegation shape
// с обязательным workflow step и ordered snapshot target/step enums, не доказывая
// eligibility пары и не запуская дочерний run.
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

// TestDelegationToolExposesNamedServerOwnedTargets checks escaped named metadata,
// bounded role previews, exact enums and optional workflow labels while ensuring
// private instructions/expected results are absent from the description projection.
// TestDelegationToolExposesNamedServerOwnedTargets проверяет escaped named metadata,
// bounded role previews, exact enums и optional workflow labels; private instructions
// и expected results должны отсутствовать в description projection.
func TestDelegationToolExposesNamedServerOwnedTargets(t *testing.T) {
	t.Parallel()
	for _, workflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("workflow_%t", workflow), func(t *testing.T) {
			t.Parallel()
			target := runtimecontract.RunnerDelegationTarget{
				Ref: "agt_12345678", Name: "Developer \"Кодекс\"", Purpose: "Реализовать изменение",
				RoleDescription: strings.Repeat("Я", 300), Instructions: "private-instructions-sentinel",
				ExpectedResult: "private-expected-result-sentinel",
			}
			if workflow {
				target.WorkflowStepKey, target.WorkflowStepName = "implement", "Разработка"
			}
			tool := delegationTool([]runtimecontract.RunnerDelegationTarget{target})
			properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
			description := properties["target_agent_ref"].(map[string]any)["description"].(string)
			_, encoded, found := strings.Cut(description, delegationTargetsDescriptionPrefix)
			var metadata []map[string]string
			if !found || json.Unmarshal([]byte(encoded), &metadata) != nil || len(metadata) != 1 {
				t.Fatal("delegation identity metadata must be a closed JSON projection")
			}
			item := metadata[0]
			if item["ref"] != target.Ref || item["name"] != target.Name || item["purpose"] != target.Purpose ||
				item["role_description"] != strings.Repeat("Я", 240) || item["workflow_step_key"] != target.WorkflowStepKey ||
				strings.Contains(description, "private-") {
				t.Fatal("delegation identity metadata lost its target or leaked instructions")
			}
			expectedFields := 4
			if workflow {
				expectedFields = 6
			}
			if !reflect.DeepEqual(properties["target_agent_ref"].(map[string]any)["enum"], []string{target.Ref}) ||
				(properties["workflow_step_key"] != nil) != workflow || len(item) != expectedFields {
				t.Fatal("delegation metadata changed the server-owned target or step boundary")
			}
		})
	}
}

// TestDelegationToolDescriptionStaysBoundedForManyTargets checks the top-level
// description budget independently of complete per-target metadata/enums for 128
// synthetic targets; it does not assert a bound on the whole encoded catalog.
// TestDelegationToolDescriptionStaysBoundedForManyTargets проверяет top-level description
// budget отдельно от полных per-target metadata/enums для 128 synthetic targets,
// не утверждая предел всего encoded catalog.
func TestDelegationToolDescriptionStaysBoundedForManyTargets(t *testing.T) {
	t.Parallel()
	targets := make([]runtimecontract.RunnerDelegationTarget, 128)
	for index := range targets {
		targets[index] = runtimecontract.RunnerDelegationTarget{
			Ref: fmt.Sprintf("agt_fixture_%03d", index), Name: strings.Repeat("Я", 160),
			Purpose: strings.Repeat("Я", 240), RoleDescription: strings.Repeat("Я", 240),
		}
	}
	tool := delegationTool(targets)
	if len(tool["description"].(string)) > 2000 {
		t.Fatal("delegation tool description exceeds the runner readiness limit")
	}
	properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
	field := properties["target_agent_ref"].(map[string]any)
	_, encoded, found := strings.Cut(field["description"].(string), delegationTargetsDescriptionPrefix)
	var metadata []map[string]string
	if !found || json.Unmarshal([]byte(encoded), &metadata) != nil || len(metadata) != len(targets) ||
		len(field["enum"].([]string)) != len(targets) {
		t.Fatal("bounded delegation description lost allowed target metadata")
	}
}

// TestDelegationToolExplainsCatalogLocalFileHandoff checks immutable-file-pin and
// child-local-resolution guidance for ordinary/workflow delegation without adding
// caller catalog/entry/artifact selectors to its closed input shape.
// TestDelegationToolExplainsCatalogLocalFileHandoff проверяет immutable-file-pin
// и child-local-resolution guidance обычного/workflow delegation без добавления
// caller catalog/entry/artifact selectors в closed input shape.
func TestDelegationToolExplainsCatalogLocalFileHandoff(t *testing.T) {
	t.Parallel()
	for _, workflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("workflow_%t", workflow), func(t *testing.T) {
			t.Parallel()
			target := runtimecontract.RunnerDelegationTarget{Ref: "agt_12345678", Name: "Architect"}
			if workflow {
				target.WorkflowStepKey = "architect"
			}
			tool := delegationTool([]runtimecontract.RunnerDelegationTarget{target})
			description := tool["description"].(string)
			for _, hint := range []string{
				"Delegate exact pairs", "end turn, await callback",
				"Immutable file pins: artifact_ref/revision/digest+file_name only", "child resolves own entry",
			} {
				if !strings.Contains(description, hint) {
					t.Fatal("delegation description omitted catalog-local handoff guidance")
				}
			}
			if len(description) > 2000 {
				t.Fatal("handoff guidance exceeds the runner description budget")
			}
			schema := tool["inputSchema"].(map[string]any)
			properties := schema["properties"].(map[string]any)
			wanted := 3
			if workflow {
				wanted++
			}
			if schema["additionalProperties"] != false || len(properties) != wanted ||
				!reflect.DeepEqual(properties["input"], map[string]any{"type": "object", "additionalProperties": true}) ||
				properties["entry_ref"] != nil || properties["catalog"] != nil || properties["artifact_ref"] != nil {
				t.Fatal("handoff guidance changed the delegation input boundary")
			}
		})
	}
}

// TestDecodeMCPToolCallParamsAcceptsStandardMetadata verifies standard _meta does
// not alter the decoded tool name/arguments; it does not interpret progressToken authority.
// TestDecodeMCPToolCallParamsAcceptsStandardMetadata проверяет, что стандартная _meta
// не меняет decoded name/arguments; progressToken не интерпретируется как authority.
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

// TestDecodeMCPToolCallParamsRejectsUnknownAuthorityFields verifies an extra top-level
// actor key is rejected rather than accepted as caller-supplied owner authority.
// TestDecodeMCPToolCallParamsRejectsUnknownAuthorityFields проверяет отказ лишнего
// top-level actor key вместо его принятия за caller-supplied owner authority.
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

// TestAssistantPlanInputErrorsKeepAClosedFailureClass checks unsupported DELETE_PROJECT
// shorthand returns the typed safe operation_type input failure before any owner RPC.
// TestAssistantPlanInputErrorsKeepAClosedFailureClass проверяет typed safe operation_type
// ошибку неподдерживаемого DELETE_PROJECT shorthand до любого owner RPC.
func TestAssistantPlanInputErrorsKeepAClosedFailureClass(t *testing.T) {
	t.Parallel()
	server := &Server{}
	_, err := server.proposeAssistantPlan(t.Context(), runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem}, map[string]any{
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

// TestAssistantEnvironmentVariableNamesRejectReservedPrefixes checks reserved names
// in full/sparse/secret/removal inputs and normalizer rejection, while allowing an
// ordinary APP_ name. It does not claim every possible secret value is detected.
// TestAssistantEnvironmentVariableNamesRejectReservedPrefixes проверяет reserved names
// в full/sparse/secret/removal inputs и отказ normalizer, допуская обычное APP_ имя.
// Обнаружение любого возможного secret value этим не доказывается.
func TestAssistantEnvironmentVariableNamesRejectReservedPrefixes(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"publicValues", "publicValueUpdates", "secretBindings"} {
		input := map[string]any{field: []any{map[string]any{"name": "KODEX_TEST_MODE"}}}
		if assistantEnvironmentVariableNamesValid(input) {
			t.Fatalf("reserved variable was accepted in %s", field)
		}
	}
	if assistantEnvironmentVariableNamesValid(map[string]any{"publicValueRemovals": []any{"KODEX_TEST_MODE"}}) {
		t.Fatal("reserved removal name was accepted")
	}
	if !assistantEnvironmentVariableNamesValid(map[string]any{"publicValues": []any{map[string]any{"name": "APP_TEST_MODE"}}}) {
		t.Fatal("ordinary application variable was rejected")
	}
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"type":       "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
		"parameters": map[string]any{"name": "Test", "publicValues": []any{map[string]any{"name": "KODEX_TEST_MODE", "value": "linked-chat"}}},
	}, "Test environment", "prj_12345678", "Test")
	var inputErr *assistantPlanInputError
	if operation != nil || !errors.As(err, &inputErr) || inputErr.reason != "environment_variable_name" {
		t.Fatalf("reserved variable did not produce a safe typed error: operation=%#v err=%v", operation, err)
	}
}

// TestAssistantOperationSchemaAndParserUseSameServerHydrationRegistry checks compact
// instruction/binding/capability discovery matches UPDATE hydration and exact local
// AGENT target construction; schemas do not assign caller authority.
// TestAssistantOperationSchemaAndParserUseSameServerHydrationRegistry проверяет согласие
// compact instruction/binding/capability discovery с UPDATE hydration и exact local
// AGENT target; schemas не назначают caller authority.
func TestAssistantOperationSchemaAndParserUseSameServerHydrationRegistry(t *testing.T) {
	t.Parallel()
	input := runtimecontract.RunnerInput{
		ProjectRef: "prj_12345678",
		AssistantContext: &runtimecontract.RunnerAssistantContext{
			EntityKind: "AGENT", EntityRef: "agt_12345678", EntityName: "Analyst",
			AllowedOperations: []string{"CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "CHANGE_CAPABILITY"},
		},
	}
	for _, schema := range assistantPlanOperationSchemas(input) {
		kind := assistantSchemaType(schema)
		required := schema["required"].([]string)
		if len(required) != 4 || required[0] != "type" || required[3] != "parameters" {
			t.Fatalf("%s schema is not compact: %#v", kind, required)
		}
		if !assistantServerHydratedOperation(kind) || assistantServerAction(kind) != "UPDATE" {
			t.Fatalf("%s compact schema is not accepted by the parser", kind)
		}
		target := assistantServerTarget(kind, map[string]any{"agentRef": "agt_12345678"}, input.AssistantContext)
		if target == nil || target["kind"] != "AGENT" || target["name"] != "Analyst" {
			t.Fatalf("%s server target is unavailable: %#v", kind, target)
		}
	}
}

// TestAssistantPlanControlValidationRemainsRetryableInputError checks owner InvalidArgument
// becomes the closed retryable server_validation class while Unavailable retains
// its transport code instead of becoming a model-input verdict.
// TestAssistantPlanControlValidationRemainsRetryableInputError проверяет преобразование
// owner InvalidArgument в closed retryable server_validation, сохраняя transport code
// Unavailable вместо его превращения в model-input verdict.
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

// TestNormalizeServerHydratedAssistantOperationAcceptsBoundedModelShorthand checks
// CREATE_AGENT action shorthand, generated title/summary and snake_case alias removal
// without executing a create or resolving an employee.
// TestNormalizeServerHydratedAssistantOperationAcceptsBoundedModelShorthand проверяет
// CREATE_AGENT action shorthand, generated title/summary и удаление snake_case alias,
// не исполняя create и не разрешая employee.
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

// TestNormalizeServerHydratedAssistantOperationKeepsBoundedUnicodeSummary checks
// five hundred Cyrillic runes survive normalization and the character limit rejects
// one more, rather than treating multibyte text as an overlong byte sequence.
// TestNormalizeServerHydratedAssistantOperationKeepsBoundedUnicodeSummary проверяет
// сохранение пятисот кириллических runes и отказ ещё одного character,
// не считая multibyte text слишком длинным по byte count.
func TestNormalizeServerHydratedAssistantOperationKeepsBoundedUnicodeSummary(t *testing.T) {
	t.Parallel()
	planSummary := strings.Repeat("я", 500)
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"type":       "CREATE_PROJECT",
		"parameters": map[string]any{"name": "Проект", "purpose": "Проверка", "language": "ru"},
	}, planSummary, "", "")
	if err != nil {
		t.Fatalf("normalize unicode summary: %v", err)
	}
	if operation["summary"] != planSummary || len([]rune(operation["summary"].(string))) != 500 {
		t.Fatalf("unicode summary was not preserved within the schema limit: %#v", operation["summary"])
	}
	if !assistantPlanTextWithinLimit(operation["summary"].(string), 500) ||
		assistantPlanTextWithinLimit(strings.Repeat("я", 501), 500) {
		t.Fatal("unicode summary limit does not match the tool schema")
	}
}

// TestNormalizeServerHydratedAssistantOperationRejectsAliasCollision checks mixed
// canonical/snake_case project selectors return the closed alias-collision class
// instead of silently choosing one caller value.
// TestNormalizeServerHydratedAssistantOperationRejectsAliasCollision проверяет closed
// alias-collision class при canonical/snake_case project selectors вместо
// неявного выбора одного caller value.
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

// TestNormalizeServerHydratedAssistantOperationPinsCurrentProject checks omitted
// project ref is filled from the current server context and the resulting Russian
// title/summary explicitly describes the purpose edit; it does not persist an update.
// TestNormalizeServerHydratedAssistantOperationPinsCurrentProject проверяет заполнение
// пропущенного project ref текущим server context и явное русское описание purpose
// edit в title/summary, не сохраняя update.
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

// TestNormalizeEnvironmentDraftPinsCurrentProjectAndKeepsOnlyMetadata checks a foreign
// project locator is replaced by current context, preserves the selected image alias
// and describes an environment draft target without creating a runnable environment.
// TestNormalizeEnvironmentDraftPinsCurrentProjectAndKeepsOnlyMetadata проверяет замену
// чужого project locator текущим context, сохранение image alias и описание environment
// draft target без создания исполняемой среды.
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

// TestNormalizeRoleImageRecipePinsCurrentProjectAndAgentReference checks current
// project pinning and agent/environment-key alias preservation in a local recipe
// proposal; no image is built, admitted or published.
// TestNormalizeRoleImageRecipePinsCurrentProjectAndAgentReference проверяет current
// project pinning и сохранение agent/environment-key alias в локальном recipe
// proposal; образ не собирается, не допускается и не публикуется.
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

// TestNormalizeRoleImageUpdatePinsCurrentProjectAndRecipe checks current project
// pinning and exact recipe locator in local UPDATE normalization; it does not
// establish recipe ownership or change a persisted revision.
// TestNormalizeRoleImageUpdatePinsCurrentProjectAndRecipe проверяет current project
// pinning и exact recipe locator локального UPDATE normalizer, не устанавливая
// владение рецептом и не меняя сохранённую revision.
func TestNormalizeRoleImageUpdatePinsCurrentProjectAndRecipe(t *testing.T) {
	t.Parallel()
	operation, err := normalizeServerHydratedAssistantOperation(map[string]any{
		"type":       "UPDATE_ROLE_IMAGE_RECIPE",
		"parameters": map[string]any{"project_ref": "prj_untrusted", "recipe_ref": "imgrec_exact", "name": "New image"},
	}, "Update image", "prj_current1", "Marketplace")
	if err != nil {
		t.Fatal(err)
	}
	parameters := operation["parameters"].(map[string]any)
	if parameters["projectRef"] != "prj_current1" || parameters["recipeRef"] != "imgrec_exact" ||
		assistantServerTarget("UPDATE_ROLE_IMAGE_RECIPE", parameters, nil)["kind"] != "ROLE_IMAGE_RECIPE" {
		t.Fatalf("role image update was not server-bound: %#v", operation)
	}
}

// TestRoleImageUpdateCatalogUsesCanonicalServerAction checks SYSTEM/PROJECT create
// and update descriptors use the normalizer action and compact required envelope,
// while optional update metadata retains a complete versioned target description.
// TestRoleImageUpdateCatalogUsesCanonicalServerAction проверяет action normalizer
// и compact required envelope create/update descriptors SYSTEM/PROJECT; optional
// update metadata сохраняет полное описание versioned target.
func TestRoleImageUpdateCatalogUsesCanonicalServerAction(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input := assistantConfigurationFixture(scope)
			input.AssistantContext.AllowedOperations = []string{"CREATE_ROLE_IMAGE_RECIPE", "UPDATE_ROLE_IMAGE_RECIPE"}
			catalog, err := configurationCatalog(input, map[string]any{"operation_types": []any{"CREATE_ROLE_IMAGE_RECIPE", "UPDATE_ROLE_IMAGE_RECIPE"}})
			if err != nil {
				t.Fatal("canonical role image schema discovery failed")
			}
			schemas := catalog.(map[string]any)["operation_schemas"].([]map[string]any)
			if len(schemas) != 2 {
				t.Fatal("discovery lost the distinct create and update operations")
			}
			for _, schema := range schemas {
				kind := assistantSchemaType(schema)
				properties := schema["properties"].(map[string]any)
				if properties["action"].(map[string]any)["const"] != assistantServerAction(kind) {
					t.Fatal("discovered role image action differs from the server normalizer")
				}
				if !reflect.DeepEqual(schema["required"], []string{"type", "title", "summary", "parameters"}) {
					t.Fatal("role image discovery requires caller-owned action, target or OCC pins")
				}
				parameters := properties["parameters"].(map[string]any)
				fields := parameters["properties"].(map[string]any)
				if fields["expectedVersion"] != nil || fields["specSha256"] != nil || fields["action"] != nil {
					t.Fatal("role image parameters expose server-owned hydration fields")
				}
				if kind == "UPDATE_ROLE_IMAGE_RECIPE" {
					target := properties["target"].(map[string]any)
					if fields["recipeRef"] == nil || fields["environmentKey"] == nil || properties["expectedVersion"] == nil ||
						!reflect.DeepEqual(target["required"], []string{"kind", "name", "ref", "version"}) {
						t.Fatal("optional update envelope does not describe complete OCC target pins")
					}
				}
			}
		})
	}
}

// TestServerHydratedCatalogActionsMatchClosedRegistry checks all hydrated catalog
// descriptors share compact required fields and registered actions for both assistant
// scopes; an unknown UPDATE_ prefix does not acquire an UPDATE action.
// TestServerHydratedCatalogActionsMatchClosedRegistry проверяет общие compact required
// fields и registered actions hydrated descriptors обоих assistant scopes;
// неизвестный UPDATE_ prefix не получает UPDATE action.
func TestServerHydratedCatalogActionsMatchClosedRegistry(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input := assistantConfigurationFixture(scope)
		input.AssistantContext = nil
		input.RuntimeEnvironmentRef = "renv_fixture123"
		for _, schema := range assistantPlanOperationSchemas(input) {
			kind := assistantSchemaType(schema)
			if !assistantServerHydratedOperation(kind) {
				continue
			}
			properties := schema["properties"].(map[string]any)
			if properties["action"].(map[string]any)["const"] != assistantServerAction(kind) {
				t.Fatalf("server-hydrated descriptor action diverged for %s", kind)
			}
			if !reflect.DeepEqual(schema["required"], []string{"type", "title", "summary", "parameters"}) {
				t.Fatalf("server-hydrated descriptor requires authority fields for %s", kind)
			}
		}
		for _, kind := range assistantOperationTypes(input) {
			if kind == "UPDATE_UNKNOWN_RESOURCE" {
				t.Fatal("discovery expanded the closed operation registry")
			}
		}
	}
	if assistantServerHydratedOperation("UPDATE_UNKNOWN_RESOURCE") || assistantServerAction("UPDATE_UNKNOWN_RESOURCE") != "CREATE" {
		t.Fatal("unknown UPDATE prefix acquired a registered action")
	}
}
