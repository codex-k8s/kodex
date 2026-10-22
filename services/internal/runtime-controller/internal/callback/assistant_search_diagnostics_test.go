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

// assistantSearchDiagnosticClient records search calls and exact requests while returning configured metadata or failure; other exercised MCP methods use its embedded stub.
// assistantSearchDiagnosticClient фиксирует поисковые вызовы и точные запросы, возвращая заданные метаданные или ошибку; прочие вызываемые методы MCP берутся из вложенной подстановки.
type assistantSearchDiagnosticClient struct {
	*assistantFreshCatalogMCPClient
	response *controlplanev1.SearchAssistantResourcesResponse
	failure  error
	calls    int
	request  *controlplanev1.SearchAssistantResourcesRequest
}

// SearchAssistantResources increments the local call count, captures the request and returns the configured response/error without any remote search.
// SearchAssistantResources увеличивает локальный счётчик, сохраняет запрос и возвращает заданные ответ и ошибку без удалённого поиска.
func (client *assistantSearchDiagnosticClient) SearchAssistantResources(_ context.Context, request *controlplanev1.SearchAssistantResourcesRequest, _ ...grpc.CallOption) (*controlplanev1.SearchAssistantResourcesResponse, error) {
	client.calls++
	client.request = request
	return client.response, client.failure
}

// TestAssistantSearchDiagnosticsDoNotTrustUnregisteredStages checks untyped errors, unknown local classes, a nil wrapper and remote status text all retain the closed unknown fallback.
// TestAssistantSearchDiagnosticsDoNotTrustUnregisteredStages проверяет, что нетипизированные ошибки, неизвестные локальные классы, nil-оболочка и удалённый текст статуса сохраняют закрытый неизвестный результат.
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

// TestAssistantSearchDiagnosticsPreserveSystemAndProjectSuccess checks Unicode query lengths at both accepted limits and exact lease/fence/generation forwarding in both assistant scopes.
// TestAssistantSearchDiagnosticsPreserveSystemAndProjectSuccess проверяет Unicode-запросы на обеих допустимых границах и точную передачу аренды, ограждения и поколения в обеих областях помощника.
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

