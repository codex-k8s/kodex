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
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type assistantCurrentDiagnosticClient struct {
	*assistantFreshCatalogMCPClient
	failure error
}

func (client *assistantCurrentDiagnosticClient) SearchAssistantResources(context.Context, *controlplanev1.SearchAssistantResourcesRequest, ...grpc.CallOption) (*controlplanev1.SearchAssistantResourcesResponse, error) {
	return nil, client.failure
}

func TestAssistantCurrentDiagnosticsLogClosedStageWithoutChangingMCPFailure(t *testing.T) {
	input, arguments, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	value, _ := status.New(codes.Unavailable, "PRIVATE_PROVIDER_PAYLOAD").WithDetails(&errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT"})
	client := &assistantCurrentDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{}, failure: value.Err()}
	var logs bytes.Buffer
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
	recorder := httptest.NewRecorder()
	server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"current-diagnostic"`), Params: params}, input)
	var logged map[string]any
	if json.Unmarshal(logs.Bytes(), &logged) != nil || logged["failure_class"] != "assistant_current_configuration_prompt_context" || logged["grpc_code"] != "Unavailable" ||
		!strings.Contains(recorder.Body.String(), `"error_code":"TOOL_UNAVAILABLE"`) || client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
		t.Fatal("closed diagnostic did not preserve the existing failed MCP projection")
	}
	for _, private := range []string{"PRIVATE_PROVIDER_PAYLOAD", input.LeaseFence, input.Instructions, input.Task} {
		if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(recorder.Body.String(), private)) {
			t.Fatal("diagnostic log or MCP failure disclosed private content")
		}
	}
}

func TestAssistantCurrentDiagnosticsClassifyOnlyCanonicalClosedStages(t *testing.T) {
	known := map[string]string{
		"ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT":      "assistant_current_configuration_prompt_context",
		"ASSISTANT_CURRENT_CONFIGURATION_CONFIG_VIEW":         "assistant_current_configuration_config_view",
		"ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_READ":     "assistant_current_configuration_owner_core_read",
		"ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_VERSION":  "assistant_current_configuration_owner_core_version",
		"ASSISTANT_CURRENT_CONFIGURATION_TEMPLATE_PROJECTION": "assistant_current_configuration_template_projection",
		"ASSISTANT_CURRENT_CONFIGURATION_UNCLASSIFIED":        "assistant_current_configuration_unclassified",
	}
	for reason, want := range known {
		value, err := status.New(codes.Unavailable, "PRIVATE_PROVIDER_PAYLOAD").WithDetails(&errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: reason})
		if err != nil || controlFailureClass(value.Err()) != want {
			t.Fatal("canonical stage was not projected to the closed failure class")
		}
	}
	for _, info := range []*errdetails.ErrorInfo{
		{Domain: "kodex.control-plane", Reason: "PRIVATE_PROVIDER_PAYLOAD"},
		{Domain: "foreign", Reason: "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT"},
		{Domain: "kodex.control-plane", Reason: "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT", Metadata: map[string]string{"payload": "PRIVATE_PROVIDER_PAYLOAD"}},
	} {
		value, _ := status.New(codes.Unavailable, "PRIVATE_PROVIDER_PAYLOAD").WithDetails(info)
		if controlFailureClass(value.Err()) != "control_unavailable" {
			t.Fatal("noncanonical details escaped the bounded fallback")
		}
	}
	info := &errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT"}
	duplicate, _ := status.New(codes.Unavailable, "PRIVATE_PROVIDER_PAYLOAD").WithDetails(info, info)
	wrongCode, _ := status.New(codes.PermissionDenied, "PRIVATE_PROVIDER_PAYLOAD").WithDetails(info)
	if controlFailureClass(duplicate.Err()) != "control_unavailable" || controlFailureClass(wrongCode.Err()) != "control_permissiondenied" {
		t.Fatal("ambiguous stage details changed the existing error boundary")
	}
}
