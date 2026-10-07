package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func assistantWorkflowConfigurationFixture(t *testing.T) (runtimecontract.RunnerInput, map[string]any, *controlplanev1.AssistantConfigurationCatalogResponse) {
	t.Helper()
	input, arguments, _ := assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "WORKFLOW_CONFIGURATION")
	version := int64(7)
	input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "WORKFLOW", EntityRef: "wfl_workflow123", EntityVersion: &version, AllowedOperations: []string{"UPDATE_WORKFLOW", "CHANGE_INTEGRATION_GRANT"}}
	selector := arguments["assistant_configuration_catalog"].(map[string]any)
	delete(selector, "query")
	selector["entity_kind"], selector["entity_ref"] = "WORKFLOW", input.AssistantContext.EntityRef
	draft := assistantWorkflowDraft{Ref: "draft-workflow", Name: "Full workflow", Purpose: "Bounded review", CoordinatorAgentRef: "agt_manager123", Instructions: "Preserve exact global instructions", CompletionCriteria: "READY_FOR_HUMAN_REVIEW", VersionNumber: 1, Concurrency: 3, TimeoutSeconds: 3600, Inputs: []assistantWorkflowDraftInput{}, ResultSchema: map[string]any{}}
	steps := []any{}
	for index := 1; index <= 33; index++ {
		key := fmt.Sprintf("step-%03d", index)
		dependencies := []string{}
		if index > 1 {
			dependencies = []string{fmt.Sprintf("step-%03d", index-1)}
		}
		draft.Steps = append(draft.Steps, assistantWorkflowDraftStep{Key: key, Name: key, AgentRef: "agt_reviewer123", Instructions: "Exact semantic review", ExpectedResult: "PASS or BLOCKED", Position: int32(index), TimeoutSeconds: 900, DependsOn: dependencies, RequiredCapabilityKeys: []string{"platform.artifact.manage"}, GateDecisions: []string{}})
		steps = append(steps, map[string]any{"key": key, "name": key, "purpose": "Exact semantic review", "agentRef": "agt_reviewer123", "parallel": false, "parallelGroup": 0, "timeoutSeconds": 900, "expectedResult": "PASS or BLOCKED", "humanGate": false, "gateDecisions": []string{}, "requiredCapabilityKeys": []string{"platform.artifact.manage"}})
	}
	snapshot := map[string]any{"workflowRef": input.AssistantContext.EntityRef, "projectRef": input.ProjectRef, "name": draft.Name, "purpose": draft.Purpose, "coordinatorAgentRef": draft.CoordinatorAgentRef, "instructions": draft.Instructions, "completionCriteria": draft.CompletionCriteria, "maxConcurrency": draft.Concurrency, "timeoutSeconds": draft.TimeoutSeconds, "inputFields": []any{}, "steps": steps, "draft": draft}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// CP snapshot представляет draft как JSON object, а не Go struct с field order.
	var canonical map[string]any
	if json.Unmarshal(raw, &canonical) != nil {
		t.Fatal("invalid snapshot fixture")
	}
	raw, err = json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	response := &controlplanev1.AssistantConfigurationCatalogResponse{Kind: controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_WORKFLOW_CONFIGURATION, AssistantRef: input.AgentRef, ScopeKind: "PROJECT", OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef,
		WorkflowConfiguration: &controlplanev1.AssistantWorkflowConfiguration{WorkflowRef: input.AssistantContext.EntityRef, ProjectRef: input.ProjectRef, Version: version, ConfigurationJson: raw, ConfigurationSha256: hex.EncodeToString(hash[:])}}
	return input, arguments, response
}

