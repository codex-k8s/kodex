package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeZaiCodingReasoningToolContinuation qualifies both exact model aliases against the pinned real SDK and native loop, replacing only the remote model and managed tool; it does not verify a live subscription.
// TestNativeZaiCodingReasoningToolContinuation проверяет оба точных model aliases настоящими закреплёнными SDK и native loop, подменяя только удалённую модель и managed tool; живая подписка не проверяется.
func TestNativeZaiCodingReasoningToolContinuation(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	for _, model := range []string{"glm-5.3", "glm-5.3-flash"} {
		t.Run(model, func(t *testing.T) {
			fixture := &nativeFixture{}
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
				var body map[string]any
				if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &body) != nil || r.URL.Path != "/api/coding/paas/v4/chat/completions" || r.Method != "POST" || body["model"] != model || r.Header.Get("Authorization") != "Bearer synthetic-not-a-credential" {
					t.Error("coding endpoint, model or authentication changed")
					w.WriteHeader(400)
					return
				}
				for _, field := range []string{"cache", "cache_control", "prompt_cache_key", "cache_salt", "reasoning_effort"} {
					if _, present := body[field]; present {
						t.Errorf("default coding request contains unqualified field %s", field)
					}
				}
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || len(thinking) != 2 || thinking["type"] != "enabled" || thinking["clear_thinking"] != false {
					t.Errorf("bundled Coding Plan thinking default is not enabled: %#v", body["thinking"])
				}
				call := calls.Add(1)
				if call > 2 {
					t.Error("unexpected auxiliary or fallback model call")
					w.WriteHeader(400)
					return
				}
				if call == 2 {
					messages, _ := body["messages"].([]any)
					reasoning, tool := false, false
					for _, message := range messages {
						item, _ := message.(map[string]any)
						if item["role"] == "assistant" && item["reasoning_content"] == "SYNTHETIC_REASONING" {
							reasoning = true
						}
						if item["role"] == "tool" {
							tool = true
						}
					}
					if !reasoning || !tool {
						t.Errorf("reasoning/tool continuation lost: reasoning=%t tool=%t", reasoning, tool)
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				emit := func(delta map[string]any, finish any) {
					payload, _ := json.Marshal(map[string]any{"id": "synthetic-zai", "object": "chat.completion.chunk", "created": 1, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
					_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
				}
				emit(map[string]any{"role": "assistant"}, nil)
				emit(map[string]any{"reasoning_content": "SYNTHETIC_REASONING"}, nil)
				if call == 1 {
					emit(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_zai_synthetic", "type": "function", "function": map[string]any{"name": "kodex_counter", "arguments": "{\"ordinal\":1}"}}}}, nil)
					emit(map[string]any{}, "tool_calls")
				} else {
					emit(map[string]any{"content": "SYNTHETIC_ZAI_FINAL"}, nil)
					emit(map[string]any{}, "stop")
				}
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer endpoint.Close()
			config := nativeToolConfiguration(t, fixture)
			config["model"], config["small_model"] = "zai-coding-plan/"+model, "zai-coding-plan/"+model
			config["enabled_providers"] = []string{"zai-coding-plan"}
			config["provider"] = map[string]any{"zai-coding-plan": map[string]any{"npm": "@ai-sdk/openai-compatible", "options": map[string]any{"baseURL": endpoint.URL + "/api/coding/paas/v4", "apiKey": "synthetic-not-a-credential"}, "models": map[string]any{model: map[string]any{"name": model, "limit": map[string]int{"context": 1000000, "output": 32768}}}}}
			server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			session, err := server.client.CreateSession(ctx, "Synthetic Coding Plan qualification")
			if err != nil {
				t.Fatal(err)
			}
			result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_91234567890123456789012345", Model: Model{ProviderID: "zai-coding-plan", ModelID: model}, Parts: []TextPart{{Type: "text", Text: "Perform one synthetic managed counter step."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
			fixture.mu.Lock()
			tools := fixture.tools
			fixture.mu.Unlock()
			if err != nil || result.FinalText != "SYNTHETIC_ZAI_FINAL" || calls.Load() != 2 || tools != 1 {
				t.Fatalf("coding continuation failed: calls=%d tools=%d err=%v", calls.Load(), tools, err)
			}
		})
	}
}
