package callback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestOptionalFinancialDeliveryPreservesAuthorityDenial(t *testing.T) {
	for _, code := range []int{204, 403, 404, 409, 422, 500, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.URL.Path != "/v1/executions/lease_fixture/progress" {
					t.Error("wrong callback resource")
				}
				writer.WriteHeader(code)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := &Client{http: server.Client(), base: base, token: "fixture"}
			err := client.FinancialObservation(context.Background(), model.Input{LeaseRef: "lease_fixture"}, runtimecontract.ProviderFinancialObservation{Sequence: 1, State: "UNAVAILABLE"})
			wantError := code >= 400 && code < 500
			if (err != nil) != wantError {
				t.Fatalf("code %d authority/optional classification mismatch", code)
			}
			if calls != 1 {
				t.Fatal("optional observation was replayed")
			}
		})
	}
}
