package codex

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

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
