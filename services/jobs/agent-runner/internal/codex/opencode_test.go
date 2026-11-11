package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// openCodeInputFixture constructs inert provider metadata, not an authorized live runner snapshot.
// openCodeInputFixture создаёт неактивные metadata, не авторизованный live runner snapshot.
func openCodeInputFixture() model.Input {
	return model.Input{Provider: "opencode", Model: "model-a", ReasoningMode: runtimecontract.ReasoningUnsupported, ModelProvider: &runtimecontract.ModelProviderConfiguration{
		Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40),
		Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway",
		BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.40/32"},
		Models: []runtimecontract.ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}},
	}}
}

// TestOpenCodeConfigurationKeepsCredentialsOutOfConfigAndDeniesNativeTools verifies the managed-only projection.
// TestOpenCodeConfigurationKeepsCredentialsOutOfConfigAndDeniesNativeTools проверяет managed-only projection.
func TestOpenCodeConfigurationKeepsCredentialsOutOfConfigAndDeniesNativeTools(t *testing.T) {
	input := openCodeInputFixture()
	raw, err := OpenCodeConfiguration(input, "http://127.0.0.1:12345/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("invalid generated config")
	}
	if _, boundedByHiddenSteps := config["agent"]; boundedByHiddenSteps {
		t.Fatal("managed turn acquired a hidden provider step ceiling")
	}
	permission := config["permission"].(map[string]any)
	if permission["*"] != "deny" || permission["kodex_*"] != "allow" || config["share"] != "disabled" || config["autoupdate"] != false {
		t.Fatal("managed profile enabled an unsupported native capability")
	}
	providers := config["provider"].(map[string]any)
	options := providers["test-gateway"].(map[string]any)["options"].(map[string]any)
	if options["apiKey"] != "{env:KODEX_MODEL_PROVIDER_API_KEY}" || options["baseURL"] != input.ModelProvider.BaseURL {
		t.Fatal("credential source or provider endpoint changed")
	}
	for _, endpoint := range []string{"http://127.0.0.1:12345/mcp?", "http://localhost:12345/mcp", "https://127.0.0.1:12345/mcp", "http://127.0.0.1:12345/other"} {
		if _, err := OpenCodeConfiguration(input, endpoint); err == nil {
			t.Fatal("noncanonical MCP bridge accepted")
		}
	}
}

// TestOpenCodeConfigurationRejectsCodexStateAndUnknownModels closes incompatible engine selections.
// TestOpenCodeConfigurationRejectsCodexStateAndUnknownModels закрывает несовместимые engine selections.
func TestOpenCodeConfigurationRejectsCodexStateAndUnknownModels(t *testing.T) {
	for name, mutate := range map[string]func(*model.Input){
		"old engine":  func(i *model.Input) { i.Provider = "openai" },
		"model typo":  func(i *model.Input) { i.Model = "unconfigured-model" },
		"old overlay": func(i *model.Input) { i.ConfigOverlay = "personality = \"friendly\"\n" },
	} {
		t.Run(name, func(t *testing.T) {
			input := openCodeInputFixture()
			mutate(&input)
			if _, err := OpenCodeConfiguration(input, "http://127.0.0.1:12345/mcp"); err == nil {
				t.Fatal("incompatible engine input accepted")
			}
		})
	}
}

// TestOpenCodeEnvironmentRetainsManagedTransportOnly excludes unrelated secrets/config while keeping exact proxy and CA values unique.
// TestOpenCodeEnvironmentRetainsManagedTransportOnly исключает unrelated secrets/config, сохраняя unique exact proxy и CA values.
func TestOpenCodeEnvironmentRetainsManagedTransportOnly(t *testing.T) {
	setRuntimeTransportFixture(t)
	t.Setenv("UNRELATED_ROLE_SECRET", "synthetic-unrelated")
	t.Setenv("OPENCODE_CONFIG", "/untrusted/config.json")
	env, err := openCodeEnvironment("/private/home", []byte(`{}`), "synthetic-model-key", "synthetic-mcp-key")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatal("malformed environment")
		}
		if _, exists := values[key]; exists {
			t.Fatal("duplicate environment key")
		}
		values[key] = value
	}
	if _, ok := values["UNRELATED_ROLE_SECRET"]; ok {
		t.Fatal("unrelated credential inherited")
	}
	if _, ok := values["OPENCODE_CONFIG"]; ok {
		t.Fatal("foreign configuration inherited")
	}
	if values["HTTPS_PROXY"] != syntheticRuntimeProxy || values["NODE_EXTRA_CA_CERTS"] != runtimeTrustBundle || values[openCodeAPIKeyEnvironment] != "synthetic-model-key" {
		t.Fatal("managed environment boundary changed")
	}
}

