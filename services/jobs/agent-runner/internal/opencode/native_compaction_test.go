package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// nativeFixture records model wire and managed tool effects; upstream OpenCode still owns the loop.
// nativeFixture фиксирует model wire и managed tool effects; циклом по-прежнему владеет upstream OpenCode.
type nativeFixture struct {
	modelDelay       time.Duration
	proxyCacheBypass bool
	cacheExpectation *nativeCacheWireExpectation
	cacheSalts       map[string]bool
	mu               sync.Mutex
	tools            int
	summaries        int
	sinceSummary     int
	modelCalls       int
	maximumWire      int
	targetTools      int
	lastWire         []byte
	violations       []string
	identityKey      string
	identityRevision string
	nativeIdentities map[string]bool
}

// mcp serves a stateless synthetic managed tool with strict ordinal effects and no arbitrary I/O.
// mcp обслуживает stateless synthetic managed tool с точными ordinal effects без произвольного I/O.
func (fixture *nativeFixture) mcp(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var call struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string `json:"protocolVersion"`
			Name            string `json:"name"`
			Arguments       struct {
				Ordinal int `json:"ordinal"`
			} `json:"arguments"`
		} `json:"params"`
	}
	raw, readErr := io.ReadAll(io.LimitReader(request.Body, 2<<20+1))
	if readErr != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &call) != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(call.Method, "notifications/") {
		writer.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch call.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": call.Params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "synthetic", "version": "1.0.0"}}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "counter", "description": "Perform the next synthetic counter step", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"ordinal": map[string]any{"type": "integer", "minimum": 1}}, "required": []string{"ordinal"}, "additionalProperties": false}}}}
	case "tools/call":
		fixture.mu.Lock()
		identityKey, identityRevision := fixture.identityKey, fixture.identityRevision
		fixture.mu.Unlock()
		if identityKey != "" {
			var envelope struct {
				Params json.RawMessage `json:"params"`
			}
			_ = json.Unmarshal(raw, &envelope)
			clean, id, err := VerifyManagedToolIdentity(envelope.Params, "kodex", identityRevision, identityKey)
			if err != nil || json.Unmarshal(clean, &call.Params) != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			fixture.mu.Lock()
			if fixture.nativeIdentities[id] {
				fixture.violations = append(fixture.violations, "native tool identity repeated")
			}
			fixture.nativeIdentities[id] = true
			fixture.mu.Unlock()
		}
		fixture.mu.Lock()
		if call.Params.Name != "counter" || call.Params.Arguments.Ordinal != fixture.tools+1 {
			fixture.violations = append(fixture.violations, "tool ordinal repeated or outside the managed catalog")
		}
		fixture.tools++
		ordinal := fixture.tools
		fixture.mu.Unlock()
		text := fmt.Sprintf("SYNTHETIC_COUNTER_%d\n%s", ordinal, strings.Repeat("bounded synthetic tool output ", 80))
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}}
	case "ping":
		result = map[string]any{}
	default:
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
}

