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

// delegationRecoveryClient records local delegation/audit calls and injects an owner
// error or selected audit-phase failure, without creating real runs or durable events.
// delegationRecoveryClient записывает local delegation/audit calls и подставляет owner
// error либо выбранный audit-phase failure без real runs и durable events.
type delegationRecoveryClient struct {
	cp.RuntimeWorkServiceClient
	requests                  []*cp.DelegateExecutionRequest
	projections               []*cp.RecordRunToolCallRequest
	ownerError                error
	projectionFailureRevision int64
}

// DelegateExecution records the exact request pointer and returns an injected error
// or fixed child/callback refs; it does not authenticate, deduplicate or create a child.
// DelegateExecution записывает exact request pointer и возвращает injected error
// либо fixed child/callback refs, не аутентифицируя,не deduplicating и не создавая child.
func (c *delegationRecoveryClient) DelegateExecution(_ context.Context, request *cp.DelegateExecutionRequest, _ ...grpc.CallOption) (*cp.DelegateExecutionResponse, error) {
	c.requests = append(c.requests, request)
	if c.ownerError != nil {
		return nil, c.ownerError
	}
	return &cp.DelegateExecutionResponse{ChildRun: &cp.Run{Ref: "run_childfixture"}, CallbackEdgeRef: "edg_callbackfixture"}, nil
}

// RecordRunToolCall records each activity and may fail one configured revision
// with a private canary; successful synthetic event refs do not prove durable audit.
// RecordRunToolCall записывает activity и может отклонить заданную revision
// с private canary; synthetic event refs не доказывают durable audit.
func (c *delegationRecoveryClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	c.projections = append(c.projections, request)
	if request.Revision == c.projectionFailureRevision {
		return nil, status.Error(codes.Unavailable, "PRIVATE_PROJECTION")
	}
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_fixture"}}, nil
}

// delegationRecoveryInput creates two exact synthetic target/step pairs and private
// execution canaries with fixed lease generation for local recovery boundary tests.
// delegationRecoveryInput создаёт две exact synthetic target/step pairs и private
// execution canaries с fixed lease generation для local recovery boundary tests.
func delegationRecoveryInput() runtimecontract.RunnerInput {
	return runtimecontract.RunnerInput{LeaseRef: "lea_fixture", LeaseFence: "PRIVATE_FENCE", LeaseGeneration: 7,
		Instructions: "PRIVATE_INSTRUCTIONS", Task: "PRIVATE_ROOT_TASK", DelegationTargets: []runtimecontract.RunnerDelegationTarget{
			{Ref: "agt_architectfixture", WorkflowStepKey: "step-002"},
			{Ref: "agt_developerfixture", WorkflowStepKey: "step-003"},
		}}
}

// delegationRecoveryArguments returns fresh valid target/step/task/input maps with
// private canaries that must not be reflected in error wire or diagnostic logs.
// delegationRecoveryArguments возвращает новые valid target/step/task/input maps
// с private canaries, которые не должны отражаться в error wire/logs.
func delegationRecoveryArguments() map[string]any {
	return map[string]any{"target_agent_ref": "agt_architectfixture", "workflow_step_key": "step-002", "task": "PRIVATE_CHILD_TASK", "input": map[string]any{"data": "PRIVATE_INPUT"}}
}

// delegationRecoveryCall invokes local MCP dispatch with a recording fake, captures
// wire/logs, rejects private canary disclosure and checks textual/structured recovery
// codes agree before returning the structured projection and captured diagnostics.
// delegationRecoveryCall вызывает local MCP dispatch с recording fake, собирает
// wire/logs,проверяет отсутствие private canaries и согласие textual/structured
// recovery codes, возвращая structured projection и captured diagnostics.
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

// TestDelegationInputRecoveryRejectsWithoutOwnerEffect exercises twelve malformed
// shape/pair/task/input cases, checking no delegate RPC, bounded retry guidance,
// closed failure labels and failed audit shape rather than a success receipt.
// TestDelegationInputRecoveryRejectsWithoutOwnerEffect проверяет двенадцать malformed
// shape/pair/task/input cases: нет delegate RPC, есть bounded retry guidance,closed
// failure labels и failed audit shape вместо success receipt.
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

// TestDelegationInputRecoveryPreservesOwnerAndProjectionFailures checks seven owner
// status classes and either failed audit phase never become locally retryable input
// or trigger another delegation attempt.
// TestDelegationInputRecoveryPreservesOwnerAndProjectionFailures проверяет, что семь owner
// status classes и отказ любой audit phase не становятся locally retryable input
// и не вызывают ещё одну delegation attempt.
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

// TestDelegationInputRecoveryKeepsCurrentPairAndExecutionPins checks one corrected
// snapshot pair sends exactly one request with original lease/idempotency/task/input,
// a removed pair stays rejected, and ordinary targets may omit step/input.
// TestDelegationInputRecoveryKeepsCurrentPairAndExecutionPins проверяет один corrected
// snapshot pair и ровно один request с исходными lease/idempotency/task/input,
// отказ удалённой пары и optional step/input обычного target.
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

// TestDelegationInputClassificationIsClosedAndPrivate checks matching error text
// and unknown private reasons cannot acquire a local failure class, while an
// unsupported protobuf value returns the fixed registered input failure.
// TestDelegationInputClassificationIsClosedAndPrivate проверяет отсутствие local failure
// class у matching error text/unknown private reason и fixed registered input failure
// для неподдерживаемого protobuf value.
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
