package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
)

type financialProgressClient struct {
	cp.RuntimeWorkServiceClient
	requests []*cp.ReportExecutionProgressRequest
}

func (client *financialProgressClient) ReportExecutionProgress(_ context.Context, request *cp.ReportExecutionProgressRequest, _ ...grpc.CallOption) (*cp.ReportExecutionProgressResponse, error) {
	client.requests = append(client.requests, request)
	return &cp.ReportExecutionProgressResponse{}, nil
}

func TestFinancialHTTPCallbackPreservesBindingPresenceAndRejectsMalformed(t *testing.T) {
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t)
	client := &financialProgressClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, manager: manager,
		control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	digest, _ := json.Marshal(input.RuntimeRevisionDigest)
	prefix := `{"runtime_revision_digest":` + string(digest) + `,"financial":`
	valid := `{"sequence":1,"state":"AVAILABLE","windows":[{"usedPercent":100}],"credits":{"hasCredits":true,"balance":"59999.000000000000000001"}}`
	for _, candidate := range []struct {
		name, observation, suffix string
		want                      int
	}{
		{"valid", valid, "", http.StatusNoContent},
		{"duplicate", `{"sequence":1,"sequence":2,"state":"AVAILABLE"}`, "", http.StatusBadRequest},
		{"unknown", `{"sequence":1,"state":"AVAILABLE","paid":true}`, "", http.StatusBadRequest},
		{"mixed", valid, `,"progress":"runtime.running"`, http.StatusBadRequest},
		{"invalid window", `{"sequence":1,"state":"AVAILABLE","windows":[{"usedPercent":101}]}`, "", http.StatusBadRequest},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/progress",
				bytes.NewBufferString(prefix+candidate.observation+candidate.suffix+`}`))
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "progress")
			response := httptest.NewRecorder()
			server.route(response, request)
			if response.Code != candidate.want {
				t.Fatalf("callback status = %d, want %d", response.Code, candidate.want)
			}
		})
	}
	if len(client.requests) != 1 {
		t.Fatalf("malformed callback reached control-plane: calls=%d", len(client.requests))
	}
	request := client.requests[0]
	observation := request.GetFinancial()
	if request.GetLeaseRef() != input.LeaseRef || request.GetFence() != input.LeaseFence ||
		request.GetGeneration() != input.LeaseGeneration || request.GetProgress() != "" ||
		observation.GetSequence() != 1 || len(observation.GetWindows()) != 1 ||
		observation.Windows[0].GetUsedPercent() != 100 || observation.Windows[0].ResetsAt != nil ||
		observation.GetCredits().GetBalance() != "59999.000000000000000001" ||
		observation.GetCredits().Unlimited != nil {
		t.Fatal("callback lost private binding, decimal precision or field presence")
	}
}
