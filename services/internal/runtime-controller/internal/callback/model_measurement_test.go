package callback

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestModelMeasurementCallbackWirePreservesBinding exercises actual runner serialization through
// the strict HTTP decoder and owner RPC projection; malformed or mixed envelopes never reach the owner.
// TestModelMeasurementCallbackWirePreservesBinding проводит сериализацию runner через строгий
// HTTP decoder и owner RPC; неверные и смешанные конверты не доходят до владельца.
func TestModelMeasurementCallbackWirePreservesBinding(t *testing.T) {
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t)
	client := &financialProgressClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, manager: manager,
		control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	duration, first := int64(120), int64(30)
	measurements := []runtimecontract.ModelMeasurement{
		{Ref: "request_fixture01", Source: "HTTP_REQUEST", Phase: "NORMAL", Outcome: "COMPLETED", Completeness: "COMPLETE", DurationMS: &duration, TTFTMS: &first},
		{Ref: "step_fixture001", Source: "NATIVE_STEP", Phase: "COMPACTION", Outcome: "COMPLETED", Completeness: "PARTIAL", Usage: &runtimecontract.TokenUsage{Completeness: runtimecontract.UsagePartial, InputTokens: 100, OutputTokens: 10, TotalTokens: 110, CachedInputTokens: 20, CacheWriteInputTokens: 5}},
	}
	for _, measurement := range measurements {
		raw, err := json.Marshal(runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, ModelMeasurement: &measurement})
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range []struct {
			name string
			raw  string
			want int
		}{
			{"valid", string(raw), http.StatusNoContent},
			{"duplicate", strings.Replace(string(raw), `"progress":""`, `"progress":"","progress":""`, 1), http.StatusBadRequest},
			{"aliased", strings.Replace(string(raw), `"model_measurement"`, `"ModelMeasurement"`, 1), http.StatusBadRequest},
			{"unknown", strings.TrimSuffix(string(raw), "}") + `,"untrusted":true}`, http.StatusBadRequest},
			{"mixed", strings.Replace(string(raw), `"progress":""`, `"progress":"MODEL_REQUEST_RUNNING"`, 1), http.StatusBadRequest},
			{"foreign revision", strings.Replace(string(raw), input.RuntimeRevisionDigest, strings.Repeat("0", 64), 1), http.StatusBadRequest},
			{"invalid source", strings.Replace(string(raw), measurement.Source, "UNKNOWN_SOURCE", 1), http.StatusBadRequest},
		} {
			t.Run(measurement.Source+"/"+candidate.name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/progress", bytes.NewBufferString(candidate.raw))
				request.Header.Set("Authorization", "Bearer "+ticket)
				bindTestExecutionHeaders(request, input, "progress")
				response := httptest.NewRecorder()
				server.route(response, request)
				if response.Code != candidate.want {
					t.Fatalf("callback status = %d, want %d", response.Code, candidate.want)
				}
			})
		}
	}
	if len(client.requests) != 2 {
		t.Fatalf("unexpected owner calls: %d", len(client.requests))
	}
	for index, request := range client.requests {
		if request.LeaseRef != input.LeaseRef || request.Fence != input.LeaseFence || request.Generation != input.LeaseGeneration || request.Progress != "" || request.ModelMeasurement == nil || request.ModelMeasurement.Ref != measurements[index].Ref {
			t.Fatal("measurement owner binding lost")
		}
	}
	if client.requests[0].ModelMeasurement.GetTtftMs() != first || client.requests[1].ModelMeasurement.Usage.GetCacheWriteInputTokens() != 5 {
		t.Fatal("measurement accounting lost")
	}
}
