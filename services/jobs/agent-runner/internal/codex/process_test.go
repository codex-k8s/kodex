package codex

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestTurnStartPinsModelReasoningAndPersonalityOnEveryAttempt(t *testing.T) {
	for _, session := range []string{"", "existing-thread"} {
		input := model.Input{Model: "gpt-6-astra", CodexSessionID: session, WorkspaceRoot: "/workspace",
			CodexApprovalPolicy: "never", ConfigOverlay: "model_reasoning_effort = \"high\"\npersonality = \"pragmatic\"\n"}
		params, err := turnStartParams(input, "exact-thread", []byte("exact server prompt"))
		if err != nil || params["model"] != "gpt-6-astra" || params["effort"] != "high" ||
			params["personality"] != "pragmatic" || params["threadId"] != "exact-thread" {
			t.Fatalf("turn parameters = %#v, error = %v", params, err)
		}
	}
}

func TestExecuteLocalRejectsUnknownSelectionBeforeProcessOrCredentialAccess(t *testing.T) {
	for _, input := range []model.Input{
		{Provider: "openai", Model: "unknown"},
		{Provider: "openai", Model: "gpt-6-astra", ConfigOverlay: "unknown = true"},
		{Provider: "openai", Model: "gpt-6-astra", ConfigOverlay: "model_reasoning_effort = \"none\""},
		{Provider: "openai", Model: "gpt-6-astra", EnvironmentTools: []runtimecontract.RuntimeEnvironmentTool{{Command: "missing-kodex-tool"}}},
		{Provider: "openai", Model: "gpt-6-astra", ConfigOverlay: "[mcp_servers.foreign]\nurl = \"https://example.invalid\""},
	} {
		if _, err := executeLocal(context.Background(), input, []byte("task"), ""); !errors.Is(err, ErrRuntimeProfile) {
			t.Fatalf("selection reached credential/process boundary: %v", err)
		}
	}
}

func TestProtocolErrorReportsOnlyMethodAndCode(t *testing.T) {
	t.Parallel()
	err := protocolError("turn/start", json.RawMessage(`{"code":-32602,"message":"secret diagnostic"}`))
	if err == nil || err.Error() != "Codex app-server returned a protocol error for turn/start (code -32602)" {
		t.Fatalf("protocol error = %v", err)
	}
	if strings.Contains(err.Error(), "secret diagnostic") {
		t.Fatal("protocol error exposed the upstream diagnostic")
	}
}

