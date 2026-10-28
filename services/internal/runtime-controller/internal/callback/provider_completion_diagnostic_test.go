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

// completionDiagnostic builds an exact-bound synthetic notification-failure
// observation for callback tests; it is not evidence from a running provider.
// completionDiagnostic собирает точно привязанное синтетическое наблюдение
// ошибки уведомления для callback-тестов, не свидетельство работающего провайдера.
func completionDiagnostic(input runtimecontract.RunnerInput) *runtimecontract.ProviderFailureDiagnostic {
	return &runtimecontract.ProviderFailureDiagnostic{Schema: runtimecontract.ProviderFailureDiagnosticSchema,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, ExecutionBindingDigest: input.ExecutionBindingDigest,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
		Kind: "REQUEST_FAILURE", Stage: "TERMINAL_WAIT", Class: "PROVIDER", Detail: "NOTIFICATION_INVALID",
		Notification: "thread/tokenUsage/updated", NotificationError: "TOKEN_USAGE_LAST_EXCEEDS_TOTAL", AccountRead: "NONE"}
}

// diagnosticCompletionClient is a typed completion RPC spy with a pre-call
// assertion hook and configurable result; embedded unexercised RPCs are not fakes.
// diagnosticCompletionClient записывает типизированные completion RPC с hook
// проверки до записи и заданным результатом; остальные встроенные RPC не подменены.
type diagnosticCompletionClient struct {
	cp.RuntimeWorkServiceClient
	err      error
	requests []*cp.CompleteExecutionRequest
	before   func()
}

// CompleteExecution invokes the required hook, retains the request and returns
// an empty response plus the configured error without committing owner state.
// CompleteExecution вызывает обязательный hook, сохраняет запрос и возвращает
// пустой ответ с заданной ошибкой, не фиксируя состояние владельца.
func (client *diagnosticCompletionClient) CompleteExecution(_ context.Context, request *cp.CompleteExecutionRequest, _ ...grpc.CallOption) (*cp.CompleteExecutionResponse, error) {
	client.before()
	client.requests = append(client.requests, request)
	return &cp.CompleteExecutionResponse{}, client.err
}

// TestProviderCompletionDiagnosticOwnerCommitBeforeCleanup checks local route
// ordering: a nil mock owner result permits one safe pre-cleanup observation,
// whereas replay, failure, success and rejected bindings do not fabricate it.
// TestProviderCompletionDiagnosticOwnerCommitBeforeCleanup проверяет порядок
// локального маршрута: nil от mock владельца допускает одно безопасное наблюдение
// до очистки; повтор, ошибка, успех и неверные привязки не подделывают его.
func TestProviderCompletionDiagnosticOwnerCommitBeforeCleanup(t *testing.T) {
	for _, scenario := range []string{"exact", "interrupted", "workspace", "workspace mismatched code", "missing", "CP error", "replay", "success", "unauthorized", "input", "execution", "session", "turn", "attempt", "private enum"} {
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
				Usage: runtimecontract.TokenUsage{Completeness: runtimecontract.UsagePartial, TotalTokens: 3, InputTokens: 2, OutputTokens: 1}, ProviderDiagnostic: completionDiagnostic(input)}
			wantStatus, commits := http.StatusNoContent, 1
			switch scenario {
			case "interrupted":
				payload.ProviderDiagnostic.Detail = "CONTEXT_CANCELLED"
				payload.ProviderDiagnostic.Notification, payload.ProviderDiagnostic.NotificationError = "NONE", "NONE"
				payload.SafeErrorCode, payload.ResultSummary = "RUNTIME_UNAVAILABLE", "i18n:RUNTIME_INTERRUPTED"
			case "workspace", "workspace mismatched code":
				payload.ProviderDiagnostic.Kind = "TERMINAL_FAILURE"
				payload.ProviderDiagnostic.Stage = "WORKSPACE_NORMALIZATION"
				payload.ProviderDiagnostic.Class = "WORKSPACE"
				payload.ProviderDiagnostic.Detail = "FILE_TYPE_INVALID"
				payload.ProviderDiagnostic.TerminalCode = "RUNTIME_ARTIFACT_INVALID"
				payload.ProviderDiagnostic.Notification, payload.ProviderDiagnostic.NotificationError = "NONE", "NONE"
				if scenario == "workspace" {
					payload.SafeErrorCode = "RUNTIME_ARTIFACT_INVALID"
				} else {
					wantStatus, commits = http.StatusBadRequest, 0
				}
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
				if (scenario == "exact" || scenario == "workspace") && !strings.Contains(logs.String(), `"diagnostic_status":"OBSERVED"`) {
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
			if scenario == "exact" || scenario == "interrupted" || scenario == "workspace" || scenario == "missing" {
				if !deleted || strings.Count(logs.String(), providerCompletionDiagnosticLog) != 1 {
					t.Fatal("commit did not produce one pre-cleanup observation")
				}
				if scenario == "workspace" && (!strings.Contains(logs.String(), `"provider_class":"WORKSPACE"`) || !strings.Contains(logs.String(), `"provider_detail":"FILE_TYPE_INVALID"`)) {
					t.Fatal("workspace cause did not survive owner commit before pod cleanup")
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
				if got.GetMutation().GetIdempotencyKey() != stableKey(input.LeaseRef, "complete") || got.GetLeaseRef() != input.LeaseRef || got.GetFence() != input.LeaseFence || got.GetGeneration() != input.LeaseGeneration {
					t.Fatal("completion changed the existing owner intent scope")
				}
				if got.GetSafeErrorCode() != payload.SafeErrorCode || got.GetResultSummary() != payload.ResultSummary || got.GetUsage().GetTotalTokens() != 3 || got.GetUsage().GetCompleteness() != cp.UsageCompleteness_USAGE_COMPLETENESS_PARTIAL || got.GetCodexArchiveSha256() != "" {
					t.Fatal("diagnostic changed CP state payload")
				}
			}
		})
	}
}

// TestProviderCompletionDiagnosticDirectUnknownCannotReflectPrivateValues
// verifies invalid provider details become unavailable rather than reflected,
// and a nonnil owner error suppresses the observation entirely.
// TestProviderCompletionDiagnosticDirectUnknownCannotReflectPrivateValues
// проверяет замену неверных подробностей провайдера на недоступность без их
// отражения, а также полное подавление наблюдения при ненулевой ошибке владельца.
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
