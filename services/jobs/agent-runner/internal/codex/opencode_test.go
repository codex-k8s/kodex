package codex

import (
	"bytes"
	"encoding/json"
	"errors"
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

// openCodeWireFixture encodes a bounded synthetic CLI transcript with no provider or environment data.
// openCodeWireFixture кодирует bounded synthetic CLI transcript без provider/environment data.
func openCodeWireFixture(t *testing.T, events ...map[string]any) *bytes.Reader {
	t.Helper()
	var buffer bytes.Buffer
	for _, event := range events {
		if event["sessionID"] == nil {
			event["sessionID"] = "ses_syntheticfixture0001"
		}
		if err := json.NewEncoder(&buffer).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	return bytes.NewReader(buffer.Bytes())
}

// openCodeFinishFixture returns explicit normalized counters whose provenance remains partial.
// openCodeFinishFixture возвращает explicit normalized counters с частичной полнотой provenance.
func openCodeFinishFixture(id, reason string) map[string]any {
	return map[string]any{"type": "step_finish", "part": map[string]any{"id": id, "type": "step-finish", "reason": reason,
		"tokens": map[string]any{"input": 10, "output": 5, "reasoning": 2, "cache": map[string]any{"read": 3, "write": 1}}}}
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
		"old session": func(i *model.Input) { i.CodexSessionID = "00000000-0000-4000-8000-000000000001" },
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

// TestOpenCodeStreamPublishesFinalAndPreservesPartialUsage binds published output without fake Codex archives.
// TestOpenCodeStreamPublishesFinalAndPreservesPartialUsage связывает output без поддельных Codex archives.
func TestOpenCodeStreamPublishesFinalAndPreservesPartialUsage(t *testing.T) {
	reader := openCodeWireFixture(t,
		map[string]any{"type": "step_start", "part": map[string]any{"id": "start-1", "type": "step-start"}},
		map[string]any{"type": "text", "part": map[string]any{"id": "text-1", "type": "text", "text": "Result synthetic-secret"}},
		openCodeFinishFixture("finish-1", "stop"))
	var messages []runtimecontract.RuntimeAgentMessage
	result, err := readOpenCodeTurn(reader, openCodeInputFixture(), func(activity runtimecontract.RuntimeActivity) error {
		if activity.Message != nil {
			messages = append(messages, *activity.Message)
		}
		return nil
	}, []string{"synthetic-secret"})
	if err != nil || result.Outcome != "SUCCEEDED" || result.FinalMessage != "Result [REDACTED]" {
		t.Fatal("valid terminal turn rejected", err)
	}
	if len(messages) != 1 || messages[0].Phase != runtimecontract.RuntimeMessageFinal || messages[0].Text != result.FinalMessage {
		t.Fatal("published final message differs from completion")
	}
	if result.Usage.InputTokens != 14 || result.Usage.OutputTokens != 7 || result.Usage.TotalTokens != 21 || result.UsageCompleteness != runtimecontract.UsagePartial {
		t.Fatal("normalized usage was lost or declared complete")
	}
	if result.SessionID != "" || result.ArchiveRelativePath != "" {
		t.Fatal("adapter fabricated Codex provider state")
	}
}

// TestOpenCodeStreamRejectsNativeTools prevents a provider-native tool event from succeeding silently.
// TestOpenCodeStreamRejectsNativeTools запрещает неявный успех provider-native tool event.
func TestOpenCodeStreamRejectsNativeTools(t *testing.T) {
	reader := openCodeWireFixture(t,
		map[string]any{"type": "step_start", "part": map[string]any{"id": "start-1", "type": "step-start"}},
		map[string]any{"type": "tool_use", "part": map[string]any{"id": "tool-1", "type": "tool", "tool": "bash", "state": map[string]any{"status": "completed"}}},
		openCodeFinishFixture("finish-1", "stop"))
	result, err := readOpenCodeTurn(reader, openCodeInputFixture(), func(runtimecontract.RuntimeActivity) error { return nil }, nil)
	if err == nil || result.Outcome == "SUCCEEDED" {
		t.Fatal("native tool escaped the managed profile")
	}
}

// TestOpenCodeStreamRequiresTerminalCompletion rejects truncation, provider errors and session mixing.
// TestOpenCodeStreamRequiresTerminalCompletion отклоняет обрыв, provider errors и смешение сессий.
func TestOpenCodeStreamRequiresTerminalCompletion(t *testing.T) {
	for name, events := range map[string][]map[string]any{
		"truncated": {{"type": "step_start", "part": map[string]any{"id": "start-1", "type": "step-start"}}},
		"error":     {{"type": "error", "error": map[string]any{"message": "synthetic"}}},
		"mixed sessions": {
			{"type": "step_start", "part": map[string]any{"id": "start-1", "type": "step-start"}},
			{"type": "text", "sessionID": "ses_otherfixture0001", "part": map[string]any{"id": "text-1", "type": "text", "text": "unexpected"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := readOpenCodeTurn(openCodeWireFixture(t, events...), openCodeInputFixture(), func(runtimecontract.RuntimeActivity) error { return nil }, nil)
			if err == nil || result.Outcome == "SUCCEEDED" {
				t.Fatal("incomplete or mixed provider stream accepted")
			}
		})
	}
}

// TestOpenCodeManagedToolTurnContinuesToFinal accepts completed gateway tools followed by a final assistant step.
// TestOpenCodeManagedToolTurnContinuesToFinal принимает completed gateway tools и последующий final assistant step.
func TestOpenCodeManagedToolTurnContinuesToFinal(t *testing.T) {
	reader := openCodeWireFixture(t,
		map[string]any{"type": "step_start", "part": map[string]any{"id": "start-1", "type": "step-start"}},
		map[string]any{"type": "tool_use", "part": map[string]any{"id": "tool-1", "type": "tool", "tool": "kodex_echo", "state": map[string]any{"status": "completed", "input": map[string]string{"value": "SYNTHETIC_OK"}, "output": "SYNTHETIC_OK"}}},
		openCodeFinishFixture("finish-1", "tool-calls"),
		map[string]any{"type": "step_start", "part": map[string]any{"id": "start-2", "type": "step-start"}},
		map[string]any{"type": "text", "part": map[string]any{"id": "text-2", "type": "text", "text": "SYNTHETIC_OK"}},
		openCodeFinishFixture("finish-2", "stop"))
	result, err := readOpenCodeTurn(reader, openCodeInputFixture(), func(runtimecontract.RuntimeActivity) error { return nil }, nil)
	if err != nil || result.Outcome != "SUCCEEDED" || result.SessionID != "" {
		t.Fatal("managed tool continuation failed")
	}
}

// TestOpenCodeAuthenticationFailureNeedsNoNativeSession classifies early key rejection without creating resume state.
// TestOpenCodeAuthenticationFailureNeedsNoNativeSession классифицирует ранний отказ ключа без создания resume state.
func TestOpenCodeAuthenticationFailureNeedsNoNativeSession(t *testing.T) {
	reader := strings.NewReader(`{"type":"error","error":{"data":{"statusCode":401}}}` + "\n")
	_, err := readOpenCodeTurn(reader, openCodeInputFixture(), func(runtimecontract.RuntimeActivity) error { return nil }, nil)
	if !errors.Is(err, ErrProviderAuthentication) {
		t.Fatal("early rejected key lost authentication classification")
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
	input.CodexSandbox = "workspace-write"
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
