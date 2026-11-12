package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// nativeProtocolProxyModels adds exact synthetic aliases to the isolated proxy without any public upstream.
// nativeProtocolProxyModels добавляет точные синтетические aliases без публичного upstream.
const nativeProtocolProxyModels = `  - model_name: responses-alias
    litellm_params:
      model: openai/gpt-4.1
      api_base: http://127.0.0.1:4001/v1
      api_key: synthetic-not-a-credential
  - model_name: messages-alias
    litellm_params:
      model: anthropic/claude-sonnet-4-20250514
      api_base: http://127.0.0.1:4001
      api_key: synthetic-not-a-credential
  - model_name: responses-fallback
    litellm_params:
      model: openai/gpt-4.1
      api_base: http://127.0.0.1:4001/fallback/v1
      api_key: synthetic-not-a-credential
  - model_name: messages-fallback
    litellm_params:
      model: anthropic/claude-sonnet-4-20250514
      api_base: http://127.0.0.1:4001/fallback
      api_key: synthetic-not-a-credential
`

// nativeProxyProtocols observes actual upstream calls after the real proxy has resolved aliases and filtered parameters.
// nativeProxyProtocols наблюдает фактические запросы после разрешения aliases и фильтрации параметров настоящим proxy.
type nativeProxyProtocols struct {
	t             *testing.T
	mu            sync.Mutex
	calls         map[string]int
	bodies        map[string]map[string]any
	failPrimary   bool
	fallbackCalls int
}

