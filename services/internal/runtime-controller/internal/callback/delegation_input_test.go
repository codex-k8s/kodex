package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
)

type delegationRecoveryClient struct {
	cp.RuntimeWorkServiceClient
	requests                  []*cp.DelegateExecutionRequest
	projections               []*cp.RecordRunToolCallRequest
	ownerError                error
	projectionFailureRevision int64
}

func (c *delegationRecoveryClient) DelegateExecution(_ context.Context, request *cp.DelegateExecutionRequest, _ ...grpc.CallOption) (*cp.DelegateExecutionResponse, error) {
	c.requests = append(c.requests, request)
	if c.ownerError != nil {
		return nil, c.ownerError
	}
	return &cp.DelegateExecutionResponse{ChildRun: &cp.Run{Ref: "run_childfixture"}, CallbackEdgeRef: "edg_callbackfixture"}, nil
}

func (c *delegationRecoveryClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	c.projections = append(c.projections, request)
	if request.Revision == c.projectionFailureRevision {
		return nil, status.Error(codes.Unavailable, "PRIVATE_PROJECTION")
	}
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_fixture"}}, nil
}

func delegationRecoveryInput() runtimecontract.RunnerInput {
	return runtimecontract.RunnerInput{LeaseRef: "lea_fixture", LeaseFence: "PRIVATE_FENCE", LeaseGeneration: 7,
		Instructions: "PRIVATE_INSTRUCTIONS", Task: "PRIVATE_ROOT_TASK", DelegationTargets: []runtimecontract.RunnerDelegationTarget{
			{Ref: "agt_architectfixture", WorkflowStepKey: "step-002"},
			{Ref: "agt_developerfixture", WorkflowStepKey: "step-003"},
		}}
}

func delegationRecoveryArguments() map[string]any {
	return map[string]any{"target_agent_ref": "agt_architectfixture", "workflow_step_key": "step-002", "task": "PRIVATE_CHILD_TASK", "input": map[string]any{"data": "PRIVATE_INPUT"}}
}

func delegationRecoveryCall(t *testing.T, client *delegationRecoveryClient, input runtimecontract.RunnerInput, arguments map[string]any) (map[string]any, string) {
	t.Helper()
	var logs bytes.Buffer
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	params, err := json.Marshal(map[string]any{"name": "delegate_agent", "arguments": arguments})
	if err != nil {
		t.Fatal("fixture encoding failed")
	}
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"delegate-fixture"`), Params: params}, input)
	var response struct {
		Result struct {
			Structured map[string]any `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(writer.Body.Bytes(), &response) != nil {
		t.Fatal("MCP response decoding failed")
	}
	for _, private := range []string{"PRIVATE_CHILD_TASK", "PRIVATE_INPUT", input.Instructions, input.Task, input.LeaseFence, "PRIVATE_OWNER", "PRIVATE_PROJECTION", "PRIVATE_FIELD"} {
		if strings.Contains(writer.Body.String(), private) || strings.Contains(logs.String(), private) {
			t.Fatal("delegation recovery disclosed private data")
		}
	}
	if response.Result.Structured != nil {
		var text map[string]any
		if len(response.Result.Content) != 1 || json.Unmarshal([]byte(response.Result.Content[0].Text), &text) != nil || text["error_code"] != response.Result.Structured["error_code"] {
			t.Fatal("MCP text and structured recovery disagree")
		}
	}
	return response.Result.Structured, logs.String()
}

func TestDelegationInputRecoveryRejectsWithoutOwnerEffect(t *testing.T) {
	for _, fixture := range []struct {
		name, reason string
		change       func(map[string]any)
	}{
		{"shape", "shape", func(a map[string]any) { a["PRIVATE_FIELD"] = "PRIVATE_INPUT" }},
		{"target-type", "shape", func(a map[string]any) { a["target_agent_ref"] = 42 }},
		{"step-type", "shape", func(a map[string]any) { a["workflow_step_key"] = 42 }},
		{"wrong-pair", "selection", func(a map[string]any) { a["target_agent_ref"] = "agt_developerfixture" }},
		{"foreign-target", "selection", func(a map[string]any) { a["target_agent_ref"] = "agt_foreignfixture" }},
		{"removed-step", "selection", func(a map[string]any) { a["workflow_step_key"] = "step-001" }},
		{"missing-step", "selection", func(a map[string]any) { delete(a, "workflow_step_key") }},
		{"empty-task", "task", func(a map[string]any) { a["task"] = " \t\n " }},
		{"task-type", "task", func(a map[string]any) { a["task"] = 42 }},
		{"task-bound", "task", func(a map[string]any) { a["task"] = strings.Repeat("я", 32769) }},
		{"input-shape", "input", func(a map[string]any) { a["input"] = []any{"PRIVATE_INPUT"} }},
		{"input-null", "input", func(a map[string]any) { a["input"] = nil }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			arguments := delegationRecoveryArguments()
			fixture.change(arguments)
			client := &delegationRecoveryClient{}
			result, logs := delegationRecoveryCall(t, client, delegationRecoveryInput(), arguments)
			if len(client.requests) != 0 || result["error_code"] != delegationInputInvalidCode || result["retryable"] != true || result["guidance"] != delegationInputInvalidGuidance ||
				!strings.Contains(logs, `"failure_class":"delegation_input_`+fixture.reason+`"`) {
				t.Fatal("local rejection lost bounded recovery or reached owner effect")
			}
			if len(client.projections) != 2 || client.projections[1].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.projections[1].SafeResult != "TOOL_UNAVAILABLE" {
				t.Fatal("recovery changed owner failed receipt")
			}
		})
	}
}

