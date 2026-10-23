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
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

// financialProgressClient records financial-progress RPC requests without applying
// provider-account balance, limit or admission state changes.
// financialProgressClient записывает financial-progress RPC requests без изменения
// provider-account balance,limit и admission state.
type financialProgressClient struct {
	cp.RuntimeWorkServiceClient
	requests []*cp.ReportExecutionProgressRequest
}

// ReportExecutionProgress stores the exact request pointer and returns an empty
// successful fake response; no account observation is persisted.
// ReportExecutionProgress сохраняет exact request pointer и выдаёт empty successful
// fake response без сохранения account observation.
func (client *financialProgressClient) ReportExecutionProgress(_ context.Context, request *cp.ReportExecutionProgressRequest, _ ...grpc.CallOption) (*cp.ReportExecutionProgressResponse, error) {
	client.requests = append(client.requests, request)
	return &cp.ReportExecutionProgressResponse{}, nil
}

// TestFinancialHTTPCallbackPreservesBindingPresenceAndRejectsMalformed checks local
// HTTP forwards exactly one valid financial observation with lease binding, optional
// presence and decimal precision; duplicate/aliased/unknown/mixed/invalid JSON stays
// rejected before owner RPC, without invoking or billing a real provider.
// TestFinancialHTTPCallbackPreservesBindingPresenceAndRejectsMalformed проверяет передачу
// local HTTP ровно одного valid financial observation с lease binding,optional presence
// и decimal precision; duplicate/aliased/unknown/mixed/invalid JSON отвергается
// до owner RPC без вызова/биллинга реального provider.
func TestFinancialHTTPCallbackPreservesBindingPresenceAndRejectsMalformed(t *testing.T) {
	manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t)
	client := &financialProgressClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, manager: manager,
		control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	digest, _ := json.Marshal(input.RuntimeRevisionDigest)
	prefix := `{"runtime_revision_digest":` + string(digest) + `,"financial":`
	valid := `{"sequence":1,"state":"AVAILABLE","windows":[{"usedPercent":100}],"credits":{"hasCredits":true,"balance":"59999.000000000000000001"}}`
	process, err := json.Marshal(runtimecontract.BindProviderProcessObservation(input, "0.160.0"))
	if err != nil {
		t.Fatal("provider process fixture failed")
	}
	for _, candidate := range []struct {
		name, observation, suffix string
		want                      int
	}{
		{"valid", valid, "", http.StatusNoContent},
		{"duplicate", `{"sequence":1,"sequence":2,"state":"AVAILABLE"}`, "", http.StatusBadRequest},
		{"duplicate payload", valid, `,"financial":` + valid, http.StatusBadRequest},
		{"aliased sequence", `{"sequence":1,"Sequence":2,"state":"AVAILABLE"}`, "", http.StatusBadRequest},
		{"aliased credits", `{"sequence":1,"state":"AVAILABLE","credits":{"Balance":"59999"}}`, "", http.StatusBadRequest},
		{"aliased payload", valid, `,"Financial":` + valid, http.StatusBadRequest},
		{"unknown", `{"sequence":1,"state":"AVAILABLE","paid":true}`, "", http.StatusBadRequest},
		{"mixed", valid, `,"progress":"runtime.running"`, http.StatusBadRequest},
		{"mixed process", valid, `,"provider_process":` + string(process), http.StatusBadRequest},
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
