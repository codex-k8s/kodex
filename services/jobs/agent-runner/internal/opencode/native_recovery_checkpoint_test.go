package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeFailedSummaryCheckpointResumesWithoutReplayingTools joins a failed real compactor,
// exports/imports its native history into an empty HOME and completes the original managed loop.
// The synthetic backend supplies only model responses; it neither edits history nor retries effects.
// TestNativeFailedSummaryCheckpointResumesWithoutReplayingTools останавливает настоящий compactor
// после ошибки, переносит native history через export/import в пустой HOME и завершает managed loop.
// Синтетический backend заменяет только модель, не меняет историю и не повторяет effects.
func TestNativeFailedSummaryCheckpointResumesWithoutReplayingTools(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{targetTools: 12}
	config := nativeToolConfiguration(t, fixture)
	var failedSummaries atomic.Int32
	var recovering atomic.Bool
	var wireMu sync.Mutex
	var firstRecoveryWire []byte
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20+1))
		if err != nil || len(raw) > 2<<20 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if recovering.Load() {
			wireMu.Lock()
			if firstRecoveryWire == nil {
				firstRecoveryWire = append([]byte{}, raw...)
			}
			wireMu.Unlock()
		} else if bytes.Contains(raw, []byte("SYNTHETIC_NATIVE_COMPACTION")) {
			failedSummaries.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"SYNTHETIC_SUMMARY_REJECTED","type":"invalid_request_error"}}`))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		fixture.model(w, r)
	}))
	defer model.Close()
	config["provider"].(map[string]any)["synthetic"].(map[string]any)["options"].(map[string]string)["baseURL"] = model.URL + "/v1"
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	first := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	session, err := first.client.CreateSession(ctx, "Synthetic failed compaction recovery")
	if err != nil {
		t.Fatal(err)
	}
	result, err := first.client.Execute(ctx, session.ID, Prompt{
		MessageID: "msg_E1234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
		System: "SYNTHETIC_BEFORE_FAILURE_INSTRUCTIONS",
		Parts:  []TextPart{{Type: "text", Text: "SYNTHETIC_FIRST_RECOVERY_INPUT. Complete all managed counter steps."}},
	}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
	first.stop(t)
	fixture.mu.Lock()
	accepted := fixture.tools
	fixture.mu.Unlock()
	if err == nil || result.FinalText != "" || accepted < 1 || accepted >= 12 || failedSummaries.Load() != 1 {
		t.Fatalf("failed compaction did not preserve its recovery boundary: accepted=%d failures=%d", accepted, failedSummaries.Load())
	}
	export := filepath.Join(t.TempDir(), "failed-summary.json")
	checkpoint, err := first.server.Export(ctx, session.ID, export)
	raw, readErr := os.ReadFile(export)
	if err != nil || readErr != nil || checkpoint.SessionID != session.ID {
		t.Fatal("failed summary did not produce an exact joined native checkpoint")
	}
	before := nativeCheckpointToolOutputs(t, raw)
	if len(before) != accepted {
		t.Fatal("accepted managed outputs were missing from the stopped native export")
	}
	replacement := emptyNativeRestoreHome(t)
	if err := restore(ctx, binary, replacement, export, session.ID, checkpoint.SHA256); err != nil {
		t.Fatal("official import rejected the joined failed-summary checkpoint", err)
	}
	recovering.Store(true)
	second := startConfiguredNativeServer(t, binary, replacement, config)
	continued, err := second.client.Execute(ctx, session.ID, Prompt{
		MessageID: "msg_F1234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
		System: "SYNTHETIC_AFTER_FAILURE_INSTRUCTIONS",
		Parts:  []TextPart{{Type: "text", Text: "SYNTHETIC_SECOND_RECOVERY_INPUT. Continue the retained completed counter steps."}},
	}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
	second.stop(t)
	if err != nil || continued.FinalText != "SYNTHETIC_FINAL_COUNTER_12" || continued.Compactions < 1 {
		t.Fatal("native recovery failed to continue through a successful compaction", err)
	}
	reexport := filepath.Join(t.TempDir(), "continued.json")
	if _, err := second.server.Export(ctx, session.ID, reexport); err != nil {
		t.Fatal("continued history could not be sealed")
	}
	continuedRaw, err := os.ReadFile(reexport)
	if err != nil {
		t.Fatal(err)
	}
	after := nativeCheckpointToolOutputs(t, continuedRaw)
	for id, output := range before {
		if after[id] != output {
			t.Fatal("native recovery changed an already accepted managed output")
		}
	}
	wireMu.Lock()
	firstWire := string(firstRecoveryWire)
	wireMu.Unlock()
	for _, output := range before {
		marker := strings.SplitN(output, "\n", 2)[0]
		if !strings.Contains(firstWire, marker) {
			t.Fatal("restored native history did not reach the first recovery model request")
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 12 || len(after) != 12 || len(fixture.violations) != 0 || fixture.maximumWire > 64<<10 ||
		!strings.Contains(firstWire, "SYNTHETIC_AFTER_FAILURE_INSTRUCTIONS") || strings.Contains(firstWire, "SYNTHETIC_BEFORE_FAILURE_INSTRUCTIONS") ||
		strings.Count(firstWire, "SYNTHETIC_SECOND_RECOVERY_INPUT") != 1 {
		t.Fatalf("native recovery boundary: tools=%d retained=%d violations=%d maximumWire=%d freshSystem=%t oldSystem=%t businessInputs=%d", fixture.tools, len(after), len(fixture.violations), fixture.maximumWire, strings.Contains(firstWire, "SYNTHETIC_AFTER_FAILURE_INSTRUCTIONS"), strings.Contains(firstWire, "SYNTHETIC_BEFORE_FAILURE_INSTRUCTIONS"), strings.Count(firstWire, "SYNTHETIC_SECOND_RECOVERY_INPUT"))
	}
	t.Logf("failed summary recovery: acceptedBefore=%d totalTools=%d recoveredCompactions=%d retainedToolOutputs=%d maximumWireBytes=%d", accepted, fixture.tools, continued.Compactions, len(after), fixture.maximumWire)
}

// nativeCheckpointToolOutputs indexes only completed managed tool outputs by native part ID.
// It checks preservation without changing or reconstructing any native session message.
// nativeCheckpointToolOutputs индексирует завершённые managed outputs по native part ID.
// Проверяет сохранность, не меняя и не восстанавливая самостоятельно native messages.
func nativeCheckpointToolOutputs(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var record struct{ Messages []Message }
	if json.Unmarshal(raw, &record) != nil {
		t.Fatal("native recovery export is malformed")
	}
	outputs := map[string]string{}
	for _, message := range record.Messages {
		for _, rawPart := range message.Parts {
			var part struct {
				ID, Type, Tool string
				State          struct{ Status, Output string }
			}
			if json.Unmarshal(rawPart, &part) != nil {
				t.Fatal("native recovery part is malformed")
			}
			if part.Type == "tool" && part.Tool == "kodex_counter" && part.State.Status == "completed" {
				if outputs[part.ID] != "" || part.State.Output == "" {
					t.Fatal("native managed output is duplicated or empty")
				}
				outputs[part.ID] = part.State.Output
			}
		}
	}
	return outputs
}
