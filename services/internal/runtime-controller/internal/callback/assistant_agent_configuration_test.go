package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func assistantAgentConfigurationFixture(t *testing.T) (runtimecontract.RunnerInput, map[string]any, *controlplanev1.AssistantConfigurationCatalogResponse) {
	t.Helper()
	input, arguments, _ := assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "AGENT_CONFIGURATION")
	version := int64(9)
	input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: "agt_recipient123", EntityVersion: &version, AllowedOperations: []string{"CREATE_INSTRUCTION_DRAFT", "UPDATE_AGENT"}}
	selector := arguments["assistant_configuration_catalog"].(map[string]any)
	delete(selector, "query")
	selector["entity_kind"], selector["entity_ref"] = "AGENT", input.AssistantContext.EntityRef
	content := strings.Repeat("Полные инструкции {{ .organization.name }} {{ .integrations.items }}\n", 650)
	digest := sha256.Sum256([]byte(content))
	instruction := map[string]any{"ref": "ins_revision123", "versionNumber": 2, "state": "PUBLISHED", "content": content, "digest": hex.EncodeToString(digest[:])}
	snapshot := map[string]any{"agentRef": input.AssistantContext.EntityRef, "projectRef": input.ProjectRef, "name": "Manager", "purpose": "Full configuration", "roleDefinitionRef": "role_manager123", "roleDefinitionName": "Manager", "roleDescription": "Coordinate exact delegation", "avatarUrl": "", "state": "READY", "enabled": true,
		"runtime": map[string]any{"key": "codex", "name": "Codex", "provider": "openai", "model": "fixture", "revision": "9"}, "publishedInstructions": instruction, "effectiveInstructions": instruction, "instructionBinding": map[string]any{"ref": "ibd_binding123", "version": 3, "revisionRef": "ins_revision123", "effective": true}}
	raw, _ := json.Marshal(snapshot)
	hash := sha256.Sum256(raw)
	return input, arguments, &controlplanev1.AssistantConfigurationCatalogResponse{Kind: controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_AGENT_CONFIGURATION, AssistantRef: input.AgentRef, ScopeKind: "PROJECT", OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef,
		AgentConfiguration: &controlplanev1.AssistantAgentConfiguration{AgentRef: input.AssistantContext.EntityRef, ProjectRef: input.ProjectRef, Version: version, ConfigurationJson: raw, ConfigurationSha256: hex.EncodeToString(hash[:])}}
}

