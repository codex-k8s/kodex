package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"reflect"
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

type assistantSearchDiagnosticClient struct {
	*assistantFreshCatalogMCPClient
	response *controlplanev1.SearchAssistantResourcesResponse
	failure  error
	calls    int
	request  *controlplanev1.SearchAssistantResourcesRequest
}

func (client *assistantSearchDiagnosticClient) SearchAssistantResources(_ context.Context, request *controlplanev1.SearchAssistantResourcesRequest, _ ...grpc.CallOption) (*controlplanev1.SearchAssistantResourcesResponse, error) {
	client.calls++
	client.request = request
	return client.response, client.failure
}

func TestAssistantSearchDiagnosticsDoNotTrustUnregisteredStages(t *testing.T) {
	for _, err := range []error{
		errors.New("PRIVATE_UNKNOWN_ERROR"),
		&assistantSearchError{class: "PRIVATE_UNKNOWN_STAGE"},
		(*assistantSearchError)(nil),
		status.Error(codes.Unknown, assistantSearchQueryInvalid),
	} {
		if assistantSearchFailureClass(err) != "" || controlFailureClass(err) != "control_unknown" {
			t.Fatal("unregistered search stage escaped the closed diagnostic fallback")
		}
	}
}

func TestAssistantSearchDiagnosticsPreserveSystemAndProjectSuccess(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input, _, _ := assistantOwnCurrentFixture(scope)
		for _, query := range []string{"яя", strings.Repeat("я", 160)} {
			client := &assistantSearchDiagnosticClient{response: &controlplanev1.SearchAssistantResourcesResponse{}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			result, err := server.findPlatformResources(t.Context(), input, map[string]any{"query": query})
			if err != nil || result == nil || client.calls != 1 || client.request.GetQuery() != query ||
				client.request.GetLeaseRef() != input.LeaseRef || client.request.GetFence() != input.LeaseFence || client.request.GetGeneration() != input.LeaseGeneration {
				t.Fatal("diagnostic change altered valid query or exact verified lease binding")
			}
		}
	}
}

func TestAssistantSearchSchemaMatchesClosedQueryInput(t *testing.T) {
	schema := assistantResourceSearchTool()["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	query := properties["query"].(map[string]any)
	if schema["additionalProperties"] != false || !reflect.DeepEqual(schema["required"], []string{"query"}) ||
		len(properties) != 1 || query["type"] != "string" || query["minLength"] != 2 || query["maxLength"] != 160 {
		t.Fatal("resource search schema differs from its closed handler input")
	}
}

func TestAssistantSearchDiagnosticsKeepClosedStagesAndOwnerCodes(t *testing.T) {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeProject)
	for _, test := range []struct {
		name      string
		input     runtimecontract.RunnerInput
		arguments map[string]any
		response  *controlplanev1.SearchAssistantResourcesResponse
		failure   error
		class     string
		code      codes.Code
		calls     int
	}{
		{"context", runtimecontract.RunnerInput{}, map[string]any{"query": "safe"}, nil, nil, "assistant_search_context_invalid", codes.Unknown, 0},
		{"shape", input, map[string]any{"query": "safe", "PRIVATE_ARGUMENT": "PRIVATE_VALUE"}, nil, nil, "assistant_search_input_shape_invalid", codes.Unknown, 0},
		{"query", input, map[string]any{"query": "x"}, nil, nil, "assistant_search_query_invalid", codes.Unknown, 0},
		{"query type", input, map[string]any{"query": 42}, nil, nil, "assistant_search_query_invalid", codes.Unknown, 0},
		{"query bound", input, map[string]any{"query": strings.Repeat("я", 161)}, nil, nil, "assistant_search_query_invalid", codes.Unknown, 0},
		{"owner", input, map[string]any{"query": "safe"}, nil, status.Error(codes.PermissionDenied, "PRIVATE_OWNER_ERROR"), "assistant_search_owner_failed", codes.PermissionDenied, 1},
		{"response", input, map[string]any{"query": "safe"}, nil, nil, "assistant_search_response_shape_invalid", codes.Unknown, 1},
		{"identity", input, map[string]any{"query": "safe"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Ref: "PRIVATE_RESULT"}}}, nil, "assistant_search_result_identity_invalid", codes.Unknown, 1},
		{"route", input, map[string]any{"query": "safe"}, &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_PROJECT, Ref: "prj_first123", ProjectRef: "prj_second123"}}}, nil, "assistant_search_result_route_invalid", codes.Unknown, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &assistantSearchDiagnosticClient{response: test.response, failure: test.failure}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			result, err := server.findPlatformResources(t.Context(), test.input, test.arguments)
			if err == nil || result != nil || controlFailureClass(err) != test.class || status.Code(err) != test.code || client.calls != test.calls {
				t.Fatal("resource search lost closed failure stage, rejection or original owner code")
			}
		})
	}
}

func TestAssistantSearchDiagnosticsKeepFailedMCPProjectionPrivate(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, _, _ := assistantOwnCurrentFixture(scope)
			client := &assistantSearchDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{},
				failure: status.Error(codes.Unavailable, "PRIVATE_OWNER_ERROR")}
			var logs bytes.Buffer
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			params, _ := json.Marshal(map[string]any{"name": "find_platform_resources", "arguments": map[string]any{"query": "PRIVATE_QUERY"}})
			request := httptest.NewRequest("POST", "/mcp", nil)
			request.Header.Set("Authorization", "Bearer PRIVATE_HEADER")
			recorder := httptest.NewRecorder()
			server.callTool(recorder, request, mcpRequest{ID: json.RawMessage(`"search-diagnostic"`), Params: params}, input)
			var logged map[string]any
			if json.Unmarshal(logs.Bytes(), &logged) != nil || logged["tool"] != "find_platform_resources" || logged["stage"] != "operation" ||
				logged["failure_class"] != "assistant_search_owner_failed" || logged["grpc_code"] != "Unavailable" ||
				!strings.Contains(recorder.Body.String(), `"error_code":"TOOL_UNAVAILABLE"`) ||
				client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.projection.GetSafeResult() != "TOOL_UNAVAILABLE" {
				t.Fatal("search diagnostic changed the existing failed MCP result or projection")
			}
			for _, private := range []string{"PRIVATE_QUERY", "PRIVATE_OWNER_ERROR", "PRIVATE_HEADER", input.LeaseFence, input.Instructions, input.Task} {
				if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(recorder.Body.String(), private)) {
					t.Fatal("search diagnostic disclosed private content")
				}
			}
		})
	}
}
