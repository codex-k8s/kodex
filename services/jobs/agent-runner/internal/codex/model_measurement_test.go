package codex

import (
	"bytes"
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPrivateMeasurementBridgeRejectsForeignBindingsAndAmbiguousBodies exercises the real private handler without requiring provider UID or external authority.
// TestPrivateMeasurementBridgeRejectsForeignBindingsAndAmbiguousBodies проверяет настоящий private handler без provider UID или внешней authority.
func TestPrivateMeasurementBridgeRejectsForeignBindingsAndAmbiguousBodies(t *testing.T) {
	value := runtimecontract.ModelMeasurement{Ref: "request_synthetic01", Source: "HTTP_REQUEST", Phase: "NORMAL", Outcome: "FAILED", Completeness: "PARTIAL", DurationMS: new(int64(5))}
	raw, _ := json.Marshal(value)
	for _, test := range []struct {
		name, token, revision, path string
		body                        []byte
		want                        int
	}{
		{"valid", strings.Repeat("a", 64), strings.Repeat("b", 64), "/model-measurements", raw, 204},
		{"foreign bearer", strings.Repeat("c", 64), strings.Repeat("b", 64), "/model-measurements", raw, 400},
		{"old revision", strings.Repeat("a", 64), strings.Repeat("c", 64), "/model-measurements", raw, 400},
		{"query", strings.Repeat("a", 64), strings.Repeat("b", 64), "/model-measurements?target=foreign", raw, 400},
		{"duplicate", strings.Repeat("a", 64), strings.Repeat("b", 64), "/model-measurements", bytes.Replace(raw, []byte(`"source":"HTTP_REQUEST"`), []byte(`"source":"HTTP_REQUEST","source":"HTTP_REQUEST"`), 1), 400},
		{"oversized", strings.Repeat("a", 64), strings.Repeat("b", 64), "/model-measurements", bytes.Repeat([]byte("x"), 4097), 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("X-Kodex-Runtime-Revision", test.revision)
			response := httptest.NewRecorder()
			calls := 0
			serveModelMeasurement(response, request, strings.Repeat("a", 64), strings.Repeat("b", 64), func(activity runtimecontract.RuntimeActivity) error { calls++; return activity.Validate() })
			if response.Code != test.want || calls != map[bool]int{true: 1, false: 0}[test.want == 204] {
				t.Fatal("private observation binding failed")
			}
		})
	}
}