// TestOpenCodeWorkspaceIsExplicitAndKeepsNativeToolsDenied checks the separate loopback catalog,
// exact local permission names and the shared finite timeout. No SDK or live model is executed.
// TestOpenCodeWorkspaceIsExplicitAndKeepsNativeToolsDenied проверяет отдельный loopback catalog,
// exact local permission names и общий конечный timeout без запуска SDK либо живой модели.
func TestOpenCodeWorkspaceIsExplicitAndKeepsNativeToolsDenied(t *testing.T) {
	input := openCodeInputFixture()
	input.Mode = runtimecontract.RunnerModeTurn
	input.AssistantScope = runtimecontract.AssistantScopeNone
	input.ProjectRef = "prj_fixture"
	input.LeaseRef = "lea_fixture"
	input.LeaseFence = "fixture-fence"
	input.LeaseGeneration = 1
	input.InputDigest = strings.Repeat("a", 64)
	input.RuntimeRevisionDigest = strings.Repeat("b", 64)
	input.WorkspaceAccess = "workspace-write"
	input.Capabilities = []string{runtimecontract.ArtifactCapability}
	input.WorkspacePolicy = runtimecontract.RuntimeWorkspacePolicyV1()
	input.EnvironmentPolicy = runtimecontract.DefaultRuntimeEnvironmentPolicy()
	input.EnvironmentPolicy.ManagedWorkspace = true
	input.EnvironmentTools = []runtimecontract.RuntimeEnvironmentTool{{Name: "Go", Command: "go"}}
	raw, err := OpenCodeConfiguration(input, "http://127.0.0.1:12345/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("invalid config")
	}
	workspace := config["mcp"].(map[string]any)["workspace"].(map[string]any)
	if workspace["url"] != "http://127.0.0.1:12345/workspace/mcp" || workspace["timeout"] != float64(runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds*1000) {
		t.Fatal("local catalog or timeout lost")
	}
	permission := config["permission"].(map[string]any)
	if permission["*"] != "deny" || permission["workspace_*"] != nil || permission["bash"] != nil {
		t.Fatal("native/wildcard local authority enabled")
	}
	for _, name := range runtimecontract.RuntimeWorkspaceToolNames(input) {
		if permission["workspace_"+name] != "allow" || !allowedOpenCodeTool(input, "workspace_"+name) {
			t.Fatal("exact local tool missing", name)
		}
	}
	if allowedOpenCodeTool(input, "workspace_unpublished") || allowedOpenCodeTool(input, "bash") {
		t.Fatal("unpublished tool accepted")
	}
	input.EnvironmentPolicy.ManagedWorkspace = false
	if _, err := OpenCodeConfiguration(input, "http://127.0.0.1:12345/mcp"); err == nil {
		t.Fatal("unqualified command profile accepted")
	}
}

// TestOpenCodeCoordinatorKeepsInstalledToolsUnexposed preserves environment metadata while omitting
// the workspace MCP and every native shell/file permission from a delegate-only coordinator profile.
// TestOpenCodeCoordinatorKeepsInstalledToolsUnexposed сохраняет environment metadata без
// workspace MCP и любых native shell/file permissions в профиле delegate-only coordinator.
func TestOpenCodeCoordinatorKeepsInstalledToolsUnexposed(t *testing.T) {
	input := openCodeInputFixture()
	input.EnvironmentPolicy.ManagedWorkspace = true
	input.EnvironmentTools = []runtimecontract.RuntimeEnvironmentTool{{Name: "Go", Command: "go"}}
	input.Capabilities = []string{"platform.run.delegate"}
	raw, err := OpenCodeConfiguration(input, "http://127.0.0.1:12345/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("invalid coordinator configuration")
	}
	if config["mcp"].(map[string]any)["workspace"] != nil || runtimecontract.RuntimeWorkspaceCommandAllowed(input, "go") {
		t.Fatal("coordinator received a file/command catalog")
	}
	permission := config["permission"].(map[string]any)
	if permission["*"] != "deny" || permission["bash"] != nil || permission["workspace_*"] != nil {
		t.Fatal("native authority expanded")
	}
}

// TestOpenCodeReasoningProjection carries owner-validated effort into the model options without arbitrary overlay passthrough.
// TestOpenCodeReasoningProjection передаёт проверенное владельцем усилие в параметры модели без произвольного переноса overlay.
func TestOpenCodeReasoningProjection(t *testing.T) {
	for _, api := range []string{runtimecontract.ModelAPIResponses, runtimecontract.ModelAPIChatCompletions} {
		input := openCodeInputFixture()
		input.ModelProvider.API = api
		input.ConfigOverlay = "model_reasoning_effort = \"high\"\n"
		input.EffectiveReasoningEffort = "high"
		input.ReasoningMode = runtimecontract.ReasoningSupported
		raw, err := OpenCodeConfiguration(input, "http://127.0.0.1:12345/mcp")
		if err != nil {
			t.Fatal("valid reasoning profile rejected", err)
		}
		var config map[string]any
		if json.Unmarshal(raw, &config) != nil {
			t.Fatal("invalid configuration")
		}
		options := config["provider"].(map[string]any)[input.ModelProvider.ProviderID].(map[string]any)["models"].(map[string]any)[input.Model].(map[string]any)["options"].(map[string]any)
		if len(options) != 1 || options["reasoningEffort"] != "high" {
			t.Fatal("model reasoning projection changed")
		}
	}
}
