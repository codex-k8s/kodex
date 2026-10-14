package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func completionDiagnostic(input runtimecontract.RunnerInput) *runtimecontract.ProviderFailureDiagnostic {
	return &runtimecontract.ProviderFailureDiagnostic{Schema: runtimecontract.ProviderFailureDiagnosticSchema,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, ExecutionBindingDigest: input.ExecutionBindingDigest,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
		Kind: "REQUEST_FAILURE", Stage: "TERMINAL_WAIT", Class: "PROVIDER", Detail: "NOTIFICATION_INVALID",
		Notification: "thread/tokenUsage/updated", NotificationError: "TOKEN_USAGE_LAST_EXCEEDS_TOTAL", AccountRead: "NONE"}
}

type diagnosticCompletionClient struct {
	cp.RuntimeWorkServiceClient
	err      error
	requests []*cp.CompleteExecutionRequest
	before   func()
}

func (client *diagnosticCompletionClient) CompleteExecution(_ context.Context, request *cp.CompleteExecutionRequest, _ ...grpc.CallOption) (*cp.CompleteExecutionResponse, error) {
	client.before()
	client.requests = append(client.requests, request)
	return &cp.CompleteExecutionResponse{}, client.err
}

func TestProviderCompletionDiagnosticOwnerCommitBeforeCleanup(t *testing.T) {
	for _, scenario := range []string{"exact", "missing", "CP error", "replay", "success", "unauthorized", "input", "execution", "session", "turn", "attempt", "private enum"} {
		t.Run(scenario, func(t *testing.T) {
			manager, kube, input, _, ticket := providerCredentialRefreshRouteFixture(t)
			var logs bytes.Buffer
			client := &diagnosticCompletionClient{before: func() {
				if logs.Len() != 0 {
					t.Error("diagnostic claimed commit before owner proof")
				}
			}}
			if scenario == "CP error" {
				client.err = status.Error(codes.Unavailable, "PRIVATE_OWNER_SENTINEL")
			}
			if scenario == "replay" {
				client.err = status.Error(codes.AlreadyExists, "PRIVATE_OWNER_SENTINEL")
			}
			payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt,
				SafeErrorCode: "PROVIDER_UNAVAILABLE", ResultSummary: "PRIVATE_SUMMARY_SENTINEL",
				Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}, ProviderDiagnostic: completionDiagnostic(input)}
			wantStatus, commits := http.StatusNoContent, 1
			switch scenario {
			case "missing":
				payload.ProviderDiagnostic = nil
			case "success":
				payload.Success, payload.SafeErrorCode, payload.ProviderDiagnostic = true, "", nil
			case "unauthorized":
				ticket = strings.Repeat("0", 64)
				wantStatus, commits = http.StatusNotFound, 0
			case "CP error":
				wantStatus = http.StatusServiceUnavailable
			case "input":
				payload.ProviderDiagnostic.InputDigest = strings.Repeat("f", 64)
				wantStatus, commits = http.StatusBadRequest, 0
			case "execution":
				payload.ProviderDiagnostic.ExecutionBindingDigest = strings.Repeat("f", 64)
				wantStatus, commits = http.StatusBadRequest, 0
			case "session":
				payload.ProviderDiagnostic.SessionRef = "ses_foreign"
				wantStatus, commits = http.StatusBadRequest, 0
			case "turn":
				payload.ProviderDiagnostic.TurnRef = "trn_foreign"
				wantStatus, commits = http.StatusBadRequest, 0
			case "attempt":
				payload.ProviderDiagnostic.Attempt++
				wantStatus, commits = http.StatusBadRequest, 0
			case "private enum":
				payload.ProviderDiagnostic.Detail = "PRIVATE_DIAGNOSTIC_SENTINEL"
				wantStatus, commits = http.StatusBadRequest, 0
			}
			deleted := false
			kube.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
				deleted = true
				if scenario == "exact" && !strings.Contains(logs.String(), `"diagnostic_status":"OBSERVED"`) {
					t.Error("cleanup lost failure observation")
				}
				return false, nil, nil
			})
			server := &Server{manager: manager, coordinator: NewCoordinator(), config: Config{RequestTimeout: time.Second},
				control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			raw, _ := json.Marshal(payload)
			request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/complete", bytes.NewReader(raw))
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "complete")
			response := httptest.NewRecorder()
			server.route(response, request)
			if response.Code != wantStatus || len(client.requests) != commits {
				t.Fatalf("status=%d commits=%d", response.Code, len(client.requests))
			}
			if scenario == "exact" || scenario == "missing" {
				if !deleted || strings.Count(logs.String(), providerCompletionDiagnosticLog) != 1 {
					t.Fatal("commit did not produce one pre-cleanup observation")
				}
				if scenario == "missing" && !strings.Contains(logs.String(), `"diagnostic_status":"UNAVAILABLE"`) {
					t.Fatal("absence fabricated a captured diagnostic")
				}
			} else if strings.Contains(logs.String(), providerCompletionDiagnosticLog) {
				t.Fatal("non-commit or success acquired failure observation")
			}
			if (scenario == "CP error" || commits == 0) && deleted {
				t.Fatal("failed completion changed cleanup behavior")
			}
			for _, secret := range []string{"PRIVATE_SUMMARY_SENTINEL", "PRIVATE_OWNER_SENTINEL", "PRIVATE_DIAGNOSTIC_SENTINEL", ticket, input.LeaseFence} {
				if secret != "" && strings.Contains(logs.String(), secret) {
					t.Fatal("failure observation disclosed private data")
				}
			}
			if commits == 1 {
				got := client.requests[0]
				if got.GetSafeErrorCode() != payload.SafeErrorCode || got.GetResultSummary() != payload.ResultSummary || got.GetUsage().GetTotalTokens() != 3 || got.GetCodexArchiveSha256() != "" {
					t.Fatal("diagnostic changed CP state payload")
				}
			}
		})
	}
}

func TestProviderCompletionDiagnosticDirectUnknownCannotReflectPrivateValues(t *testing.T) {
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "nod_fixture", SessionRef: "ses_fixture", TurnRef: "trn_fixture", Attempt: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64)}
	var logs bytes.Buffer
	server := &Server{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	diagnostic := completionDiagnostic(input)
	diagnostic.Detail = "PRIVATE_SENTINEL"
	server.logCommittedProviderDiagnostic(t.Context(), input, runtimecontract.RunnerCompletionRequest{ProviderDiagnostic: diagnostic}, nil)
	if !strings.Contains(logs.String(), `"diagnostic_status":"UNAVAILABLE"`) || strings.Contains(logs.String(), "PRIVATE_SENTINEL") {
		t.Fatal("untrusted diagnostic reflected private data")
	}
	logs.Reset()
	server.logCommittedProviderDiagnostic(t.Context(), input, runtimecontract.RunnerCompletionRequest{}, errors.New("PRIVATE_SENTINEL"))
	if logs.Len() != 0 {
		t.Fatal("owner failure became a commit observation")
	}
}
