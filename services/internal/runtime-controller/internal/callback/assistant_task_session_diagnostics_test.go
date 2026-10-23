package callback

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTaskSessionOwnerRPCDiagnosticPreservesSanitizedCode checks recognized remote
// codes survive while unknown numeric codes become Unknown, with fixed text and no
// remote details/unwrap chain; no terminal success or retry authority is invented.
// TestTaskSessionOwnerRPCDiagnosticPreservesSanitizedCode проверяет сохранение recognized
// remote codes и замену unknown numeric на Unknown с fixed text без remote details/
// unwrap chain; terminal success и retry authority не создаются.
func TestTaskSessionOwnerRPCDiagnosticPreservesSanitizedCode(t *testing.T) {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	for _, code := range []codes.Code{codes.Aborted, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Internal, codes.Unknown, codes.Code(123)} {
		remote, _ := status.New(code, "PRIVATE_OWNER_MESSAGE").WithDetails(&errdetails.ErrorInfo{Reason: "PRIVATE_REMOTE_REASON", Metadata: map[string]string{"private": "PRIVATE_REMOTE_DETAILS"}})
		client := &assistantSearchDiagnosticClient{failure: remote.Err()}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
		result, err := server.readTaskSession(t.Context(), input, map[string]any{"run_ref": "run_selected123"})
		want := code
		if code == codes.Code(123) {
			want = codes.Unknown
		}
		if result != nil || status.Code(err) != want || client.calls != 1 ||
			err.Error() != errAssistantTaskSessionRead.Error() || len(status.Convert(err).Details()) != 0 ||
			safeToolCallResult("read_task_session", result, err) != "TOOL_UNAVAILABLE" {
			t.Fatal("owner rejection lost safe code or changed tool outcome")
		}
		if errors.Unwrap(err) != nil {
			t.Fatal("remote error chain escaped diagnostic")
		}
	}
}

