package callback

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func assistantOwnCurrentFixture(scope runtimecontract.AssistantScope) (runtimecontract.RunnerInput, map[string]any, *controlplanev1.SearchAssistantResourcesResponse) {
	input, _, response := assistantFreshCatalogFixture(scope, "CURRENT_CONFIGURATION")
	input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef, input.Attempt = "run_owned123", "node_owned123", "ses_owned123", "turn_owned123", 2
	input.RuntimeRevisionRef, input.RuntimeRevisionVersion, input.RuntimeRevisionDigest = "rev_pinned123", 3, strings.Repeat("b", 64)
	input.RuntimeConfigRef, input.RuntimeConfigVersion, input.RuntimeConfigDigest = "cfg_previous123", 2, strings.Repeat("b", 64)
	input.ImageManifestDigest = "sha256:" + strings.Repeat("b", 64)
	input.ImageReference = "pull.fixture.invalid/assistant@" + input.ImageManifestDigest
	input.WorkspacePolicy = runtimecontract.RuntimeWorkspacePolicyV1()
	input.ProviderCredentialSHA256 = "PRIVATE_CREDENTIAL_SENTINEL"
	input.Instructions, input.Task = "PRIVATE_EXECUTION_SENTINEL", "PRIVATE_TASK_SENTINEL"
	digest := strings.Repeat("a", 64)
	current := &controlplanev1.AssistantCurrentConfiguration{AgentVersion: 7,
		ImageToolInventory: &controlplanev1.ImageToolInventory{Status: "UNAVAILABLE"},
		Configuration: &controlplanev1.AgentRuntimeConfiguration{Ref: "cfg_current123", Version: 4, AgentRef: input.AgentRef, RuntimeProfileRef: input.RuntimeProfileRef,
			Provider: "openai", Model: "gpt-6.1-sol", Digest: digest, ProviderPolicy: &controlplanev1.ProviderAccountPolicyVersion{Ref: "pol_current123", Version: 3, Mode: "FIXED", Digest: digest}},
		PublishedOverlay:   &controlplanev1.ConfigOverlayVersion{Ref: "ovr_current123", Version: 5, Revision: 5, State: "PUBLISHED", Content: "model_reasoning_effort = \"high\"", Digest: digest},
		EnvironmentBinding: &controlplanev1.AgentRuntimeEnvironmentBinding{Ref: "bnd_current123", AgentRef: input.AgentRef, Version: 6, EnvironmentRef: "env_current123", VersionRef: "envv_current123", Digest: digest},
		EnvironmentRef:     "env_current123", EnvironmentVersion: 8, EnvironmentVersionRef: "envv_current123", EnvironmentRevision: 9, EnvironmentDigest: digest,
		Image:          &controlplanev1.RuntimeEnvironmentImage{Reference: "pull.fixture.invalid/assistant@sha256:" + digest, Digest: "sha256:" + digest},
		Tools:          []*controlplanev1.RuntimeEnvironmentTool{{Name: "go", Command: "go", Description: "Compile Go", UsageHint: "Use bounded local builds"}},
		Values:         []*controlplanev1.RuntimeEnvironmentValue{{Name: "PUBLIC_SETTING", Value: "public-value"}},
		SecretBindings: []*controlplanev1.RuntimeSecretBinding{{Name: "CREDENTIAL", SecretRef: "sec_current123", Revision: 4}},
		Policy: &controlplanev1.RuntimeEnvironmentPolicy{Resources: &controlplanev1.RuntimeResourcePolicy{CpuRequestMilli: 1000, CpuLimitMilli: 2000},
			Network: &controlplanev1.RuntimeNetworkPolicy{DenyByDefault: true}, KubernetesAccess: &controlplanev1.RuntimeKubernetesAccessProfile{Kind: controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE}},
		InstructionTemplateRef: "tpl_current123", InstructionTemplateDigest: digest, PublishedInstructions: "{{ .agent.name }} full template",
		TemplateVariables: []*controlplanev1.TemplateVariable{{Name: "agent.name", ValueType: "string", Available: true, Reason: controlplanev1.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE},
			{Name: "task", ValueType: "string", Reason: controlplanev1.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_RUNTIME_CONTEXT_REQUIRED}}}
	if scope == runtimecontract.AssistantScopeSystem {
		current.SystemCoreRevision, current.SystemCoreInstructions, current.OwnerInstructions, current.OwnerInstructionsRevision = "system-assistant-core-v45", "Versioned core", "Owner policy", 11
	}
	response.AssistantConfigurationCatalog.CurrentConfiguration, response.AssistantConfigurationCatalog.Entries = current, nil
	return input, map[string]any{"assistant_configuration_catalog": map[string]any{"kind": "CURRENT_CONFIGURATION", "assistant_ref": input.AgentRef}}, response
}

