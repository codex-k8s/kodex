package opencode

import (
	"context"
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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

// TestNativeImageHookBindsActualToolCalls qualifies upstream-provided call identity through compaction.
// The hook observes native state; the synthetic managed counter still owns its independent effects.
// TestNativeImageHookBindsActualToolCalls проверяет upstream call identity через compaction.
// Hook наблюдает native state; synthetic managed counter отдельно учитывает свои effects.
func TestNativeImageHookBindsActualToolCalls(t *testing.T) {
	for _, kind := range []string{"DIRECT", "LITELLM"} {
		t.Run(kind, func(t *testing.T) { qualifyNativeImageHook(t, kind) })
	}
}

// qualifyNativeImageHook checks native tools, compaction, process replacement and the per-request model transport hook.
// qualifyNativeImageHook проверяет native tools, compaction, process replacement и per-request model transport hook.
func qualifyNativeImageHook(t *testing.T, routeKind string, cache ...*nativeCacheWireExpectation) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{modelDelay: 20 * time.Millisecond, proxyCacheBypass: routeKind == "LITELLM", identityKey: strings.Repeat("d", 64), identityRevision: strings.Repeat("b", 64), nativeIdentities: map[string]bool{}}
	if len(cache) > 0 {
		fixture.cacheExpectation = cache[0]
	}
	var measurementMutex sync.Mutex
	var measurements []runtimecontract.ModelMeasurement
	collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture.mu.Lock()
		key, revision := fixture.identityKey, fixture.identityRevision
		fixture.mu.Unlock()
		raw, err := io.ReadAll(io.LimitReader(request.Body, 4097))
		value, decodeErr := runtimecontract.DecodeModelMeasurement(raw)
		if err != nil || decodeErr != nil || request.URL.Path != "/model-measurements" || request.Header.Get("Authorization") != "Bearer "+key || request.Header.Get("X-Kodex-Runtime-Revision") != revision {
			t.Error("invalid actual HTTP measurement")
			writer.WriteHeader(400)
			return
		}
		measurementMutex.Lock()
		measurements = append(measurements, value)
		measurementMutex.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer collector.Close()
	measurementEnvironment := "KODEX_MODEL_MEASUREMENT_URL=" + collector.URL + "/model-measurements"
	config := nativeToolConfiguration(t, fixture)
	base := config["provider"].(map[string]any)["synthetic"].(map[string]any)["options"].(map[string]string)["baseURL"]
	spec := map[string]any{"provider_id": "synthetic", "model_id": "tiny", "base_url": base, "api": "CHAT_COMPLETIONS", "route_kind": routeKind}
	if fixture.cacheExpectation != nil {
		expected := fixture.cacheExpectation
		spec["inference_cache"] = map[string]any{"mechanism": expected.mechanism, "namespace": expected.namespace, "no_reuse": expected.noReuse}
	}
	transport, _ := json.Marshal(spec)
	transportEnvironment := "KODEX_MODEL_TRANSPORT=" + string(transport)
	home := t.TempDir()
	if os.Chmod(home, 0o700) != nil {
		t.Fatal("prepare isolated native HOME")
	}
	plugin, err := InstallManagedToolPlugin(home)
	if err != nil {
		fixture.mu.Lock()
		t.Errorf("native hook failed: tools=%d requests=%d identities=%d violations=%d", fixture.tools, fixture.modelCalls, len(fixture.nativeIdentities), len(fixture.violations))
		fixture.mu.Unlock()
		t.Fatal(err)
	}
	config["plugin"] = []string{plugin}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, "config", "opencode"), 0o700) })
	server := startConfiguredNativeServerEnvironment(t, binary, home, config, []string{measurementEnvironment, transportEnvironment, "KODEX_MCP_PROXY_TOKEN=" + fixture.identityKey, "KODEX_NATIVE_REVISION=" + fixture.identityRevision})
	session, err := server.client.CreateSession(t.Context(), "Synthetic native tool identity")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	compacting, running := 0, 0
	phase := func(code string) error {
		switch code {
		case "MODEL_CONTEXT_COMPACTING":
			compacting++
		case "MODEL_REQUEST_RUNNING":
			running++
		default:
			t.Error("unknown native progress code")
		}
		return nil
	}
	result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_61234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Run the synthetic managed counter."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil }, TurnObservers{Progress: phase})
	if err != nil {
		fixture.mu.Lock()
		t.Errorf("native hook failed: tools=%d requests=%d identities=%d violations=%d", fixture.tools, fixture.modelCalls, len(fixture.nativeIdentities), len(fixture.violations))
		fixture.mu.Unlock()
		t.Fatal(err)
	}
	if result.FinalText != "SYNTHETIC_FINAL_COUNTER_12" {
		messages, _ := server.client.Messages(ctx, session.ID, 6, "")
		for _, message := range messages {
			for _, part := range message.Parts {
				var wire struct {
					State struct {
						Error string `json:"error"`
					} `json:"state"`
				}
				if json.Unmarshal(part, &wire) == nil && wire.State.Error != "" {
					t.Logf("synthetic tool failure: %.180s", wire.State.Error)
				}
			}
		}
	}
	server.stop(t)
	measurementMutex.Lock()
	measuredNormal, measuredCompaction := 0, 0
	refs := map[string]bool{}
	for _, measurement := range measurements {
		if measurement.Source != "HTTP_REQUEST" || measurement.Outcome != "COMPLETED" || measurement.Completeness != "COMPLETE" || measurement.TTFTMS == nil || *measurement.TTFTMS < 10 || refs[measurement.Ref] {
			t.Errorf("actual fetch observation incomplete: phase=%s outcome=%s completeness=%s", measurement.Phase, measurement.Outcome, measurement.Completeness)
		}
		refs[measurement.Ref] = true
		if measurement.Phase == "NORMAL" {
			measuredNormal++
		}
		if measurement.Phase == "COMPACTION" {
			measuredCompaction++
		}
	}
	measurementMutex.Unlock()
	if measuredNormal < 12 || measuredCompaction < 1 {
		t.Fatalf("actual phase measurements missing: normal=%d compaction=%d", measuredNormal, measuredCompaction)
	}
	export := filepath.Join(t.TempDir(), "native.json")
	if _, err := server.server.Export(ctx, session.ID, export); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(export)
	if err != nil || strings.Contains(string(raw), fixture.identityKey) {
		t.Fatal("hook credential persisted in native history")
	}
	fixture.mu.Lock()
	if compacting == 0 || running == 0 || fixture.tools != 12 || len(fixture.nativeIdentities) != 12 || len(fixture.violations) != 0 || result.Compactions < 1 || result.FinalText != "SYNTHETIC_FINAL_COUNTER_12" {
		fixture.mu.Unlock()
		t.Fatal("native hook bypassed or changed managed loop")
	}
	fixture.identityKey, fixture.identityRevision, fixture.targetTools = strings.Repeat("e", 64), strings.Repeat("f", 64), 13
	key, revision := fixture.identityKey, fixture.identityRevision
	fixture.mu.Unlock()
	replacement := t.TempDir()
	if os.Chmod(replacement, 0o700) != nil {
		t.Fatal("prepare replacement HOME")
	}
	for _, directory := range []string{"config", "cache", "data", "state"} {
		if os.Mkdir(filepath.Join(replacement, directory), 0o700) != nil {
			t.Fatal("prepare replacement XDG")
		}
	}
	checkpoint, err := InspectCheckpoint(raw, session.ID)
	if err != nil || restore(ctx, binary, replacement, export, session.ID, checkpoint.SHA256) != nil {
		t.Fatal("restore exact native tool history")
	}
	plugin, err = InstallManagedToolPlugin(replacement)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(replacement, "config", "opencode"), 0o700) })
	config["plugin"] = []string{plugin}
	second := startConfiguredNativeServerEnvironment(t, binary, replacement, config, []string{measurementEnvironment, transportEnvironment, "KODEX_MCP_PROXY_TOKEN=" + key, "KODEX_NATIVE_REVISION=" + revision})
	continued, err := second.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_71234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, System: "SYNTHETIC_CURRENT_INSTRUCTIONS_B", Parts: []TextPart{{Type: "text", Text: "Complete the next managed counter step."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil }, TurnObservers{Progress: phase})
	if err != nil {
		t.Fatal(err)
	}
	second.stop(t)
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 13 || len(fixture.nativeIdentities) != 13 || len(fixture.violations) != 0 || continued.FinalText != "SYNTHETIC_FINAL_COUNTER_13" {
		t.Fatal("native recovery repeated an effect or reused old hook authority")
	}
}

// nativeCacheWireExpectation defines synthetic wire assertions, not a declaration of real backend support.
// nativeCacheWireExpectation задаёт synthetic wire assertions, не заявление о поддержке реального backend.
type nativeCacheWireExpectation struct {
	mechanism, namespace string
	noReuse              bool
}

// TestNativeCachePolicyControlsEveryRequest checks request-specific nonces through real native tools, compaction and replacement, while default requests carry no managed hints.
// TestNativeCachePolicyControlsEveryRequest проверяет request-specific nonce через настоящие native tools, compaction и replacement; default requests не несут managed hints.
func TestNativeCachePolicyControlsEveryRequest(t *testing.T) {
	for _, row := range []struct {
		name, route, mechanism string
		noReuse                bool
	}{
		{"no hints", "DIRECT", "NONE", false},
		{"qualified salt", "LITELLM", "VLLM_CACHE_SALT", false},
		{"no reuse each HTTP", "LITELLM", "VLLM_CACHE_SALT", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			qualifyNativeImageHook(t, row.route, &nativeCacheWireExpectation{mechanism: row.mechanism, namespace: strings.Repeat("c", 64), noReuse: row.noReuse})
		})
	}
}