// taskDiagnosticCursor encodes a synthetic versioned source/binding/offset cursor
// for progress/mismatch tests; base64 text is not a signed owner capability.
// taskDiagnosticCursor кодирует synthetic versioned source/binding/offset cursor
// для progress/mismatch tests; base64 text не является signed owner capability.
func taskDiagnosticCursor(source, binding string, offset int) string {
	raw, _ := json.Marshal(controlplaneapi.TaskSessionCursor{Version: 1, Binding: binding, Source: source, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// TestTaskSessionDiagnosticClosedStagesAndGuards exercises eleven guard/projection/
// binding/cursor failure paths and expected owner-call counts, checks local codes stay
// Unknown and rejects unknown/plain/typed-nil markers without private reflection.
// TestTaskSessionDiagnosticClosedStagesAndGuards проверяет одиннадцать guard/projection/
// binding/cursor failure paths и owner-call counts,Unknown local codes и отказ unknown/
// plain/typed-nil markers без private reflection.
func TestTaskSessionDiagnosticClosedStagesAndGuards(t *testing.T) {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeProject)
	input.ProjectRef = callbackTaskSessionPage().ProjectRef
	for _, scenario := range []string{"context", "shape", "locator", "cursor input", "reply", "projection", "binding", "cursor shape", "cursor source", "cursor progress", "cursor binding"} {
		t.Run(scenario, func(t *testing.T) {
			current := input
			args := map[string]any{"run_ref": "run_selected123"}
			response := &cp.SearchAssistantResourcesResponse{AssistantTaskSession: callbackTaskSessionPage()}
			want, calls := taskSessionInput, 0
			switch scenario {
			case "context":
				current = runtimecontract.RunnerInput{}
				want = taskSessionContext
			case "shape":
				args["PRIVATE_ARGUMENT"] = "PRIVATE_VALUE"
			case "locator":
				args["run_ref"] = "../PRIVATE_VALUE"
			case "cursor input":
				args["cursor"] = 3
				want = taskSessionInputCursor
			case "reply":
				response.Results = []*cp.SearchResult{{Ref: "PRIVATE_RESULT"}}
				want, calls = taskSessionReply, 1
			case "projection":
				response.AssistantTaskSession.Messages[0].Text = "PRIVATE_VALUE"
				want, calls = taskSessionProjection, 1
			case "binding":
				response.AssistantTaskSession.RunRef = "run_foreign123"
				_ = controlplaneapi.SealTaskSessionPage(response.AssistantTaskSession)
				want, calls = taskSessionBinding, 1
			case "cursor shape":
				args["cursor"] = "PRIVATE_CURSOR"
				want, calls = taskSessionCursorShape, 1
			case "cursor source":
				args["cursor"] = taskDiagnosticCursor(strings.Repeat("c", 64), strings.Repeat("b", 64), 10)
				want, calls = taskSessionCursorSource, 1
			case "cursor progress", "cursor binding":
				page := response.AssistantTaskSession
				binding := strings.Repeat("b", 64)
				args["cursor"] = taskDiagnosticCursor(page.SourceSha256, binding, 10)
				next := 12
				if scenario == "cursor binding" {
					binding, next = strings.Repeat("c", 64), 11
				}
				page.Truncated, page.NextCursor = true, taskDiagnosticCursor(page.SourceSha256, binding, next)
				_ = controlplaneapi.SealTaskSessionPage(page)
				want, calls = taskSessionCursorProgress, 1
			}
			client := &assistantSearchDiagnosticClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			result, err := server.readTaskSession(t.Context(), current, args)
			stage, code, ok := taskSessionFailureDetails(err)
			if result != nil || !ok || stage != want || code != codes.Unknown || status.Code(err) != codes.Unknown ||
				!errors.Is(err, errAssistantTaskSessionRead) || client.calls != calls || safeToolCallResult("read_task_session", result, err) != "TOOL_UNAVAILABLE" {
				t.Fatal("rejection changed guards or lost closed stage")
			}
			if strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("stage reflected private data")
			}
		})
	}
	for _, err := range []error{errors.New("PRIVATE_MESSAGE"), status.Error(codes.Unknown, taskSessionOwnerRPC),
		&taskSessionReadError{stage: "PRIVATE_STAGE"}, (*taskSessionReadError)(nil)} {
		if stage, _, ok := taskSessionFailureDetails(err); ok || stage != "" {
			t.Fatal("unknown marker supplied diagnostic stage")
		}
	}
}

// TestTaskSessionDiagnosticPreservesPagedSuccess checks both assistant scopes and
// terminal/continuation pages preserve exact public JSON and forwarded cursor after
// adding sanitized failure diagnostics; all owner responses remain synthetic.
// TestTaskSessionDiagnosticPreservesPagedSuccess проверяет прежние exact public JSON
// и forwarded cursor обоих assistant scopes/terminal-continuation pages при sanitized
// failure diagnostics; owner responses синтетические.
func TestTaskSessionDiagnosticPreservesPagedSuccess(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input, _, _ := assistantOwnCurrentFixture(scope)
		input.ProjectRef = callbackTaskSessionPage().ProjectRef
		for _, more := range []bool{false, true} {
			page := callbackTaskSessionPage()
			binding := strings.Repeat("b", 64)
			args := map[string]any{"run_ref": page.RunRef, "cursor": taskDiagnosticCursor(page.SourceSha256, binding, 10)}
			if more {
				page.Truncated, page.NextCursor = true, taskDiagnosticCursor(page.SourceSha256, binding, 11)
			}
			if controlplaneapi.SealTaskSessionPage(page) != nil {
				t.Fatal("synthetic page invalid")
			}
			client := &assistantSearchDiagnosticClient{response: &cp.SearchAssistantResourcesResponse{AssistantTaskSession: page}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			result, err := server.readTaskSession(t.Context(), input, args)
			expected, _ := controlplaneapi.ProjectTaskSessionPage(page)
			before, _ := json.Marshal(taskSessionToolResult{expected})
			after, _ := json.Marshal(result)
			if err != nil || !bytes.Equal(before, after) || client.calls != 1 || client.request.AssistantTaskSessionRead.Cursor != args["cursor"] {
				t.Fatal("diagnostic altered success bytes or cursor")
			}
		}
	}
}

// TestTaskSessionDiagnosticCannotLogUnverifiedPins checks invalid refs/digests/attempt
// yield only UNAVAILABLE provenance and do not expose canaries or run refs in logs.
// It tests structural fallback, not proof that arbitrary caller input is authenticated.
// TestTaskSessionDiagnosticCannotLogUnverifiedPins проверяет лишь UNAVAILABLE provenance
// invalid refs/digests/attempt без canaries/run refs в logs. Проверяется structural
// fallback,не аутентификация произвольного caller input.
func TestTaskSessionDiagnosticCannotLogUnverifiedPins(t *testing.T) {
	input := runtimecontract.RunnerInput{RunRef: "PRIVATE_INVALID_REF!", NodeRef: "nod_fixture123", SessionRef: "ses_fixture123", TurnRef: "trn_fixture123", Attempt: 1,
		LeaseRef: "PRIVATE_FENCE", RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64)}
	for _, mutate := range []func(*runtimecontract.RunnerInput){
		func(*runtimecontract.RunnerInput) {},
		func(i *runtimecontract.RunnerInput) {
			i.RunRef, i.InputDigest = "run_fixture123", "PRIVATE_INVALID_DIGEST"
		},
		func(i *runtimecontract.RunnerInput) { i.RunRef, i.Attempt = "run_fixture123", 0 },
	} {
		current := input
		mutate(&current)
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		logger.Warn("safe fixture", taskSessionFailureAttributes(current, json.RawMessage(`"PRIVATE_ID"`), taskSessionOwnerRPC)...)
		if strings.Contains(logs.String(), "PRIVATE_") || !strings.Contains(logs.String(), `"diagnostic_binding":"UNAVAILABLE"`) || strings.Contains(logs.String(), "run_ref") {
			t.Fatal("unverified input acquired provenance or reflected private data")
		}
	}
}

// taskDiagnosticMCPClient adds injectable activity rejection to the search fake so
// tests distinguish authorization/audit failure from actual task-session handler work.
// taskDiagnosticMCPClient добавляет injectable activity rejection к search fake
// для различения authorization/audit failure и task-session handler work.
type taskDiagnosticMCPClient struct {
	*assistantSearchDiagnosticClient
	rejectRecorder bool
}