// serve handles only the two synthetic protocol routes; the existing managed-tool fixture retains the chat route.
// serve обслуживает два синтетических протокола; прежняя оснастка управляемых инструментов сохраняет chat route.
func (p *nativeProxyProtocols) serve(w http.ResponseWriter, r *http.Request) bool {
	fallback := strings.HasPrefix(r.URL.Path, "/fallback/")
	path := strings.TrimPrefix(r.URL.Path, "/fallback")
	api, model := "RESPONSES", "gpt-4.1"
	if path == "/v1/messages" {
		api, model = "MESSAGES", "claude-sonnet-4-20250514"
	} else if path != "/v1/responses" {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	var body map[string]any
	if err != nil || json.Unmarshal(raw, &body) != nil || body["model"] != model || body["stream"] != true {
		p.t.Error("real proxy changed protocol, alias or streaming input")
		w.WriteHeader(400)
		return true
	}
	if body["cache"] != nil || body["disable_fallbacks"] != nil {
		p.t.Error("proxy-only controls leaked upstream")
	}
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]int{}
		p.bodies = map[string]map[string]any{}
	}
	if fallback {
		p.fallbackCalls++
	}
	fail := p.failPrimary && !fallback
	p.calls[api]++
	p.bodies[api] = body
	p.mu.Unlock()
	if fail {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"error":{"message":"synthetic primary failure","type":"server_error"}}`)
		return true
	}
	w.Header().Set("Content-Type", "text/event-stream")
	nativeAdapterFinalStream(w, api)
	return true
}

// count returns the correlated upstream request count without inferring cache behavior from latency or headers.
// count возвращает счётчик upstream-запросов без вывода о кеше по задержке или заголовкам.
func (p *nativeProxyProtocols) count(api string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[api]
}

// qualifyProxyFallbackFence first observes the configured fallback, then proves the exact native controls suppress it.
// qualifyProxyFallbackFence сначала наблюдает настроенный fallback, затем доказывает его запрет точными native controls.
func qualifyProxyFallbackFence(t *testing.T, client *http.Client, origin, path string, actual map[string]any, observed *nativeProxyProtocols) {
	t.Helper()
	observed.mu.Lock()
	observed.failPrimary = true
	before := observed.fallbackCalls
	observed.mu.Unlock()
	defer func() { observed.mu.Lock(); observed.failPrimary = false; observed.mu.Unlock() }()
	send := func(value map[string]any) int {
		raw, _ := json.Marshal(value)
		response, err := nativeProxyAPIRequest(client, origin, path, raw)
		if err != nil {
			t.Error("fallback probe transport failed")
			return 0
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
		return response.StatusCode
	}
	baseline := make(map[string]any, len(actual))
	for key, value := range actual {
		baseline[key] = value
	}
	baseline["disable_fallbacks"] = false
	if send(baseline) != 200 {
		t.Error("configured synthetic fallback did not succeed")
	}
	observed.mu.Lock()
	afterBaseline := observed.fallbackCalls
	observed.mu.Unlock()
	if afterBaseline != before+1 {
		t.Error("synthetic fallback effectiveness is unproven")
	}
	if status := send(actual); status < 400 || status > 599 {
		t.Errorf("native fallback fence returned status=%d", status)
	}
	observed.mu.Lock()
	defer observed.mu.Unlock()
	if observed.fallbackCalls != afterBaseline {
		t.Error("real proxy bypassed native fallback fence")
	}
}

// qualifyProxyNoHints verifies that the proxy does not inject cache hints when the captured request omits them.
// qualifyProxyNoHints проверяет отсутствие инъекции подсказок proxy при их отсутствии в захваченном запросе.
func qualifyProxyNoHints(t *testing.T, client *http.Client, origin, path, api string, actual map[string]any, observed *nativeProxyProtocols) {
	t.Helper()
	request := make(map[string]any, len(actual))
	for key, value := range actual {
		request[key] = value
	}
	delete(request, "prompt_cache_key")
	delete(request, "cache_control")
	before := observed.count(api)
	for range 2 {
		raw, _ := json.Marshal(request)
		response, err := nativeProxyAPIRequest(client, origin, path, raw)
		if err != nil {
			t.Fatal("no-hints proxy probe failed")
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
		response.Body.Close()
		if response.StatusCode != 200 || readErr != nil {
			t.Fatal("no-hints proxy response failed")
		}
	}
	observed.mu.Lock()
	defer observed.mu.Unlock()
	body := observed.bodies[api]
	if observed.calls[api] != before+2 || body["prompt_cache_key"] != nil || body["cache_control"] != nil {
		t.Error("no-hints proxy policy was not preserved")
	}
}

// qualifyNativeProxyProtocols proves native SDK calls, seeded cache bypass and exact hints through the real proxy for both APIs.
// qualifyNativeProxyProtocols проверяет native SDK, обход заполненного кеша и точные подсказки через настоящий proxy для обоих API.
func qualifyNativeProxyProtocols(t *testing.T, binary string, client *http.Client, origin string, observed *nativeProxyProtocols) {
	for _, api := range []string{"RESPONSES", "MESSAGES"} {
		t.Run(api, func(t *testing.T) {
			alias, path, npm, mechanism := "responses-alias", "/v1/responses", "@ai-sdk/openai", "OPENAI_PROMPT_CACHE_KEY"
			if api == "MESSAGES" {
				alias, path, npm, mechanism = "messages-alias", "/v1/messages", "@ai-sdk/anthropic", "ANTHROPIC_EPHEMERAL"
			}
			var probes sync.Once
			var bodyMu sync.Mutex
			var actualBody map[string]any
			front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
				var body map[string]any
				if json.Unmarshal(raw, &body) != nil || r.URL.Path != path || body["model"] != alias {
					t.Error("native proxy protocol request mismatch")
					w.WriteHeader(400)
					return
				}
				cache, _ := body["cache"].(map[string]any)
				if len(cache) != 2 || cache["no-cache"] != true || cache["no-store"] != true || body["disable_fallbacks"] != true {
					t.Error("native proxy controls missing")
					w.WriteHeader(400)
					return
				}
				bodyMu.Lock()
				actualBody = body
				bodyMu.Unlock()
				probes.Do(func() {
					qualifyLiteLLMCacheReadWrite(t, client, origin, path, body, func() int { return observed.count(api) })
				})
				response, err := nativeProxyAPIRequest(client, origin, path, raw)
				if err != nil {
					w.WriteHeader(502)
					return
				}
				defer response.Body.Close()
				w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
				w.WriteHeader(response.StatusCode)
				_, _ = io.Copy(w, response.Body)
			}))
			defer front.Close()
			config := nativeConfiguration()
			provider := config["provider"].(map[string]any)["synthetic"].(map[string]any)
			provider["npm"] = npm
			models := provider["models"].(map[string]any)
			models[alias] = models["tiny"]
			delete(models, "tiny")
			config["model"] = "synthetic/" + alias
			config["small_model"] = "synthetic/" + alias
			provider["options"].(map[string]string)["baseURL"] = front.URL + "/v1"
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
			spec, _ := json.Marshal(map[string]any{"api": api, "provider_id": "synthetic", "model_id": alias, "base_url": front.URL + "/v1", "route_kind": "LITELLM", "inference_cache": map[string]any{"mechanism": mechanism, "namespace": strings.Repeat("c", 64), "no_reuse": false}})
			server := startConfiguredNativeServerEnvironment(t, binary, home, config, []string{"KODEX_MODEL_TRANSPORT=" + string(spec), "KODEX_MCP_PROXY_TOKEN=" + strings.Repeat("a", 64), "KODEX_NATIVE_REVISION=" + strings.Repeat("b", 64)})
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			session, err := server.client.CreateSession(ctx, "Synthetic real proxy protocol")
			if err != nil {
				t.Fatal(err)
			}
			result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_D1234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: alias}, System: "SYNTHETIC_CURRENT_SYSTEM", Parts: []TextPart{{Type: "text", Text: "SYNTHETIC_CURRENT_TASK"}}}, func(string) bool { return false }, func(string, string) error { return nil })
			if err != nil || result.FinalText != "SYNTHETIC_ADAPTER_FINAL" {
				t.Fatalf("real proxy protocol failed: %v", err)
			}
			observed.mu.Lock()
			body := observed.bodies[api]
			observed.mu.Unlock()
			if api == "RESPONSES" && body["prompt_cache_key"] != strings.Repeat("c", 64) {
				t.Error("real proxy dropped Responses cache key")
			}
			if api == "MESSAGES" {
				marker, _ := body["cache_control"].(map[string]any)
				if len(marker) != 1 || marker["type"] != "ephemeral" {
					t.Error("real proxy dropped Messages automatic cache control")
				}
			}
			bodyMu.Lock()
			captured := actualBody
			bodyMu.Unlock()
			qualifyProxyNoHints(t, client, origin, path, api, captured, observed)
			qualifyProxyFallbackFence(t, client, origin, path, captured, observed)
			t.Logf("real LiteLLM protocol=%s upstreamCalls=%d alias/hints/cache bypass checked", api, observed.count(api))
		})
	}
}
