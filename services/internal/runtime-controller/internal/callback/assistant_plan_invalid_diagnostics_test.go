package callback

import (
	"bytes"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAssistantPlanInvalidDetailsAreClosed(t *testing.T) {
	for _, stage := range []string{"HYDRATE", "NORMALIZE", "COMMAND", "BIND", "AUTHORIZE"} {
		for _, field := range []string{"WORKFLOW_SHAPE", "WORKFLOW_REF", "WORKFLOW_VERSION", "WORKFLOW_BINDING", "MAX_CONCURRENCY", "TIMEOUT_SECONDS", "WORKFLOW_TEXT", "INPUT_FIELDS", "STEPS", "INPUT_FIELD_KEY", "STEP_SHAPE", "STEP_KEY", "STEPS_GRAPH", "WORKFLOW_DRAFT", "STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS"} {
			err := planDiagnosticError(codes.InvalidArgument, "ASSISTANT_PLAN_"+stage, map[string]string{"category": "INVALID", "operation_index": "32", "field": field})
			diagnostic, ok := assistantPlanInvalidDetails(err)
			if !ok || diagnostic.stage != strings.ToLower(stage) || diagnostic.field != field || diagnostic.index != 32 || controlFailureClass(err) != "assistant_plan_"+strings.ToLower(stage)+"_invalid" {
				t.Fatal("closed internal invalid metadata lost its stage or field")
			}
		}
	}
	base := map[string]string{"category": "INVALID", "operation_index": "1", "field": "STEPS_INSTRUCTIONS"}
	for _, change := range []struct{ key, value string }{{"field", "PRIVATE_PAYLOAD"}, {"field", ""}, {"field", "STEPS_INSTRUCTIONS\nPRIVATE_PAYLOAD"}, {"category", "CONFLICT"}, {"operation_index", "0"}, {"operation_index", "33"}, {"operation_index", "01"}, {"unknown", "PRIVATE_PAYLOAD"}} {
		metadata := map[string]string{}
		for key, value := range base {
			metadata[key] = value
		}
		metadata[change.key] = change.value
		if _, ok := assistantPlanInvalidDetails(planDiagnosticError(codes.InvalidArgument, "ASSISTANT_PLAN_HYDRATE", metadata)); ok {
			t.Fatal("invalid metadata escaped the closed schema")
		}
	}
	info := &errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "ASSISTANT_PLAN_HYDRATE", Metadata: base}
	duplicate, _ := status.New(codes.InvalidArgument, "PRIVATE_PAYLOAD").WithDetails(info, info)
	info.Domain = "foreign"
	foreign, _ := status.New(codes.InvalidArgument, "PRIVATE_PAYLOAD").WithDetails(info)
	info.Domain = "kodex.control-plane"
	info.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	unknownProto, _ := status.New(codes.InvalidArgument, "PRIVATE_PAYLOAD").WithDetails(info)
	for _, err := range []error{duplicate.Err(), foreign.Err(), unknownProto.Err(), status.Error(codes.InvalidArgument, "PRIVATE_PAYLOAD"), planDiagnosticError(codes.Aborted, "ASSISTANT_PLAN_HYDRATE", base), planDiagnosticError(codes.InvalidArgument, "PRIVATE_PAYLOAD", base)} {
		if _, ok := assistantPlanInvalidDetails(err); ok {
			t.Fatal("foreign, ambiguous or raw error gained diagnostic authority")
		}
	}
}

func TestAssistantPlanInvalidDiagnosticKeepsFailedReceiptAndGuidance(t *testing.T) {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	arguments := map[string]any{"summary": "PRIVATE_SUMMARY", "operations": []any{map[string]any{"type": "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", "parameters": map[string]any{"systemAssistantRef": input.AgentRef, "instructions": "PRIVATE_CONTENT"}}}}
	for _, stage := range []string{"HYDRATE", "NORMALIZE", "COMMAND"} {
		t.Run(stage, func(t *testing.T) {
			client := &assistantPlanDiagnosticClient{assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{}, failure: planDiagnosticError(codes.InvalidArgument, "ASSISTANT_PLAN_"+stage, map[string]string{"category": "INVALID", "operation_index": "1", "field": "STEPS_INSTRUCTIONS"})}
			var logs bytes.Buffer
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			params, _ := json.Marshal(map[string]any{"name": "propose_configuration_plan", "arguments": arguments})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"invalid-diagnostic"`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || !wire.Result.IsError || wire.Result.StructuredContent["error_code"] != "PLAN_INPUT_INVALID" || wire.Result.StructuredContent["retryable"] != true || wire.Result.StructuredContent["failure_stage"] != strings.ToLower(stage) || wire.Result.StructuredContent["failure_field"] != "STEPS_INSTRUCTIONS" || client.projection.GetSafeResult() != "PLAN_INPUT_INVALID" || client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
				t.Fatal("invalid diagnostic changed failed activity or corrective guidance")
			}
			if !strings.Contains(logs.String(), `"failure_field":"STEPS_INSTRUCTIONS"`) || !strings.Contains(logs.String(), `"failure_class":"assistant_plan_`+strings.ToLower(stage)+`_invalid"`) {
				t.Fatal("closed stage and field were not logged")
			}
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var item map[string]any
				if json.Unmarshal([]byte(line), &item) != nil || item["failure_class"] != "assistant_plan_"+strings.ToLower(stage)+"_invalid" || item["failure_field"] != "STEPS_INSTRUCTIONS" {
					t.Fatal("outer tool boundary lost the verified diagnostic")
				}
			}
			for _, private := range []string{"PRIVATE_PAYLOAD", "PRIVATE_SUMMARY", "PRIVATE_CONTENT", input.LeaseFence, input.AgentRef} {
				if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(recorder.Body.String(), private) || strings.Contains(client.projection.GetSafeResult(), private)) {
					t.Fatal("invalid diagnostic disclosed payload, ref or credential")
				}
			}
		})
	}
	bare := assistantPlanControlError(status.Error(codes.InvalidArgument, "PRIVATE_PAYLOAD"))
	if _, ok := assistantPlanInvalidDetails(bare); ok || safeToolCallResult("propose_configuration_plan", nil, bare) != "PLAN_INPUT_INVALID" || safeToolCallResult("read_file", nil, bare) != "TOOL_UNAVAILABLE" {
		t.Fatal("bare invalid invented fields or changed another tool")
	}
}
