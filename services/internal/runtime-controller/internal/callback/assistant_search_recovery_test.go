package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type assistantSearchRecoveryClient struct {
	*assistantSearchDiagnosticClient
	terminalFailure bool
}

func (client *assistantSearchRecoveryClient) RecordRunToolCall(ctx context.Context, request *controlplanev1.RecordRunToolCallRequest, options ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	if client.terminalFailure && request.GetRevision() == 2 {
		return nil, status.Error(codes.Unavailable, "PRIVATE_PROJECTION_ERROR")
	}
	return client.assistantFreshCatalogMCPClient.RecordRunToolCall(ctx, request, options...)
}

func TestAssistantSearchInvalidInputIsRecoverableWithoutOwnerRead(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, arguments := range []map[string]any{
			{}, {"query": ""}, {"query": " \t\n "}, {"query": " x "}, {"query": 42},
			{"query": strings.Repeat("я", 161)}, {"query": "PRIVATE_QUERY", "PRIVATE_FIELD": "PRIVATE_VALUE"},
		} {
			input, _, _ := assistantOwnCurrentFixture(scope)
			client := &assistantSearchRecoveryClient{assistantSearchDiagnosticClient: &assistantSearchDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{}}}
			var logs bytes.Buffer
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			params, _ := json.Marshal(map[string]any{"name": "find_platform_resources", "arguments": arguments})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"search-input"`), Params: params}, input)
			var response struct {
				Result struct {
					IsError    bool           `json:"isError"`
					Structured map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || !response.Result.IsError ||
				response.Result.Structured["error_code"] != "SEARCH_INPUT_INVALID" || response.Result.Structured["retryable"] != true ||
				!strings.Contains(response.Result.Structured["guidance"].(string), "Retry at most once") || client.calls != 0 ||
				client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.projection.GetSafeResult() != "TOOL_UNAVAILABLE" {
				t.Fatal("local search validation was not recoverable or changed the safe owner projection")
			}
			for _, private := range []string{"PRIVATE_QUERY", "PRIVATE_FIELD", "PRIVATE_VALUE", input.LeaseFence, input.Instructions, input.Task} {
				if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(recorder.Body.String(), private)) {
					t.Fatal("search recovery disclosed private inputs")
				}
			}
			client.terminalFailure = true
			recorder = httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"search-projection-failure"`), Params: params}, input)
			if !strings.Contains(recorder.Body.String(), `"error_code":"TOOL_UNAVAILABLE"`) || strings.Contains(recorder.Body.String(), `"retryable":true`) || strings.Contains(recorder.Body.String(), "PRIVATE_PROJECTION_ERROR") {
				t.Fatal("projection failure was misreported as safely recoverable input")
			}
		}
	}
}
