package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeReasoningOptionsOnWire qualifies model-level preferences through the pinned OpenCode and bundled SDKs; a synthetic model replaces only inference.
// TestNativeReasoningOptionsOnWire проверяет настройки модели через закреплённые OpenCode и SDK; синтетическая модель заменяет только inference.
func TestNativeReasoningOptionsOnWire(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	for _, api := range []string{"RESPONSES", "CHAT_COMPLETIONS"} {
		for _, effort := range []string{"", "low", "high"} {
			t.Run(api+"/"+effort, func(t *testing.T) {
				var calls atomic.Int32
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
					var body map[string]any
					if err != nil || json.Unmarshal(raw, &body) != nil {
						t.Error("invalid synthetic model request")
						w.WriteHeader(400)
						return
					}
					actual, _ := body["reasoning_effort"].(string)
					path := "/v1/chat/completions"
					if api == "RESPONSES" {
						path = "/v1/responses"
						reasoning, _ := body["reasoning"].(map[string]any)
						actual, _ = reasoning["effort"].(string)
					}
					if r.Method != "POST" || r.URL.Path != path || actual != effort {
						t.Error("reasoning preference changed on the native SDK wire")
					}
					calls.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					nativeAdapterFinalStream(w, api)
				}))
				defer endpoint.Close()
				config := nativeConfiguration()
				provider := config["provider"].(map[string]any)["synthetic"].(map[string]any)
				if api == "RESPONSES" {
					provider["npm"] = "@ai-sdk/openai"
				}
				provider["options"].(map[string]string)["baseURL"] = endpoint.URL + "/v1"
				if effort != "" {
					provider["models"].(map[string]any)["tiny"].(map[string]any)["options"] = map[string]string{"reasoningEffort": effort}
				}
				server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				session, err := server.client.CreateSession(ctx, "Synthetic reasoning preference")
				if err != nil {
					t.Fatal(err)
				}
				result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_R1234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Return the synthetic final marker."}}}, func(string) bool { return false }, func(string, string) error { return nil })
				if err != nil || result.FinalText != "SYNTHETIC_ADAPTER_FINAL" || calls.Load() != 1 {
					t.Fatalf("native reasoning qualification failed: calls=%d err=%v", calls.Load(), err)
				}
			})
		}
	}
}
