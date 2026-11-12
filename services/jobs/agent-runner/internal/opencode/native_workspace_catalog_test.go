package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/managedworkspace"
)

// TestNativeWorkspaceCatalogReachesModel qualifies production path and isolation instructions
// through real OpenCode MCP discovery and its bundled model adapter, without executing tools.
// TestNativeWorkspaceCatalogReachesModel проверяет доставку настоящих описаний путей и изоляции
// через MCP настоящего OpenCode и встроенный adapter модели, не исполняя инструменты.
func TestNativeWorkspaceCatalogReachesModel(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	input := runtimecontract.RunnerInput{
		Mode: runtimecontract.RunnerModeTurn, Provider: "opencode", WorkspaceAccess: "workspace-write",
		AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_synthetic", LeaseRef: "lea_synthetic",
		LeaseFence: "synthetic", LeaseGeneration: 1, InputDigest: strings.Repeat("a", 64), RuntimeRevisionDigest: strings.Repeat("b", 64),
		Capabilities: []string{runtimecontract.ArtifactCapability}, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(),
		EnvironmentPolicy: runtimecontract.DefaultRuntimeEnvironmentPolicy(),
		ModelProvider: &runtimecontract.ModelProviderConfiguration{
			Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40),
			Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "synthetic",
			BaseURL: "https://model.example.invalid/v1", AddressCIDRs: []string{"192.0.2.40/32"},
			Models: []runtimecontract.ModelProviderModel{{ID: "tiny", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}},
		},
	}
	input.EnvironmentPolicy.ManagedWorkspace = true
	catalog := managedworkspace.Tools(input)
	if len(catalog) != 3 {
		t.Fatal("invalid synthetic managed workspace profile")
	}
	fixture := &nativeFixture{targetTools: -1}
	mcp := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		var call struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err == nil && json.Unmarshal(raw, &call) == nil && call.Method == "tools/list" {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"tools": catalog}})
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		fixture.mcp(writer, request)
	}))
	defer mcp.Close()
	config := nativeToolConfiguration(t, fixture)
	config["mcp"] = map[string]any{"kodex": map[string]any{"type": "remote", "url": mcp.URL, "enabled": true, "oauth": false}}
	server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	session, err := server.client.CreateSession(t.Context(), "Synthetic workspace catalog")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_61234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Inspect the available tools without calling them."}}}, func(string) bool { return false }, func(string, string) error { return nil })
	if err != nil || result.FinalText != "SYNTHETIC_FINAL_COUNTER_-1" {
		t.Fatal("native catalog qualification did not complete", err)
	}
	server.stop(t)
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var wire struct {
		Tools []struct {
			Function struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Parameters  struct {
					Properties map[string]struct {
						Description string `json:"description"`
					} `json:"properties"`
				} `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.Unmarshal(fixture.lastWire, &wire) != nil || fixture.tools != 0 || fixture.modelCalls != 1 {
		t.Fatal("unexpected model calls or tool effects")
	}
	for _, descriptor := range catalog {
		found := false
		for _, tool := range wire.Tools {
			if tool.Function.Name != "kodex_"+descriptor["name"].(string) {
				continue
			}
			found = true
			path := descriptor["inputSchema"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)
			if tool.Function.Description != descriptor["description"] || tool.Function.Parameters.Properties["path"].Description != path["description"] {
				t.Fatal("model adapter lost production tool or path instructions")
			}
		}
		if !found {
			t.Fatal("production workspace tool missing from model request")
		}
	}
}