func TestAssistantOwnCurrentConfigurationTraversesMCPWithoutAuthorityOrHistoryLeak(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, arguments, response := assistantOwnCurrentFixture(scope)
			client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: response}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"own-current"`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || wire.Result.IsError || client.request == nil || client.projection == nil {
				t.Fatal("own typed configuration did not traverse admitted MCP/RPC path")
			}
			catalog := wire.Result.StructuredContent["assistant_configuration_catalog"].(map[string]any)
			fresh := catalog["current_configuration"].(map[string]any)
			pinned := catalog["execution_snapshot"].(map[string]any)
			if fresh["configuration"].(map[string]any)["ref"] != "cfg_current123" || pinned["runtime_config_ref"] != "cfg_previous123" ||
				pinned["attempt"] != float64(2) || pinned["runtime_revision_ref"] != input.RuntimeRevisionRef || pinned["image_reference"] != input.ImageReference ||
				len(fresh["template_variables"].([]any)) != 2 || fresh["published_instructions"] == "" {
				t.Fatal("fresh current settings were conflated with immutable turn pins")
			}
			encoded := recorder.Body.String()
			for _, private := range []string{input.LeaseFence, input.ProviderCredentialSHA256, input.Instructions, input.Task, "secret_descriptors", "secret_uid", "content_sha256"} {
				if strings.Contains(encoded, private) {
					t.Fatal("own read leaked private execution or credential metadata")
				}
			}
			parameters := client.projection.GetSafeParameters().AsMap()
			if len(parameters) != 1 || parameters["catalogKind"] != "CURRENT_CONFIGURATION" || strings.Contains(client.projection.GetSafeResult(), "public-value") {
				t.Fatal("full configuration escaped into persistent tool history")
			}
		})
	}
}

func TestAssistantOwnCurrentConfigurationDoesNotInventBootstrapImageBinding(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input, arguments, response := assistantOwnCurrentFixture(scope)
		response.AssistantConfigurationCatalog.CurrentConfiguration.Image = &controlplanev1.RuntimeEnvironmentImage{}
		request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
		if err != nil {
			t.Fatal(err)
		}
		result, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog)
		if (err == nil) != (scope == runtimecontract.AssistantScopeSystem) {
			t.Fatal("unset current image changed canonical bootstrap eligibility")
		}
		if err == nil {
			current := result["current_configuration"].(map[string]any)
			if current["image"].(map[string]any)["reference"] != "" || result["execution_snapshot"].(map[string]any)["image_reference"] != input.ImageReference {
				t.Fatal("immutable actual image was substituted into unset fresh binding")
			}
		}
		response.AssistantConfigurationCatalog.CurrentConfiguration.Image.Digest = input.ImageManifestDigest
		if _, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog); err == nil {
			t.Fatal("partial image identity was accepted as unset")
		}
	}
}

func TestAssistantImageToolInventoryRemainsSeparateFromCapabilities(t *testing.T) {
	digest := strings.Repeat("a", 64)
	image := &controlplanev1.RuntimeEnvironmentImage{ArtifactRef: "imgart_owned123", Digest: "sha256:" + digest}
	value := &controlplanev1.ImageToolInventory{Status: "VERIFIED", Sha256: digest, ImageDigest: image.Digest, ProvenanceSha256: digest,
		Platforms: []*controlplanev1.ImagePlatformToolInventory{{Platform: "linux/amd64", PlatformDigest: image.Digest, ManifestSha256: digest}}}
	for _, probe := range runtimecontract.ImageToolProbes() {
		value.Platforms[0].Tools = append(value.Platforms[0].Tools, &controlplanev1.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	if !validAssistantImageToolInventory(value, image) {
		t.Fatal("truthful observed missing tools rejected")
	}
	value.Platforms[0].Tools[0].Status = "VERIFIED"
	if validAssistantImageToolInventory(value, image) {
		t.Fatal("capability or declaration faked verified binary")
	}
	value.Platforms[0].Tools[0].Status = "MISSING"
	value.ImageDigest = "sha256:" + strings.Repeat("b", 64)
	if validAssistantImageToolInventory(value, image) {
		t.Fatal("foreign image inventory accepted")
	}
	if validAssistantImageToolInventory(nil, image) {
		t.Fatal("missing new typed DTO accepted")
	}
	if !validAssistantImageToolInventory(&controlplanev1.ImageToolInventory{Status: "UNAVAILABLE"}, image) {
		t.Fatal("historical unavailable inventory invented")
	}
}

func TestAssistantOwnCurrentConfigurationRejectsUnboundOrCrossTargetSelectors(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*runtimecontract.RunnerInput, map[string]any)
	}{
		{"other target", func(_ *runtimecontract.RunnerInput, s map[string]any) { s["assistant_ref"] = "agt_foreign123" }},
		{"query", func(_ *runtimecontract.RunnerInput, s map[string]any) { s["query"] = "filter" }},
		{"offset", func(_ *runtimecontract.RunnerInput, s map[string]any) { s["offset"] = 1 }},
		{"missing lease", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.LeaseRef = "" }},
		{"missing revision", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.RuntimeRevisionDigest = "" }},
		{"missing attempt", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.Attempt = 0 }},
		{"missing turn", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.TurnRef = "" }},
		{"authority payload", func(_ *runtimecontract.RunnerInput, s map[string]any) { s["owner_ref"] = "owner_fake123" }},
	} {
		for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
			t.Run(string(scope)+"/"+mutate.name, func(t *testing.T) {
				input, arguments, response := assistantOwnCurrentFixture(scope)
				mutate.apply(&input, arguments["assistant_configuration_catalog"].(map[string]any))
				client := &assistantDefinitionCatalogClient{response: response}
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
				if _, err := server.configurationCatalog(t.Context(), input, arguments); err == nil || client.request != nil {
					t.Fatal("invalid own current selector reached RPC")
				}
			})
		}
	}
}

func TestAssistantOwnCurrentConfigurationRejectsMalformedTypedResponse(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*controlplanev1.AssistantConfigurationCatalogResponse)
	}{
		{"missing", func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.CurrentConfiguration = nil }},
		{"foreign organization", func(r *controlplanev1.AssistantConfigurationCatalogResponse) { r.OrganizationRef = "org_foreign123" }},
		{"foreign agent", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.Configuration.AgentRef = "agt_foreign123"
		}},
		{"foreign binding", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.EnvironmentBinding.VersionRef = "envv_foreign123"
		}},
		{"unsigned revision", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.EnvironmentDigest = "invalid"
		}},
		{"unknown nested", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.Policy.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		}},
		{"unknown enum", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.TemplateVariables[0].Reason = 999
		}},
		{"negative version", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.AgentVersion = -1
		}},
		{"imprecise version", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.AgentVersion = 9007199254740992
		}},
		{"duplicate variables", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.TemplateVariables = append(r.CurrentConfiguration.TemplateVariables, proto.Clone(r.CurrentConfiguration.TemplateVariables[0]).(*controlplanev1.TemplateVariable))
		}},
		{"mixed entries", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.Entries = []*controlplanev1.AssistantConfigurationCatalogEntry{{Ref: "agt_other123"}}
		}},
		{"too large text", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			r.CurrentConfiguration.PublishedInstructions = strings.Repeat("x", 128<<10+1)
		}},
		{"too many tools", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			for range 129 {
				r.CurrentConfiguration.Tools = append(r.CurrentConfiguration.Tools, &controlplanev1.RuntimeEnvironmentTool{Name: "go"})
			}
		}},
		{"aggregate bound", func(r *controlplanev1.AssistantConfigurationCatalogResponse) {
			for range 100 {
				r.CurrentConfiguration.Tools = append(r.CurrentConfiguration.Tools, &controlplanev1.RuntimeEnvironmentTool{Name: "go", Description: strings.Repeat("x", 12000)})
			}
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			input, arguments, response := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
			mutate.apply(response.AssistantConfigurationCatalog)
			client := &assistantDefinitionCatalogClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			if _, err := server.configurationCatalog(t.Context(), input, arguments); err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("malformed current response crossed typed boundary or leaked diagnostics")
			}
		})
	}
}