func TestAssistantWorkflowConfigurationTraversesNativeMCP(t *testing.T) {
	input, arguments, response := assistantWorkflowConfigurationFixture(t)
	client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
	recorder := httptest.NewRecorder()
	server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"workflow-read"`), Params: params}, input)
	var wire struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || wire.Result.IsError || client.request == nil {
		t.Fatal("native Workflow read did not reach generated adapter")
	}
	request := client.request.GetAssistantConfigurationCatalog()
	if request.GetEntityKind() != "WORKFLOW" || request.GetEntityRef() != input.AssistantContext.EntityRef || request.GetAssistantRef() != input.AgentRef || client.request.GetLeaseRef() != input.LeaseRef {
		t.Fatal("native read lost exact target/lease")
	}
	projection := wire.Result.StructuredContent["assistant_configuration_catalog"].(map[string]any)["workflow_configuration"].(map[string]any)["configuration"].(map[string]any)
	if len(projection["steps"].([]any)) != 33 {
		t.Fatal("native wire truncated Workflow graph")
	}
}

func TestAssistantWorkflowConfigurationClosedRead(t *testing.T) {
	input, arguments, response := assistantWorkflowConfigurationFixture(t)
	selector := arguments["assistant_configuration_catalog"].(map[string]any)
	request, err := parseAssistantConfigurationCatalog(input, arguments, selector)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string), "WORKFLOW_CONFIGURATION") {
		t.Fatal("missing workflow catalog descriptor")
	}
	projection, err := castAssistantConfigurationCatalog(input, request, response)
	if err != nil {
		t.Fatal(err)
	}
	configuration := projection["workflow_configuration"].(map[string]any)["configuration"].(map[string]any)
	if len(configuration["steps"].([]any)) != 33 || len(configuration["draft"].(map[string]any)["Steps"].([]any)) != 33 {
		t.Fatal("full graph truncated")
	}
	for name, mutate := range map[string]func(*controlplanev1.AssistantConfigurationCatalogResponse){
		"foreign organization": func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.OrganizationRef = "org_foreign123" },
		"foreign project":      func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.ProjectRef = "prj_foreign123" },
		"wrong target": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.WorkflowConfiguration.WorkflowRef = "wfl_foreign123"
		},
		"stale context": func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.WorkflowConfiguration.Version++ },
		"hash mismatch": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.WorkflowConfiguration.ConfigurationSha256 = "invalid"
		},
		"mixed results": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration = &controlplanev1.AssistantCurrentConfiguration{}
		},
		"unknown proto": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.WorkflowConfiguration.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
			mutate(bad)
			if _, err := castAssistantConfigurationCatalog(input, request, bad); err == nil {
				t.Fatal("accepted unbound snapshot")
			}
		})
	}
	for _, field := range []string{"entity_kind", "entity_ref"} {
		bad := map[string]any{}
		for key, value := range selector {
			if key != field {
				bad[key] = value
			}
		}
		if _, err := parseAssistantConfigurationCatalog(input, arguments, bad); err == nil {
			t.Fatal("accepted incomplete selector pair")
		}
	}
	selector["kind"], selector["entity_kind"], selector["entity_ref"] = "RECIPIENT_INTEGRATION_GRANTS", "AGENT", "agt_reviewer123"
	if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err != nil {
		t.Fatal("closed selected-agent locator rejected")
	}
	input.AssistantContext.AllowedOperations = []string{"CHANGE_INTEGRATION_GRANT"}
	if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err == nil {
		t.Fatal("selected agent bypassed UPDATE_WORKFLOW authority")
	}
}

func TestAssistantWorkflowConfigurationRejectsUnknownJSON(t *testing.T) {
	input, arguments, response := assistantWorkflowConfigurationFixture(t)
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { m["credential"] = "not-authorized" },
		func(m map[string]any) { m["draft"].(map[string]any)["credential"] = "not-authorized" },
		func(m map[string]any) {
			m["draft"].(map[string]any)["Steps"].([]any)[0].(map[string]any)["DependsOn"] = []string{"missing-step"}
		},
		func(m map[string]any) {
			m["steps"].([]any)[0].(map[string]any)["requiredCapabilityKeys"] = []string{"not-in-draft"}
		},
	} {
		bad := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
		var snapshot map[string]any
		if json.Unmarshal(bad.WorkflowConfiguration.ConfigurationJson, &snapshot) != nil {
			t.Fatal("invalid fixture")
		}
		mutate(snapshot)
		raw, _ := json.Marshal(snapshot)
		hash := sha256.Sum256(raw)
		bad.WorkflowConfiguration.ConfigurationJson, bad.WorkflowConfiguration.ConfigurationSha256 = raw, hex.EncodeToString(hash[:])
		if _, err := castAssistantConfigurationCatalog(input, request, bad); err == nil {
			t.Fatal("accepted unknown or inconsistent workflow JSON")
		}
	}
}