func TestAssistantAgentConfigurationNativeFullRead(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeProject, runtimecontract.AssistantScopeSystem} {
		t.Run(string(scope), func(t *testing.T) {
			input, arguments, response := assistantAgentConfigurationFixture(t)
			input.AssistantScope = scope
			client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"agent-read"`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || wire.Result.IsError || client.request == nil {
				t.Fatal("native full agent read failed")
			}
			request := client.request
			if request.GetLeaseRef() != input.LeaseRef || request.GetFence() != input.LeaseFence || request.GetGeneration() != input.LeaseGeneration || request.GetAssistantConfigurationCatalog().GetEntityRef() != input.AssistantContext.EntityRef {
				t.Fatal("read lost exact lease/target pins")
			}
			configuration := wire.Result.StructuredContent["assistant_configuration_catalog"].(map[string]any)["agent_configuration"].(map[string]any)["configuration"].(map[string]any)
			var before map[string]any
			_ = json.Unmarshal(response.AgentConfiguration.ConfigurationJson, &before)
			if configuration["publishedInstructions"].(map[string]any)["content"] != before["publishedInstructions"].(map[string]any)["content"] || configuration["effectiveInstructions"].(map[string]any)["content"] != before["effectiveInstructions"].(map[string]any)["content"] {
				t.Fatal("native projection truncated or rendered published template")
			}
		})
	}
}

func TestAssistantAgentConfigurationClosedSelection(t *testing.T) {
	input, arguments, _ := assistantAgentConfigurationFixture(t)
	selector := arguments["assistant_configuration_catalog"].(map[string]any)
	kinds := func(input runtimecontract.RunnerInput) []string {
		return assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
	}
	if !slices.Contains(kinds(input), "AGENT_CONFIGURATION") {
		t.Fatal("missing closed agent catalog")
	}
	for name, mutate := range map[string]func(*runtimecontract.RunnerInput, map[string]any){
		"ordinary": func(i *runtimecontract.RunnerInput, _ map[string]any) {
			i.AssistantScope = runtimecontract.AssistantScopeNone
		},
		"workflow context": func(i *runtimecontract.RunnerInput, _ map[string]any) { i.AssistantContext.EntityKind = "WORKFLOW" },
		"revoked operation": func(i *runtimecontract.RunnerInput, _ map[string]any) {
			i.AssistantContext.AllowedOperations = []string{"CHANGE_INTEGRATION_GRANT"}
		},
		"foreign helper":    func(_ *runtimecontract.RunnerInput, s map[string]any) { s["assistant_ref"] = "agt_foreign123" },
		"foreign recipient": func(_ *runtimecontract.RunnerInput, s map[string]any) { s["entity_ref"] = "agt_foreign123" },
		"missing recipient": func(_ *runtimecontract.RunnerInput, s map[string]any) { delete(s, "entity_ref") },
		"wrong kind":        func(_ *runtimecontract.RunnerInput, s map[string]any) { s["entity_kind"] = "WORKFLOW" },
		"pagination":        func(_ *runtimecontract.RunnerInput, s map[string]any) { s["offset"] = 10 },
		"query":             func(_ *runtimecontract.RunnerInput, s map[string]any) { s["query"] = "Manager" },
		"account":           func(_ *runtimecontract.RunnerInput, s map[string]any) { s["account_ref"] = "acc_other123" },
		"unknown":           func(_ *runtimecontract.RunnerInput, s map[string]any) { s["secret"] = "not-authorized" },
	} {
		t.Run(name, func(t *testing.T) {
			i := input
			context := *input.AssistantContext
			i.AssistantContext = &context
			s := map[string]any{}
			for k, v := range selector {
				s[k] = v
			}
			mutate(&i, s)
			if _, err := parseAssistantConfigurationCatalog(i, arguments, s); err == nil {
				t.Fatal("accepted out-of-context catalog")
			}
			if !assistantAgentConfigurationAvailable(i) && slices.Contains(kinds(i), "AGENT_CONFIGURATION") {
				t.Fatal("catalog exposed without immutable eligibility")
			}
		})
	}
	selector["kind"], selector["assistant_ref"] = "CURRENT_CONFIGURATION", input.AssistantContext.EntityRef
	delete(selector, "entity_kind")
	delete(selector, "entity_ref")
	if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err == nil {
		t.Fatal("CURRENT_CONFIGURATION stopped being own-only")
	}
}

func TestAssistantAgentConfigurationRejectsCorruptionAndMixedFields(t *testing.T) {
	input, arguments, response := assistantAgentConfigurationFixture(t)
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*controlplanev1.AssistantConfigurationCatalogResponse){
		"organization": func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.OrganizationRef = "org_other123" },
		"project":      func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.ProjectRef = "prj_other123" },
		"target": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.AgentConfiguration.AgentRef = "agt_other123"
		},
		"version": func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.AgentConfiguration.Version++ },
		"digest": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.AgentConfiguration.ConfigurationSha256 = strings.Repeat("0", 64)
		},
		"mixed": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.WorkflowConfiguration = &controlplanev1.AssistantWorkflowConfiguration{}
		},
		"unknown proto": func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.AgentConfiguration.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
			mutate(bad)
			if _, err := castAssistantConfigurationCatalog(input, request, bad); err == nil {
				t.Fatal("accepted unbound agent snapshot")
			}
		})
	}
	for _, field := range []string{"credential", "instruction digest", "binding revision", "missing field", "duplicate"} {
		t.Run(field, func(t *testing.T) {
			bad := proto.Clone(response).(*controlplanev1.AssistantConfigurationCatalogResponse)
			var snapshot map[string]any
			_ = json.Unmarshal(bad.AgentConfiguration.ConfigurationJson, &snapshot)
			switch field {
			case "credential":
				snapshot["runtime"].(map[string]any)["credential"] = "not-authorized"
			case "instruction digest":
				snapshot["publishedInstructions"].(map[string]any)["digest"] = strings.Repeat("0", 64)
			case "binding revision":
				snapshot["instructionBinding"].(map[string]any)["revisionRef"] = "ins_other123"
			case "missing field":
				delete(snapshot, "roleDescription")
			}
			raw, _ := json.Marshal(snapshot)
			if field == "duplicate" {
				raw = append([]byte(`{"name":"hidden",`), raw[1:]...)
			}
			hash := sha256.Sum256(raw)
			bad.AgentConfiguration.ConfigurationJson = raw
			bad.AgentConfiguration.ConfigurationSha256 = hex.EncodeToString(hash[:])
			if _, err := castAssistantConfigurationCatalog(input, request, bad); err == nil {
				t.Fatal("accepted corrupt or private agent fields")
			}
		})
	}
}

func TestAssistantAgentConfigurationManagedTemplateSelection(t *testing.T) {
	input, arguments, response := assistantAgentConfigurationFixture(t)
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	_ = json.Unmarshal(response.AgentConfiguration.ConfigurationJson, &snapshot)
	snapshot["instructionBinding"].(map[string]any)["effective"] = false
	snapshot["effectiveInstructions"].(map[string]any)["ref"] = "mcr_template123"
	content := strings.Repeat("x", 128<<10)
	contentDigest := sha256.Sum256([]byte(content))
	snapshot["effectiveInstructions"].(map[string]any)["content"] = content
	snapshot["effectiveInstructions"].(map[string]any)["digest"] = hex.EncodeToString(contentDigest[:])
	raw, _ := json.Marshal(snapshot)
	digest := sha256.Sum256(raw)
	response.AgentConfiguration.ConfigurationJson = raw
	response.AgentConfiguration.ConfigurationSha256 = hex.EncodeToString(digest[:])
	if _, err := castAssistantConfigurationCatalog(input, request, response); err != nil {
		t.Fatal("canonical managed template rejected", err)
	}
}