// TestAssistantSearchHintsExactResourceContextWithoutGrantingAuthority exercises twelve navigation cases in both assistant scopes, including exact paths with tabs, foreign projects and integrations.
// Assertions keep existing routes and allowed operations unchanged; no owner permission is created by the hint.
// TestAssistantSearchHintsExactResourceContextWithoutGrantingAuthority проверяет двенадцать случаев навигации в обеих областях помощника, включая точные пути с вкладками, чужие проекты и интеграции.
// Проверки сохраняют исходные маршруты и разрешённые операции; подсказка не создаёт полномочий владельца.
func TestAssistantSearchHintsExactResourceContextWithoutGrantingAuthority(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, test := range []struct {
			name, kind, ref, project, currentKind, currentRef, currentRoute string
			want                                                            bool
		}{
			{"agent from run", "AGENT", "agt_target123", "prj_current123", "RUN", "run_current123", "/projects/prj_current123/runs/run_current123", true},
			{"other agent", "AGENT", "agt_target123", "prj_current123", "AGENT", "agt_other123", "/projects/prj_current123/agents/agt_other123", true},
			{"exact agent", "AGENT", "agt_target123", "prj_current123", "AGENT", "agt_target123", "/projects/prj_current123/agents/agt_target123?tab=instructions", false},
			{"agent ref with other route", "AGENT", "agt_target123", "prj_current123", "AGENT", "agt_target123", "/projects/prj_current123/runs/run_current123", true},
			{"workflow from project", "WORKFLOW", "wfl_target123", "prj_current123", "PROJECT", "prj_current123", "/projects/prj_current123", true},
			{"exact workflow", "WORKFLOW", "wfl_target123", "prj_current123", "WORKFLOW", "wfl_target123", "/projects/prj_current123/workflows/wfl_target123", false},
			{"other project", "AGENT", "agt_target123", "prj_other123", "AGENT", "agt_target123", "/projects/prj_other123/agents/agt_target123", true},
			{"integration mismatch", "INTEGRATION", "int_target123", "", "AGENT", "agt_target123", "/projects/prj_current123/agents/agt_target123", true},
			{"exact integration", "INTEGRATION", "int_target123", "", "INTEGRATION_CONNECTION", "int_target123", "/integrations?connectionRef=int_target123", false},
			{"run remains read only", "RUN", "run_target123", "prj_current123", "PROJECT", "prj_current123", "/projects/prj_current123", false},
			{"environment no blanket restriction", "RUNTIME_ENVIRONMENT", "renv_target123", "prj_current123", "PROJECT", "prj_current123", "/projects/prj_current123", false},
			{"image no blanket restriction", "ROLE_IMAGE", "imgrec_target123", "prj_current123", "PROJECT", "prj_current123", "/projects/prj_current123", false},
		} {
			t.Run(string(scope)+"/"+test.name, func(t *testing.T) {
				input, _, _ := assistantOwnCurrentFixture(scope)
				input.ProjectRef = "prj_current123"
				input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: test.currentKind, EntityRef: test.currentRef, Route: test.currentRoute, AllowedOperations: []string{"UNCHANGED"}}
				kind := controlplanev1.SearchResultKind(controlplanev1.SearchResultKind_value["SEARCH_RESULT_KIND_"+test.kind])
				client := &assistantSearchDiagnosticClient{response: &controlplanev1.SearchAssistantResourcesResponse{Results: []*controlplanev1.SearchResult{{Kind: kind, Ref: test.ref, ProjectRef: test.project, Title: "Resource"}}}}
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
				result, err := server.findPlatformResources(t.Context(), input, map[string]any{"query": "Resource"})
				if err != nil {
					t.Fatal(err)
				}
				items := result.(map[string]any)["results"].([]map[string]any)
				if len(items) != 1 || items[0]["requires_context_switch"] != test.want || client.calls != 1 || input.AssistantContext.AllowedOperations[0] != "UNCHANGED" || input.AssistantContext.Route != test.currentRoute {
					t.Fatal("resource search hint changed owner context, operation authority or exact route semantics")
				}
			})
		}
	}
}

// TestAssistantSearchSchemaMatchesClosedQueryInput checks that the tool exposes only the required query field with 2..160 bounds, fixed guidance and no additional properties.
// TestAssistantSearchSchemaMatchesClosedQueryInput проверяет единственное обязательное поле query с пределами 2..160, фиксированной подсказкой и запретом дополнительных свойств.
func TestAssistantSearchSchemaMatchesClosedQueryInput(t *testing.T) {
	schema := assistantResourceSearchTool()["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	query := properties["query"].(map[string]any)
	if schema["additionalProperties"] != false || !reflect.DeepEqual(schema["required"], []string{"query"}) ||
		len(properties) != 1 || query["type"] != "string" || query["minLength"] != 2 || query["maxLength"] != 160 ||
		query["description"] != assistantSearchQueryDescription {
		t.Fatal("resource search schema differs from its closed handler input")
	}
}

// TestAssistantSearchDiagnosticsKeepClosedStagesAndOwnerCodes checks nine local/owner/response failures for fixed classes, exact RPC counts and preservation of only actual owner gRPC codes.
// TestAssistantSearchDiagnosticsKeepClosedStagesAndOwnerCodes проверяет девять локальных ошибок, ошибок владельца и ответа: фиксированные классы, точное число RPC и сохранение кодов gRPC только от настоящей ошибки владельца.
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

// TestAssistantSearchDiagnosticsKeepFailedMCPProjectionPrivate checks both scopes retain failed MCP/activity projection and closed log labels on an owner error without query, header or instruction canaries.
// TestAssistantSearchDiagnosticsKeepFailedMCPProjectionPrivate проверяет в обеих областях сохранение неуспешной проекции MCP и активности, закрытых полей лога при ошибке владельца без контрольных значений запроса, заголовка и инструкций.
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
