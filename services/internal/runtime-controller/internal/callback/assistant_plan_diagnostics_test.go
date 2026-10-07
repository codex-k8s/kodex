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

func planDiagnosticError(code codes.Code, reason string, metadata map[string]string) error {
	value, _ := status.New(code, "PRIVATE_OPERATION_PAYLOAD").WithDetails(&errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: reason, Metadata: metadata})
	return value.Err()
}

func TestAssistantPlanDiagnosticsRejectNonCanonicalDetails(t *testing.T) {
	for _, stage := range []string{"HYDRATE", "NORMALIZE", "BIND", "AUTHORIZE", "EMPTY"} {
		for _, category := range []string{"CONFLICT", "VERSION"} {
			metadata := map[string]string{"category": category, "operation_index": "32"}
			if stage == "EMPTY" {
				delete(metadata, "operation_index")
			}
			err := planDiagnosticError(codes.Aborted, "ASSISTANT_PLAN_"+stage, metadata)
			want := "assistant_plan_" + strings.ToLower(stage) + "_" + strings.ToLower(category)
			if controlFailureClass(err) != want || controlFailureClass(assistantPlanControlError(err)) != want {
				t.Fatal("closed stage was lost across the callback boundary")
			}
		}
	}
	for _, metadata := range []map[string]string{
		{"category": "CONFLICT", "operation_index": "0"}, {"category": "CONFLICT", "operation_index": "33"},
		{"category": "CONFLICT", "operation_index": "01"}, {"category": "CONFLICT", "operation_index": "PRIVATE_OPERATION_PAYLOAD"},
		{"category": "PRIVATE_OPERATION_PAYLOAD", "operation_index": "1"}, {"category": "CONFLICT", "operation_index": "1", "payload": "PRIVATE_OPERATION_PAYLOAD"},
	} {
		if controlFailureClass(planDiagnosticError(codes.Aborted, "ASSISTANT_PLAN_HYDRATE", metadata)) != "control_aborted" {
			t.Fatal("noncanonical metadata escaped the bounded fallback")
		}
	}
	if controlFailureClass(planDiagnosticError(codes.Aborted, "PRIVATE_OPERATION_PAYLOAD", map[string]string{"category": "CONFLICT", "operation_index": "1"})) != "control_aborted" ||
		controlFailureClass(planDiagnosticError(codes.PermissionDenied, "ASSISTANT_PLAN_HYDRATE", map[string]string{"category": "CONFLICT", "operation_index": "1"})) != "control_permissiondenied" {
		t.Fatal("unknown stage changed an existing error boundary")
	}
	info := &errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "ASSISTANT_PLAN_HYDRATE", Metadata: map[string]string{"category": "CONFLICT", "operation_index": "1"}}
	duplicate, _ := status.New(codes.Aborted, "PRIVATE_OPERATION_PAYLOAD").WithDetails(info, info)
	foreign := &errdetails.ErrorInfo{Domain: "foreign", Reason: info.Reason, Metadata: info.Metadata}
	foreignError, _ := status.New(codes.Aborted, "PRIVATE_OPERATION_PAYLOAD").WithDetails(foreign)
	if controlFailureClass(duplicate.Err()) != "control_aborted" || controlFailureClass(foreignError.Err()) != "control_aborted" {
		t.Fatal("ambiguous or foreign stage escaped the closed boundary")
	}
}

type assistantPlanDiagnosticClient struct {
	*assistantFreshCatalogMCPClient
	failure error
}

func (client *assistantPlanDiagnosticClient) ProposeAssistantPlan(context.Context, *controlplanev1.ProposeAssistantPlanRequest, ...grpc.CallOption) (*controlplanev1.ProposeAssistantPlanResponse, error) {
	return nil, client.failure
}

func TestAssistantPlanDiagnosticsPreservePublicFailureAndSafeOperationTypes(t *testing.T) {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	operations := []any{}
	for range 4 {
		operations = append(operations, map[string]any{"type": "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", "title": "Owner settings", "summary": "Owner settings", "parameters": map[string]any{"systemAssistantRef": input.AgentRef, "instructions": "PRIVATE_OWNER_INSTRUCTIONS"}})
	}
	arguments := map[string]any{"summary": "PRIVATE_PLAN_SUMMARY", "operations": operations}
	client := &assistantPlanDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{}, failure: planDiagnosticError(codes.Aborted, "ASSISTANT_PLAN_HYDRATE", map[string]string{"category": "CONFLICT", "operation_index": "4"})}
	var logs bytes.Buffer
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	params, _ := json.Marshal(map[string]any{"name": "propose_configuration_plan", "arguments": arguments})
	recorder := httptest.NewRecorder()
	server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"plan-diagnostic"`), Params: params}, input)
	if !strings.Contains(recorder.Body.String(), `"error_code":"TOOL_UNAVAILABLE"`) || client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
		var wire struct {
			Result struct {
				StructuredContent struct {
					ErrorCode string `json:"error_code"`
				} `json:"structuredContent"`
			} `json:"result"`
		}
		_ = json.Unmarshal(recorder.Body.Bytes(), &wire)
		classes := []string{}
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var item struct {
				FailureClass string `json:"failure_class"`
			}
			_ = json.Unmarshal([]byte(line), &item)
			classes = append(classes, item.FailureClass)
		}
		t.Fatalf("public failure fixture: code=%s state=%s classes=%v", wire.Result.StructuredContent.ErrorCode, client.projection.GetState(), classes)
	}
	projection, _ := json.Marshal(client.projection.GetSafeParameters().AsMap())
	if !strings.Contains(string(projection), `"operation_types":["UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS"`) || !strings.Contains(string(projection), `"operation_count":4`) {
		t.Fatal("validated operation types were not projected")
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatal("unexpected diagnostic log cardinality")
	}
	for _, line := range lines {
		var item map[string]any
		if json.Unmarshal([]byte(line), &item) != nil || item["failure_class"] != "assistant_plan_hydrate_conflict" || item["operation_index"] != float64(4) {
			t.Fatal("closed diagnostic lost its stage or bounded operation index")
		}
	}
	for _, private := range []string{"PRIVATE_OPERATION_PAYLOAD", "PRIVATE_OWNER_INSTRUCTIONS", "PRIVATE_PLAN_SUMMARY", input.LeaseFence} {
		if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(recorder.Body.String(), private) || strings.Contains(string(projection), private)) {
			t.Fatal("diagnostics disclosed private operation data")
		}
	}
	operations[1].(map[string]any)["type"] = "PRIVATE_OPERATION_PAYLOAD"
	parameters, _, _, _ := safeToolCallParameters(input, "propose_configuration_plan", arguments)
	if _, present := parameters["operation_types"]; present {
		t.Fatal("unknown operation type entered safe public history")
	}
	arguments["operations"] = make([]any, 33)
	parameters, _, _, _ = safeToolCallParameters(input, "propose_configuration_plan", arguments)
	if _, present := parameters["operation_types"]; present {
		t.Fatal("unbounded operation type list entered safe public history")
	}
}
