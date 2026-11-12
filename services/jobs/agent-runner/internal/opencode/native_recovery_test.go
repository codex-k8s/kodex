package opencode

import (
	"bytes"
	"context"
	"errors"
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

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestNativeFailureRecoveryBounds qualifies real upstream error/retry behavior against one
// synthetic route. It retains accepted tool outputs after failed compaction, keeps normalized invalid
// accounting unknown and rejects incomplete streams, and never turns a failed summary into a business success.
// TestNativeFailureRecoveryBounds проверяет реальные upstream ошибки/retry на одном
// synthetic route. Сохраняет принятые tool outputs после ошибки compaction, не повышает неизвестный
// accounting до полного и отклоняет неполный поток и не превращает ошибку summary в успешный бизнес-результат.
func TestNativeFailureRecoveryBounds(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	for _, mode := range []string{"summary", "context-overflow", "rate-limit", "invalid-usage", "interrupted-stream"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &nativeFixture{targetTools: 12}
			config := nativeToolConfiguration(t, fixture)
			var requests, failures atomic.Int64
			model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				raw, readErr := io.ReadAll(io.LimitReader(request.Body, 2<<20+1))
				if readErr != nil || len(raw) > 2<<20 || request.URL.Path != "/v1/chat/completions" {
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				number := requests.Add(1)
				request.Body = io.NopCloser(bytes.NewReader(raw))
				switch {
				case mode == "summary" && bytes.Contains(raw, []byte("SYNTHETIC_NATIVE_COMPACTION")):
					failures.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(http.StatusBadRequest)
					_, _ = writer.Write([]byte(`{"error":{"message":"SYNTHETIC_PRIVATE_SUMMARY_ERROR","type":"invalid_request_error"}}`))
				case mode == "context-overflow":
					failures.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(http.StatusBadRequest)
					_, _ = writer.Write([]byte(`{"error":{"message":"maximum context length exceeded SYNTHETIC_PRIVATE_CONTEXT_ERROR","type":"invalid_request_error","code":"context_length_exceeded"}}`))
				case mode == "rate-limit" && number == 1:
					failures.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					writer.Header().Set("Retry-After", "0")
					writer.WriteHeader(http.StatusTooManyRequests)
					_, _ = writer.Write([]byte(`{"error":{"message":"SYNTHETIC_PRIVATE_RATE_LIMIT","type":"rate_limit_error"}}`))
				case mode == "invalid-usage" || mode == "interrupted-stream":
					failures.Add(1)
					writer.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(writer, "data: {\"id\":\"failure-qualification\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"tiny\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"SYNTHETIC_INCOMPLETE_REPLY\"},\"finish_reason\":null}]}\n\n")
					if mode == "invalid-usage" {
						_, _ = fmt.Fprint(writer, "data: {\"id\":\"failure-qualification\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"tiny\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"failure-qualification\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"tiny\",\"choices\":[],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":-2,\"total_tokens\":-3}}\n\ndata: [DONE]\n\n")
					}
				default:
					fixture.model(writer, request)
				}
			}))
			t.Cleanup(model.Close)
			config["provider"].(map[string]any)["synthetic"].(map[string]any)["options"].(map[string]string)["baseURL"] = model.URL + "/v1"
			server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
			session, err := server.client.CreateSession(t.Context(), "Native bounded error qualification")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			var measurements []runtimecontract.ModelMeasurement
			result, err := server.client.Execute(ctx, session.ID, Prompt{
				MessageID: "msg_51234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
				Parts: []TextPart{{Type: "text", Text: "Complete the managed counter steps and return the final marker."}},
			}, func(name string) bool { return name == "kodex_counter" }, func(_, text string) error {
				if strings.Contains(text, "Native compaction checkpoint") {
					return ErrProtocol
				}
				return nil
			}, TurnObservers{Measurement: func(value runtimecontract.ModelMeasurement) error {
				measurements = append(measurements, value)
				return nil
			}})
			server.stop(t)
			fixture.mu.Lock()
			tools, calls, violations := fixture.tools, fixture.modelCalls, len(fixture.violations)
			fixture.mu.Unlock()
			if failures.Load() == 0 || requests.Load() > 24 || violations != 0 {
				t.Fatalf("synthetic failure boundary was not exercised or bounded: requests=%d failures=%d violations=%d", requests.Load(), failures.Load(), violations)
			}
			if mode == "rate-limit" {
				if err != nil || result.FinalText != "SYNTHETIC_FINAL_COUNTER_12" || tools != 12 || requests.Load() != int64(calls)+1 || failures.Load() != 1 || result.Compactions < 2 {
					t.Fatalf("native rate-limit recovery repeated effects or lost compaction: requests=%d tools=%d compactions=%d error=%v", requests.Load(), tools, result.Compactions, err)
				}
			} else if mode == "invalid-usage" {
				if err != nil || requests.Load() != 1 || tools != 0 || result.Usage.TotalTokens != 0 || len(measurements) != 1 || measurements[0].Completeness != "UNKNOWN" || measurements[0].Usage.Completeness != runtimecontract.UsageUnknown {
					t.Fatalf("normalized invalid counters became complete accounting: requests=%d usage=%+v measurements=%+v error=%v", requests.Load(), result.Usage, measurements, err)
				}
			} else if err == nil || errors.Is(err, context.DeadlineExceeded) || result.FinalText != "" || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_") {
				t.Fatalf("native failure escaped its closed outcome: requests=%d final=%t error=%v", requests.Load(), result.FinalText != "", err)
			}
			if mode == "summary" {
				path := filepath.Join(t.TempDir(), "failed-summary-export.json")
				checkpoint, exportErr := server.server.Export(t.Context(), session.ID, path)
				raw, readErr := os.ReadFile(path)
				if exportErr != nil || readErr != nil || checkpoint.SessionID != session.ID || tools == 0 || tools >= 12 || failures.Load() != 1 || result.Compactions != 0 {
					t.Fatalf("failed summary lost its stopped checkpoint boundary: tools=%d failures=%d export=%v", tools, failures.Load(), exportErr)
				}
				for ordinal := 1; ordinal <= tools; ordinal++ {
					if !bytes.Contains(raw, []byte(fmt.Sprintf("SYNTHETIC_COUNTER_%d\\n", ordinal))) {
						t.Fatal("accepted tool output disappeared after failed compaction")
					}
				}
			}
			t.Logf("native failure=%s requests=%d injected=%d acceptedTools=%d compactions=%d", mode, requests.Load(), failures.Load(), tools, result.Compactions)
		})
	}
}
