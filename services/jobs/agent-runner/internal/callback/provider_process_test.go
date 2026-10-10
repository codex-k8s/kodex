package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestProviderProcessCallbackUsesExistingExactProgressAndStableRetry(t *testing.T) {
	input := validWarmTurnFixture()
	value := runtimecontract.BindProviderProcessObservation(input, "0.160.0")
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/executions/"+input.LeaseRef+"/progress" || request.Header.Get("X-Kodex-Callback-Method") != "progress" || request.Header.Get("X-Kodex-Session-Ref") != input.SessionRef || request.Header.Get("X-Kodex-Turn-Ref") != input.TurnRef || request.Header.Get("Authorization") != "Bearer ticket" {
			t.Error("new authority or incorrect execution route")
		}
		raw, _ := io.ReadAll(request.Body)
		bodies = append(bodies, raw)
		var payload runtimecontract.RunnerProgressRequest
		if json.Unmarshal(raw, &payload) != nil || payload.ProviderProcess == nil || !payload.ProviderProcess.Matches(input) || payload.Progress != runtimecontract.ProviderProcessInitializedProgress {
			t.Error("typed observed process binding lost")
		}
		if len(bodies) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
		} else {
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond}}
	if client.ProviderProcess(context.Background(), input, value) != nil || len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("bounded retry changed observation")
	}
	value.Attempt++
	if client.ProviderProcess(context.Background(), input, value) == nil || len(bodies) != 2 {
		t.Fatal("foreign observation reached callback transport")
	}
}
