package callback

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func assistantWorkflowCatalogFixture(scope runtimecontract.AssistantScope) runtimecontract.RunnerInput {
	input, _, _ := assistantOwnCurrentFixture(scope)
	input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "WORKFLOW", EntityRef: "wfl_owned123",
		AllowedOperations: []string{"UPDATE_WORKFLOW"}}
	return input
}

func TestAssistantCatalogSelectionPreservesCurrentScreenDiscovery(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input := assistantWorkflowCatalogFixture(scope)
		_, err := configurationCatalog(input, map[string]any{"operation_types": []any{"UPDATE_ROLE_IMAGE_RECIPE"}})
		if err == nil || controlFailureClass(err) != "assistant_catalog_selection_invalid" {
			t.Fatal("wrong-screen selection lost its closed input classification")
		}
		for _, arguments := range []map[string]any{{}, {"operation_types": []any{}}} {
			result, err := configurationCatalog(input, arguments)
			if err != nil {
				t.Fatal("current-screen index discovery failed")
			}
			catalog := result.(map[string]any)
			if len(catalog["operation_schemas"].([]map[string]any)) != 0 {
				t.Fatal("index discovery unexpectedly expanded operation schemas")
			}
			found := false
			for _, kind := range catalog["operation_types"].([]string) {
				if kind == "UPDATE_ROLE_IMAGE_RECIPE" {
					t.Fatal("discovery expanded the current-screen boundary")
				}
				found = found || kind == "UPDATE_WORKFLOW"
			}
			if !found || input.AssistantContext.EntityKind != "WORKFLOW" || input.AssistantContext.EntityRef != "wfl_owned123" {
				t.Fatal("discovery lost the immutable current screen")
			}
		}
		if _, err := configurationCatalog(input, map[string]any{"operation_types": []any{"UPDATE_WORKFLOW"}}); err != nil {
			t.Fatal("allowed operation selection failed")
		}
	}
}

func TestAssistantCatalogSelectionMCPRecoveryIsPrivateAndProjectionBound(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, selected := range []any{"PRIVATE_SELECTION", []any{42}, []any{"PRIVATE_SELECTION"},
			[]any{"UPDATE_ROLE_IMAGE_RECIPE"}, []any{"UPDATE_WORKFLOW", "UPDATE_WORKFLOW"},
			[]any{"UPDATE_WORKFLOW", "UPDATE_WORKFLOW", "UPDATE_WORKFLOW", "UPDATE_WORKFLOW", "UPDATE_WORKFLOW"}} {
			input := assistantWorkflowCatalogFixture(scope)
			client := &assistantSearchRecoveryClient{assistantSearchDiagnosticClient: &assistantSearchDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{}}}
			var logs bytes.Buffer
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": map[string]any{"operation_types": selected}})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"catalog-selection"`), Params: params}, input)
			var response struct {
				Result struct {
					IsError    bool           `json:"isError"`
					Structured map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || !response.Result.IsError ||
				response.Result.Structured["error_code"] != "CATALOG_INPUT_INVALID" || response.Result.Structured["retryable"] != true || client.calls != 0 ||
				client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.projection.GetSafeResult() != "TOOL_UNAVAILABLE" {
				t.Fatal("local selection failure was not recoverable or changed owner projection")
			}
			guidance, ok := response.Result.Structured["guidance"].(string)
			if !ok || !strings.Contains(guidance, "get_configuration_catalog") || !strings.Contains(guidance, "current screen") ||
				!strings.Contains(logs.String(), `"failure_class":"assistant_catalog_selection_invalid"`) {
				t.Fatal("catalog recovery lost fixed screen-bound guidance or safe classification")
			}
			for _, private := range []string{"PRIVATE_SELECTION", input.LeaseFence, input.ProviderCredentialSHA256, input.Instructions, input.Task, input.AssistantContext.EntityRef} {
				if strings.Contains(logs.String(), private) || strings.Contains(recorder.Body.String(), private) {
					t.Fatal("catalog recovery disclosed private input or execution pins")
				}
			}
			client.terminalFailure = true
			recorder = httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"catalog-projection-failure"`), Params: params}, input)
			if !strings.Contains(recorder.Body.String(), `"error_code":"TOOL_UNAVAILABLE"`) || strings.Contains(recorder.Body.String(), `"retryable":true`) || strings.Contains(recorder.Body.String(), "PRIVATE_PROJECTION_ERROR") {
				t.Fatal("projection failure was misreported as recoverable local input")
			}
		}
	}
}

func TestAssistantCatalogSelectionDoesNotClassifyTextOrOwnerFailures(t *testing.T) {
	for _, err := range []error{errors.New("configuration catalog selection is invalid"),
		status.Error(codes.Unknown, "configuration catalog selection is invalid"), status.Error(codes.PermissionDenied, "PRIVATE_OWNER_ERROR")} {
		if controlFailureClass(err) == "assistant_catalog_selection_invalid" {
			t.Fatal("untrusted error text or owner refusal was treated as local validation")
		}
	}
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unavailable} {
		input, arguments, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeProject)
		client := &assistantSearchRecoveryClient{assistantSearchDiagnosticClient: &assistantSearchDiagnosticClient{
			assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{}, failure: status.Error(code, "PRIVATE_OWNER_ERROR")}}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))}
		params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
		recorder := httptest.NewRecorder()
		server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"catalog-owner-failure"`), Params: params}, input)
		if client.calls != 1 || !strings.Contains(recorder.Body.String(), `"error_code":"TOOL_UNAVAILABLE"`) ||
			strings.Contains(recorder.Body.String(), `"retryable":true`) || strings.Contains(recorder.Body.String(), "PRIVATE_OWNER_ERROR") {
			t.Fatal("owner failure was mistaken for recoverable local catalog selection")
		}
	}
}
