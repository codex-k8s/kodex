package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

const openAPIInspectionSource = `openapi: 3.1.0
info: {title: Заявки, version: 1.0.0}
servers: [{url: https://api.example.test}]
paths:
  /health:
    get:
      operationId: getHealth
      responses: {'200': {description: OK}}
  /tickets:
    post:
      operationId: createTicket
      responses:
        '200':
          description: Created
          content:
            text/plain:
              schema: {type: string}
`

func TestOpenAPIInspectionIsBoundedLocalPreview(t *testing.T) {
	handler := generated.Handler(&Server{})
	path := "/api/v1/integration-definition-configurations/openapi-inspections"
	for _, tc := range []struct {
		name, source   string
		want           int
		firstCandidate bool
	}{
		{name: "supported and unsupported", source: openAPIInspectionSource, want: http.StatusOK, firstCandidate: true},
		{name: "insecure origin", source: strings.Replace(openAPIInspectionSource, "https://api.example.test", "http://api.example.test", 1), want: http.StatusOK},
		{name: "oversized", source: strings.Repeat("x", 128<<10+1), want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"source": tc.source})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, managedTestRequest(http.MethodPost, path, string(body)))
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
			}
			if tc.want != http.StatusOK {
				if strings.Contains(w.Body.String(), "api.example.test") {
					t.Fatal("invalid source leaked through problem response")
				}
				return
			}
			var result generated.OpenAPIInspectionResult
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Digest) != 64 ||
				result.Title != "Заявки" || len(result.Operations) != 2 ||
				result.Operations[0].Candidate != tc.firstCandidate || result.Operations[0].HealthCandidate != tc.firstCandidate || result.Operations[1].Candidate || result.Operations[1].HealthCandidate {
				t.Fatalf("unexpected inspection response: %s", w.Body.String())
			}
			if strings.Contains(w.Body.String(), "openapi: 3.1.0") {
				t.Fatal("raw source was returned")
			}
		})
	}
}
