package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeBundledProtocolAdapters exercises the real bundled Responses/Messages SDKs and transport hook without translating protocols inside Kodex.
// TestNativeBundledProtocolAdapters проверяет настоящие bundled Responses/Messages SDK и transport hook без трансляции протоколов внутри Kodex.
func TestNativeBundledProtocolAdapters(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	for _, api := range []string{"RESPONSES", "MESSAGES"} {
		for _, kind := range []string{"DIRECT", "LITELLM"} {
			for _, optimized := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/cache=%t", api, kind, optimized), func(t *testing.T) {
					var calls atomic.Int32
					endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						raw, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
						var body map[string]any
						if r.Method != "POST" || json.Unmarshal(raw, &body) != nil || body["model"] != "tiny" || body["stream"] != true || !strings.Contains(string(raw), "SYNTHETIC_CURRENT_SYSTEM") || !strings.Contains(string(raw), "SYNTHETIC_CURRENT_TASK") {
							t.Error("native adapter changed current model input")
							w.WriteHeader(400)
							return
						}
						wantPath := "/v1/responses"
						if api == "MESSAGES" {
							wantPath = "/v1/messages"
						}
						if r.URL.Path != wantPath {
							t.Error("adapter used another API")
							w.WriteHeader(404)
							return
						}
						if kind == "LITELLM" {
							cache, _ := body["cache"].(map[string]any)
							if len(cache) != 2 || cache["no-cache"] != true || cache["no-store"] != true || body["disable_fallbacks"] != true {
								t.Error("proxy read/write bypass or fallback fence missing")
							}
						} else if _, ok := body["cache"]; ok {
							t.Error("proxy-only controls reached direct adapter")
						}
						namespace := strings.Repeat("c", 64)
						if api == "RESPONSES" {
							key, present := body["prompt_cache_key"]
							if optimized && key != namespace || !optimized && present {
								t.Error("Responses cache key did not follow effective policy")
							}
						} else {
							marker, present := body["cache_control"]
							if optimized {
								value, _ := marker.(map[string]any)
								if len(value) != 1 || value["type"] != "ephemeral" {
									t.Error("Messages automatic cache marker is invalid")
								}
							} else if present {
								t.Error("No-hints Messages request acquired automatic caching")
							}
							for _, field := range []string{"system", "tools", "messages"} {
								parts, _ := body[field].([]any)
								for _, part := range parts {
									item, _ := part.(map[string]any)
									if item["cache_control"] != nil {
										t.Error("duplicate native cache breakpoint")
									}
									blocks, _ := item["content"].([]any)
									for _, block := range blocks {
										value, _ := block.(map[string]any)
										if value["cache_control"] != nil {
											t.Error("duplicate native content cache breakpoint")
										}
									}
								}
							}
						}
						if api == "MESSAGES" {
							if r.Header.Get("x-api-key") != "synthetic-not-a-credential" || r.Header.Get("anthropic-version") == "" {
								t.Error("Messages authentication/version missing")
							}
						} else if r.Header.Get("Authorization") != "Bearer synthetic-not-a-credential" {
							t.Error("Responses authentication missing")
						}
						calls.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						nativeAdapterFinalStream(w, api)
					}))
					defer endpoint.Close()
					config := nativeConfiguration()
					provider := config["provider"].(map[string]any)["synthetic"].(map[string]any)
					provider["npm"] = "@ai-sdk/openai"
					if api == "MESSAGES" {
						provider["npm"] = "@ai-sdk/anthropic"
					}
					provider["options"].(map[string]string)["baseURL"] = endpoint.URL + "/v1"
					home := t.TempDir()
					if os.Chmod(home, 0700) != nil {
						t.Fatal("prepare private native home")
					}
					plugin, err := InstallManagedToolPlugin(home)
					if err != nil {
						t.Fatal(err)
					}
					config["plugin"] = []string{plugin}
					t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, "config", "opencode"), 0700) })
					mechanism := "NONE"
					if optimized {
						mechanism = "OPENAI_PROMPT_CACHE_KEY"
						if api == "MESSAGES" {
							mechanism = "ANTHROPIC_EPHEMERAL"
						}
					}
					spec, _ := json.Marshal(map[string]any{"api": api, "provider_id": "synthetic", "model_id": "tiny", "base_url": endpoint.URL + "/v1", "route_kind": kind, "inference_cache": map[string]any{"mechanism": mechanism, "namespace": strings.Repeat("c", 64), "no_reuse": false}})
					server := startConfiguredNativeServerEnvironment(t, binary, home, config, []string{"KODEX_MODEL_TRANSPORT=" + string(spec)})
					ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
					defer cancel()
					session, err := server.client.CreateSession(ctx, "Synthetic bundled API")
					if err != nil {
						t.Fatal(err)
					}
					result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_B1234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, System: "SYNTHETIC_CURRENT_SYSTEM", Parts: []TextPart{{Type: "text", Text: "SYNTHETIC_CURRENT_TASK"}}}, func(string) bool { return false }, func(string, string) error { return nil })
					if err != nil || result.FinalText != "SYNTHETIC_ADAPTER_FINAL" || calls.Load() != 1 {
						t.Fatalf("bundled adapter failed: calls=%d err=%v", calls.Load(), err)
					}
				})
			}
		}
	}
}

// nativeAdapterFinalStream writes documented synthetic SSE frames; only the paid model is replaced, never the upstream SDK or native session engine.
// nativeAdapterFinalStream пишет документированные synthetic SSE frames; заменяется лишь платная модель, не upstream SDK или native session engine.
func nativeAdapterFinalStream(w io.Writer, api string) {
	emit := func(kind string, value any) {
		raw, _ := json.Marshal(value)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, raw)
	}
	text := "SYNTHETIC_ADAPTER_FINAL"
	if api == "MESSAGES" {
		emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_synthetic", "type": "message", "role": "assistant", "model": "tiny", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 42, "output_tokens": 1}}})
		emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
		emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 5}})
		emit("message_stop", map[string]any{"type": "message_stop"})
		return
	}
	item := map[string]any{"type": "message", "id": "msg_synthetic", "role": "assistant", "status": "in_progress", "content": []any{}}
	emit("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_synthetic", "created_at": 1700000000, "model": "tiny", "object": "response", "status": "in_progress", "output": []any{}}})
	emit("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
	emit("response.content_part.added", map[string]any{"type": "response.content_part.added", "item_id": "msg_synthetic", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
	emit("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": "msg_synthetic", "output_index": 0, "content_index": 0, "delta": text})
	emit("response.output_text.done", map[string]any{"type": "response.output_text.done", "item_id": "msg_synthetic", "output_index": 0, "content_index": 0, "text": text})
	item["status"] = "completed"
	item["content"] = []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}
	emit("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
	emit("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_synthetic", "created_at": 1700000000, "model": "tiny", "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 42, "output_tokens": 5, "total_tokens": 47, "input_tokens_details": map[string]int{"cached_tokens": 0}, "output_tokens_details": map[string]int{"reasoning_tokens": 0}}}})
}