func TestClassifyAccountReadResponse(t *testing.T) {
	t.Parallel()

	availabilityErr := errors.New("Codex app-server is unavailable")
	protocolErr := protocolError("account/read", json.RawMessage(`{"code":-32603,"message":"internal"}`))
	tests := []struct {
		name     string
		raw      json.RawMessage
		callErr  error
		wantErr  bool
		wantAuth bool
	}{
		{name: "explicit authentication required", raw: json.RawMessage(`{"account":null,"requiresOpenaiAuth":true}`), wantErr: true, wantAuth: true},
		{name: "API key account", raw: json.RawMessage(`{"account":{"type":"apiKey"},"requiresOpenaiAuth":true}`)},
		{name: "ChatGPT account", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null,"planType":"pro"},"requiresOpenaiAuth":true}`)},
		{name: "external Bedrock account", raw: json.RawMessage(`{"account":{"type":"amazonBedrock","usesCodexManagedCredentials":false},"requiresOpenaiAuth":false}`)},
		{name: "provider without OpenAI account", raw: json.RawMessage(`{"requiresOpenaiAuth":false}`)},
		{name: "transport unavailable", callErr: availabilityErr, wantErr: true},
		{name: "protocol failure", callErr: protocolErr, wantErr: true},
		{name: "invalid top-level schema", raw: json.RawMessage(`{"account":null,"requiresOpenaiAuth":"true"}`), wantErr: true},
		{name: "invalid account schema", raw: json.RawMessage(`{"account":{"type":"chatgpt","email":null},"requiresOpenaiAuth":false}`), wantErr: true},
		{name: "unknown account field", raw: json.RawMessage(`{"account":{"type":"apiKey","token":"hidden"},"requiresOpenaiAuth":false}`), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := classifyAccountReadResponse(test.raw, test.callErr)
			if (err != nil) != test.wantErr {
				t.Fatalf("classifyAccountReadResponse() error = %v, wantErr %v", err, test.wantErr)
			}
			if got := errors.Is(err, ErrProviderAuthentication); got != test.wantAuth {
				t.Fatalf("errors.Is(error, ErrProviderAuthentication) = %v, want %v; error = %v", got, test.wantAuth, err)
			}
		})
	}
}

func TestAppServerEnvironmentPreservesOnlyRequiredEgressProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://egress-gateway:8080")
	t.Setenv("HTTPS_PROXY", "http://egress-gateway:8080")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	t.Setenv("UNRELATED_SECRET", "must-not-be-forwarded")

	environment := appServerEnvironment(model.Input{CodexHome: "/workspace/.kodex"}, "mcp-token")
	for _, expected := range []string{
		"HTTP_PROXY=http://egress-gateway:8080",
		"HTTPS_PROXY=http://egress-gateway:8080",
		"NO_PROXY=127.0.0.1,localhost",
	} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("app-server environment does not contain %q: %#v", expected, environment)
		}
	}
	if slices.Contains(environment, "UNRELATED_SECRET=must-not-be-forwarded") {
		t.Fatalf("unrelated process environment was forwarded: %#v", environment)
	}
}

func TestTokenUsageNotificationRemainsEnabled(t *testing.T) {
	t.Parallel()
	for _, method := range suppressedNotificationMethods {
		if method == "thread/tokenUsage/updated" {
			t.Fatal("token usage notification is required for authoritative per-turn accounting")
		}
	}
}

func TestRequiredMCPToolNamesMatchRuntimeAuthority(t *testing.T) {
	input := model.Input{SystemAssistant: true}
	input.DelegationTargets = append(input.DelegationTargets, runtimecontract.RunnerDelegationTarget{})
	input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{})
	actual := RequiredMCPToolNames(input)
	expected := []string{
		"propose_run_metadata",
		"get_configuration_catalog",
		"propose_configuration_plan",
		"propose_assistant_metadata",
		"delegate_agent",
		"invoke_integration",
	}
	if !sameStringSet(actual, expected) {
		t.Fatalf("required MCP tools = %#v, want %#v", actual, expected)
	}
}

func TestTrustedMCPToolApproval(t *testing.T) {
	state := &protocolState{threadID: "thread-1", turnID: "turn-1"}
	request := map[string]any{
		"threadId":   "thread-1",
		"turnId":     "turn-1",
		"serverName": "kodex",
		"mode":       "form",
		"message":    "Allow this MCP tool call?",
		"requestedSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := trustedMCPToolApproval(state, raw)
	if err != nil {
		t.Fatalf("approve trusted MCP tool: %v", err)
	}
	if response["action"] != "accept" {
		t.Fatalf("action = %#v", response["action"])
	}
	content, ok := response["content"].(map[string]any)
	if !ok || len(content) != 0 {
		t.Fatalf("content = %#v", response["content"])
	}
}

func TestTrustedMCPToolApprovalRejectsAuthorityExpansion(t *testing.T) {
	state := &protocolState{threadID: "thread-1", turnID: "turn-1"}
	tests := map[string]map[string]any{
		"foreign server": {
			"threadId": "thread-1", "turnId": "turn-1", "serverName": "external", "mode": "form",
			"message": "approve", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
		},
		"foreign turn": {
			"threadId": "thread-1", "turnId": "turn-2", "serverName": "kodex", "mode": "form",
			"message": "approve", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
		},
		"user input form": {
			"threadId": "thread-1", "turnId": "turn-1", "serverName": "kodex", "mode": "form",
			"message": "provide input", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}},
			"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"},
		},
		"ordinary elicitation": {
			"threadId": "thread-1", "turnId": "turn-1", "serverName": "kodex", "mode": "form",
			"message": "provide input", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta": map[string]any{},
		},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := trustedMCPToolApproval(state, raw); err == nil {
				t.Fatal("authority expansion was accepted")
			}
		})
	}
}