func TestDelegationInputRecoveryPreservesOwnerAndProjectionFailures(t *testing.T) {
	for _, code := range []codes.Code{codes.Unknown, codes.Unavailable, codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition, codes.Aborted, codes.DeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			client := &delegationRecoveryClient{ownerError: status.Error(code, "PRIVATE_OWNER")}
			result, _ := delegationRecoveryCall(t, client, delegationRecoveryInput(), delegationRecoveryArguments())
			if len(client.requests) != 1 || result["error_code"] != "TOOL_UNAVAILABLE" || result["retryable"] != false || result["guidance"] != nil {
				t.Fatal("owner refusal became recoverable or was replayed")
			}
		})
	}
	for _, revision := range []int64{1, 2} {
		client := &delegationRecoveryClient{projectionFailureRevision: revision}
		arguments := delegationRecoveryArguments()
		arguments["target_agent_ref"] = "agt_developerfixture"
		result, _ := delegationRecoveryCall(t, client, delegationRecoveryInput(), arguments)
		if len(client.requests) != 0 || result["retryable"] == true || result["error_code"] == delegationInputInvalidCode {
			t.Fatal("failed owner projection allowed local recovery")
		}
	}
}

func TestDelegationInputRecoveryKeepsCurrentPairAndExecutionPins(t *testing.T) {
	input := delegationRecoveryInput()
	client := &delegationRecoveryClient{}
	arguments := delegationRecoveryArguments()
	arguments["target_agent_ref"] = "agt_developerfixture"
	delegationRecoveryCall(t, client, input, arguments)
	arguments["target_agent_ref"] = "agt_architectfixture"
	result, _ := delegationRecoveryCall(t, client, input, arguments)
	if result["ok"] != true || len(client.requests) != 1 {
		t.Fatal("corrected current pair did not produce exactly one owner call")
	}
	r := client.requests[0]
	if r.TargetAgentRef != "agt_architectfixture" || r.WorkflowStepKey != "step-002" || r.LeaseRef != input.LeaseRef || r.Fence != input.LeaseFence || r.Generation != input.LeaseGeneration || r.Mutation.IdempotencyKey != stableKey(input.LeaseRef, `"delegate-fixture"`) || r.Task != "PRIVATE_CHILD_TASK" || r.Input.AsMap()["data"] != "PRIVATE_INPUT" {
		t.Fatal("recovery changed exact owner request pins or input")
	}
	input.DelegationTargets = nil
	result, _ = delegationRecoveryCall(t, client, input, arguments)
	if result["ok"] == true || len(client.requests) != 1 {
		t.Fatal("old pair bypassed current catalog removal")
	}
	ordinary := delegationRecoveryInput()
	ordinary.DelegationTargets = []runtimecontract.RunnerDelegationTarget{{Ref: "agt_architectfixture"}}
	delete(arguments, "workflow_step_key")
	delete(arguments, "input")
	if _, _, _, _, err := validateDelegationInput(ordinary, arguments); err != nil {
		t.Fatal("ordinary delegation or optional input became unavailable")
	}
}

func TestDelegationInputClassificationIsClosedAndPrivate(t *testing.T) {
	for _, err := range []error{errors.New("delegation input is invalid"), status.Error(codes.Unknown, "delegation input is invalid"), &delegationInputError{reason: "PRIVATE_INPUT"}} {
		if delegationInputFailureClass(err) != "" {
			t.Fatal("untrusted error text acquired local input authority")
		}
	}
	arguments := delegationRecoveryArguments()
	arguments["input"] = map[string]any{"data": make(chan int)}
	_, _, _, _, err := validateDelegationInput(delegationRecoveryInput(), arguments)
	if delegationInputFailureClass(err) != "delegation_input_input" || err.Error() != "delegation input is invalid" {
		t.Fatal("unsupported structured input lost closed classification")
	}
}
