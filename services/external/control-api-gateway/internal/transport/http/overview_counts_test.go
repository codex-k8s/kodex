package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestOverviewHTTPCounts exercises the actual nested read-RPC HTTP projection: all required
// counters remain JSON numbers, even at zero, and owner totals are not derived from bounded lists.
// TestOverviewHTTPCounts проверяет настоящую вложенную HTTP-проекцию RPC: обязательные счётчики
// остаются JSON numbers, включая ноль; owner totals не вычисляются по ограниченным спискам.
func TestOverviewHTTPCounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		overview *cp.Overview
	}{
		{"empty organization", &cp.Overview{}},
		{"idle organization", &cp.Overview{ProjectCount: 3, AgentCount: 14}},
		{"totals exceed visible lists", &cp.Overview{ProjectCount: 3, AgentCount: 14, ActiveRunCount: 29, PendingGateCount: 37}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &cp.GetOverviewResponse{Overview: tc.overview}
			before := proto.Clone(response)
			writer := httptest.NewRecorder()
			writeReadRPCResult(writer, response, nil, "overview", "")
			if writer.Code != http.StatusOK || writer.Header().Get("Content-Type") != "application/json" || writer.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("overview HTTP response status or headers changed")
			}
			decoder := json.NewDecoder(writer.Body)
			decoder.UseNumber()
			var body map[string]any
			if decoder.Decode(&body) != nil {
				t.Fatal("overview HTTP response is not JSON")
			}
			for key, want := range map[string]int32{"projectCount": tc.overview.ProjectCount, "agentCount": tc.overview.AgentCount, "activeRunCount": tc.overview.ActiveRunCount, "pendingGateCount": tc.overview.PendingGateCount} {
				number, ok := body[key].(json.Number)
				if !ok {
					t.Errorf("required overview counter %s is absent or is not a JSON number", key)
					continue
				}
				actual, err := number.Int64()
				if err != nil || actual != int64(want) {
					t.Errorf("overview counter %s differs from owner value", key)
				}
			}
			for _, key := range []string{"activeRuns", "pendingGates", "recentArtifacts"} {
				items, ok := body[key].([]any)
				if !ok || len(items) != 0 {
					t.Errorf("required overview list %s did not remain an empty array", key)
				}
			}
			if len(body) != 7 || !proto.Equal(before, response) {
				t.Fatal("overview projection fabricated fields or changed the upstream protobuf")
			}
		})
	}
}

// TestOverviewDefaultsPreserveOptionalScalars checks other message descriptors through the same
// HTTP writer, preventing required Overview zero values from becoming fabricated financial facts.
// TestOverviewDefaultsPreserveOptionalScalars проверяет другие descriptors тем же HTTP writer:
// обязательные нули Overview не должны создавать вымышленные финансовые сведения.
func TestOverviewDefaultsPreserveOptionalScalars(t *testing.T) {
	for _, message := range []proto.Message{&cp.ProviderFinancialWindow{}, &cp.ProviderFinancialCredits{}} {
		writer := httptest.NewRecorder()
		writeReadRPCResult(writer, message, nil, "", "")
		var body map[string]any
		if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &body) != nil || len(body) != 0 {
			t.Fatal("optional financial scalar absence was replaced with fabricated data")
		}
	}
}