// model serves controlled Chat Completions responses, including reported token pressure and tool calls.
// model выдаёт управляемые Chat Completions ответы, включая token pressure и tool calls.
func (fixture *nativeFixture) model(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	var input struct {
		Model    string           `json:"model"`
		Cache    map[string]bool  `json:"cache"`
		Messages []map[string]any `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err != nil || json.Unmarshal(raw, &input) != nil || input.Model != "tiny" {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	fixture.mu.Lock()
	if request.Header.Get("x-kodex-model-phase") != "" {
		fixture.violations = append(fixture.violations, "private measurement header reached provider")
	}
	if fixture.proxyCacheBypass {
		if len(input.Cache) != 2 || !input.Cache["no-cache"] || !input.Cache["no-store"] {
			fixture.violations = append(fixture.violations, "proxy response-cache read/write bypass missing")
		}
	} else if input.Cache != nil {
		fixture.violations = append(fixture.violations, "proxy-only cache controls reached direct provider")
	}
	if fixture.identityKey != "" && (strings.Contains(string(raw), "__kodex_native") || strings.Contains(string(raw), fixture.identityKey)) {
		fixture.violations = append(fixture.violations, "hook transport material reached model input")
	}
	if expected := fixture.cacheExpectation; expected != nil {
		var wire map[string]any
		if json.Unmarshal(raw, &wire) != nil {
			fixture.violations = append(fixture.violations, "invalid cache transport")
		}
		salt, _ := wire["cache_salt"].(string)
		if expected.mechanism == "NONE" {
			for _, name := range []string{"cache_salt", "prompt_cache_key", "prompt_cache_retention", "kv_transfer_params"} {
				if _, found := wire[name]; found {
					fixture.violations = append(fixture.violations, "unmanaged provider hint")
				}
			}
		} else if expected.mechanism == "VLLM_CACHE_SALT" {
			if !strings.HasPrefix(salt, expected.namespace) || (!expected.noReuse && salt != expected.namespace) || (expected.noReuse && (salt == expected.namespace || fixture.cacheSalts[salt])) {
				fixture.violations = append(fixture.violations, "salt scope or per-request nonce mismatch")
			}
			if fixture.cacheSalts == nil {
				fixture.cacheSalts = map[string]bool{}
			}
			fixture.cacheSalts[salt] = true
		}
	}
	fixture.modelCalls++
	identityMode := fixture.identityKey != ""
	call := fixture.modelCalls
	fixture.maximumWire = max(fixture.maximumWire, len(raw))
	fixture.lastWire = append(fixture.lastWire[:0], raw...)
	isSummary := strings.Contains(string(raw), "SYNTHETIC_NATIVE_COMPACTION")
	if isSummary {
		fixture.summaries++
		fixture.sinceSummary = 0
	} else {
		fixture.sinceSummary++
	}
	ordinal := fixture.tools + 1
	target := fixture.targetTools
	if target == 0 {
		target = 12
	}
	promptTokens := 4000 + fixture.sinceSummary*3200
	fixture.mu.Unlock()
	writer.Header().Set("Content-Type", "text/event-stream")
	chunk := func(delta map[string]any, finish any) {
		data := map[string]any{"id": fmt.Sprintf("synthetic-%d", call), "object": "chat.completion.chunk", "created": 1, "model": "tiny", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		raw, _ := json.Marshal(data)
		fmt.Fprintf(writer, "data: %s\n\n", raw)
	}
	chunk(map[string]any{"role": "assistant"}, nil)
	if fixture.modelDelay > 0 {
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(fixture.modelDelay)
	}
	finish := "stop"
	switch {
	case identityMode && call > 24:
		chunk(map[string]any{"content": "SYNTHETIC_IDENTITY_FAILURE"}, nil)
	case isSummary:
		chunk(map[string]any{"content": fmt.Sprintf("Native compaction checkpoint: counter completed through %d; continue the next ordinal.", ordinal-1)}, nil)
	case ordinal > target:
		chunk(map[string]any{"content": fmt.Sprintf("SYNTHETIC_FINAL_COUNTER_%d", target)}, nil)
	default:
		name := ""
		for _, tool := range input.Tools {
			if tool.Function.Name == "kodex_counter" {
				name = tool.Function.Name
			}
		}
		if name == "" {
			fixture.mu.Lock()
			fixture.violations = append(fixture.violations, "managed tool missing from actual model request")
			fixture.mu.Unlock()
			chunk(map[string]any{"content": "SYNTHETIC_MISSING_TOOL"}, nil)
			break
		}
		chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call_%d", ordinal), "type": "function", "function": map[string]string{"name": name, "arguments": fmt.Sprintf(`{"ordinal":%d}`, ordinal)}}}}, nil)
		finish = "tool_calls"
	}
	chunk(map[string]any{}, finish)
	usage, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("synthetic-%d", call), "object": "chat.completion.chunk", "created": 1, "model": "tiny", "choices": []any{}, "usage": map[string]int{"prompt_tokens": promptTokens, "completion_tokens": 24, "total_tokens": promptTokens + 24}})
	fmt.Fprintf(writer, "data: %s\n\ndata: [DONE]\n\n", usage)
}

// TestNativeCompactionContinuesManagedToolLoop exercises the real upstream loop, compactor and session DB.
// TestNativeCompactionContinuesManagedToolLoop проверяет настоящие upstream loop, compactor и session DB.
func TestNativeCompactionContinuesManagedToolLoop(t *testing.T) {
	t.Run("baseline", func(t *testing.T) { testNativeContextWindowCompaction(t, false) })
	t.Run("lower-only-overlay", func(t *testing.T) { testNativeContextWindowCompaction(t, true) })
}

// testNativeContextWindowCompaction runs the actual upstream compactor; synthetic usage is local fixture evidence only.
// testNativeContextWindowCompaction запускает actual upstream compactor; synthetic usage относится лишь к локальной оснастке.
func testNativeContextWindowCompaction(t *testing.T, lower bool) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{}
	config := nativeToolConfiguration(t, fixture)
	if lower {
		catalog := runtimecontract.ModelProviderModel{ContextWindowTokens: 32768, MaximumOutputTokens: 2048}
		resolved, err := catalog.ResolveRuntimeContext("[context]\nwindow_tokens = 16384")
		if err != nil || catalog.ContextWindowTokens != 32768 || resolved.WindowTokens != 16384 {
			t.Fatal("lower-only context resolution failed", err)
		}
		config["provider"].(map[string]any)["synthetic"].(map[string]any)["models"].(map[string]any)["tiny"].(map[string]any)["limit"] = map[string]int{"context": resolved.WindowTokens, "output": catalog.MaximumOutputTokens}
		config["compaction"] = map[string]bool{"auto": resolved.AutoCompaction, "prune": false}
	}
	server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	session, err := server.client.CreateSession(t.Context(), "Native compaction qualification")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var commentaryCount int
	result, err := server.client.Execute(ctx, session.ID, Prompt{
		MessageID: "msg_11234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
		Parts: []TextPart{{Type: "text", Text: "Perform all twelve managed counter steps and return the final marker."}},
	}, func(name string) bool { return name == "kodex_counter" }, func(_, text string) error {
		commentaryCount++
		if strings.Contains(text, "Native compaction checkpoint") {
			return ErrProtocol
		}
		return nil
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 12 || fixture.summaries < 2 || result.Compactions < 2 || len(fixture.violations) != 0 {
		t.Fatalf("native loop qualification failed: tools=%d summaries=%d compacted=%d violations=%v", fixture.tools, fixture.summaries, result.Compactions, fixture.violations)
	}
	if result.Usage.OutputTokens != int64(fixture.modelCalls*24) {
		messages, readErr := server.client.Messages(t.Context(), session.ID, 100, "")
		var storedOutput int64
		for _, message := range messages {
			for _, raw := range message.Parts {
				var part struct {
					Type   string
					Tokens nativeTokens
				}
				if json.Unmarshal(raw, &part) == nil && part.Type == "step-finish" {
					storedOutput += part.Tokens.Output
				}
			}
		}
		t.Fatalf("native event drain lost or duplicated measured model steps: observed_output=%d stored_output=%d read_failed=%t expected_output=%d model_calls=%d compactions=%d", result.Usage.OutputTokens, storedOutput, readErr != nil, fixture.modelCalls*24, fixture.modelCalls, result.Compactions)
	}
	if result.FinalText != "SYNTHETIC_FINAL_COUNTER_12" || commentaryCount == 0 || result.Usage.InputTokens == 0 {
		t.Fatal("compaction summary was mistaken for the final response")
	}
	t.Logf("forced synthetic threshold: tools=%d compactions=%d modelCalls=%d maximumWireBytes=%d", fixture.tools, result.Compactions, fixture.modelCalls, fixture.maximumWire)
}

// nativeToolConfiguration connects real OpenCode only to the bounded synthetic model and MCP servers.
// nativeToolConfiguration подключает настоящий OpenCode только к ограниченным synthetic model и MCP servers.
func nativeToolConfiguration(t *testing.T, fixture *nativeFixture) map[string]any {
	t.Helper()
	model := httptest.NewServer(http.HandlerFunc(fixture.model))
	t.Cleanup(model.Close)
	mcp := httptest.NewServer(http.HandlerFunc(fixture.mcp))
	t.Cleanup(mcp.Close)
	config := nativeConfiguration()
	provider := config["provider"].(map[string]any)["synthetic"].(map[string]any)
	provider["options"].(map[string]string)["baseURL"] = model.URL + "/v1"
	provider["models"].(map[string]any)["tiny"] = map[string]any{"limit": map[string]int{"context": 16384, "output": 2048}}
	policy, err := (runtimecontract.ModelProviderModel{ContextWindowTokens: 16384, MaximumOutputTokens: 2048}).ResolveRuntimeContext("[context]\nauto_compaction = true\nprune = false")
	if err != nil {
		t.Fatal(err)
	}
	config["compaction"] = map[string]any{"auto": policy.AutoCompaction, "prune": policy.Prune, "reserved": 2048}
	config["agent"] = map[string]any{"compaction": map[string]string{"prompt": "SYNTHETIC_NATIVE_COMPACTION. Preserve completed counter steps and the next ordinal."}}
	config["permission"] = map[string]string{"*": "deny", "kodex_*": "allow"}
	config["mcp"] = map[string]any{"kodex": map[string]any{"type": "remote", "url": mcp.URL, "enabled": true, "oauth": false}}
	return config
}