// RecordRunToolCall either returns a private-canary Unavailable or records the
// request and supplies a synthetic event locator; it does not commit durable audit.
// RecordRunToolCall возвращает private-canary Unavailable либо записывает request
// и выдаёт synthetic event locator без durable audit commit.
func (client *taskDiagnosticMCPClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	if client.rejectRecorder {
		return nil, status.Error(codes.Unavailable, "PRIVATE_RECORDER_MESSAGE")
	}
	client.projection = request
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_synthetic123"}}, nil
}

// TestTaskSessionDiagnosticAuthenticatedMCPBoundary checks local ticket/header/audit
// guards precede the handler, success adds no failure logs and one owner error produces
// only closed sanitized provenance tied to the activity call, without private cursor/
// selected-run/ticket/fence disclosure or corrective retry permission.
// TestTaskSessionDiagnosticAuthenticatedMCPBoundary проверяет local ticket/header/audit
// guards до handler,отсутствие failure logs успеха и closed sanitized provenance одного
// owner error с привязкой к activity call без private cursor/selected-run/ticket/fence
// disclosure и corrective retry permission.
func TestTaskSessionDiagnosticAuthenticatedMCPBoundary(t *testing.T) {
	for _, scenario := range []string{"owner", "unauthorized", "foreign header", "projection failed", "success"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
				input.AssistantScope = runtimecontract.AssistantScopeSystem
			})
			client := &taskDiagnosticMCPClient{assistantSearchDiagnosticClient: &assistantSearchDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{},
				failure: status.Error(codes.Aborted, "PRIVATE_OWNER_MESSAGE")}}
			if scenario == "success" {
				client.failure, client.response = nil, &cp.SearchAssistantResourcesResponse{AssistantTaskSession: callbackTaskSessionPage()}
			}
			if scenario == "projection failed" {
				client.rejectRecorder = true
			}
			var logs bytes.Buffer
			server := &Server{manager: manager, config: Config{RequestTimeout: time.Second},
				control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			body := []byte(`{"jsonrpc":"2.0","id":"safe-call","method":"tools/call","params":{"name":"read_task_session","arguments":{"run_ref":"run_selected123","cursor":"PRIVATE_CURSOR"}}}`)
			if scenario == "success" {
				body = bytes.Replace(body, []byte(`,"cursor":"PRIVATE_CURSOR"`), nil, 1)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/mcp", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "mcp")
			if scenario == "unauthorized" {
				request.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64))
			}
			if scenario == "foreign header" {
				request.Header.Set("X-Kodex-Run-Ref", "run_foreign123")
			}
			response := httptest.NewRecorder()
			server.route(response, request)
			if scenario == "projection failed" {
				if client.calls != 0 || strings.Contains(logs.String(), "task_session_stage") || !bytes.Contains(response.Body.Bytes(), []byte("Tool authorization unavailable")) {
					t.Fatal("recorder failure executed handler or forged observation")
				}
				return
			}
			if scenario == "unauthorized" || scenario == "foreign header" {
				if client.calls != 0 || logs.Len() != 0 || response.Code != http.StatusNotFound {
					t.Fatal("unverified execution acquired diagnostic provenance")
				}
				return
			}
			if scenario == "success" {
				if client.calls != 1 || logs.Len() != 0 || bytes.Contains(response.Body.Bytes(), []byte("TOOL_UNAVAILABLE")) {
					t.Fatal("success gained failure observation")
				}
				return
			}
			var record map[string]any
			if json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record) != nil {
				t.Fatal("expected one structured safe observation")
			}
			for key, want := range map[string]any{"task_session_stage": taskSessionOwnerRPC, "grpc_code": "Aborted",
				"failure_class": "assistant_task_session_owner_rpc", "diagnostic_binding": "AUTHENTICATED_INPUT",
				"run_ref": input.RunRef, "node_ref": input.NodeRef, "session_ref": input.SessionRef, "turn_ref": input.TurnRef,
				"attempt": float64(input.Attempt), "runtime_revision_digest": input.RuntimeRevisionDigest,
				"input_digest": input.InputDigest, "execution_binding_digest": input.ExecutionBindingDigest,
				"tool_call_ref": client.projection.CallRef} {
				if record[key] != want {
					t.Fatal("observation lost authenticated execution or tool receipt binding")
				}
			}
			if client.calls != 1 || client.projection.SafeResult != "TOOL_UNAVAILABLE" ||
				client.projection.State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || !bytes.Contains(response.Body.Bytes(), []byte(`"retryable":false`)) {
				t.Fatal("diagnostic altered terminal receipt or authorized a retry")
			}
			for _, private := range []string{"PRIVATE_OWNER_MESSAGE", "PRIVATE_CURSOR", "run_selected123", ticket, input.LeaseFence} {
				if strings.Contains(logs.String(), private) {
					t.Fatal("safe observation leaked request or credential")
				}
			}
		})
	}
}
