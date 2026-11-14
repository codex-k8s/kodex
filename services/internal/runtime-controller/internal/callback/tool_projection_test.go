package callback

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// toolProjectionOwner injects receipt transport failures and counts synthetic invocation dispatches.
// It models lost acknowledgements without granting authority, calling an API or accessing a database.
// toolProjectionOwner подставляет transport failures receipt и считает synthetic invocation dispatch.
// Потерянные ACK моделируются без выдачи authority, обращения к API либо базе данных.
type toolProjectionOwner struct {
	controlplanev1.RuntimeWorkServiceClient
	codes       []codes.Code
	requests    []*controlplanev1.RecordRunToolCallRequest
	accepted    map[string]bool
	cancel      context.CancelFunc
	empty       bool
	invocations int
}

// RecordRunToolCall retains exact request copies and one receipt per idempotency key, even on a lost ACK.
// The fake deliberately accepts before returning an injected transport error.
// RecordRunToolCall сохраняет exact request copies и один receipt каждого idempotency key, даже при lost ACK.
// Подстановка намеренно принимает запись до возврата injected transport error.
func (owner *toolProjectionOwner) RecordRunToolCall(_ context.Context, request *controlplanev1.RecordRunToolCallRequest, _ ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	owner.requests = append(owner.requests, proto.Clone(request).(*controlplanev1.RecordRunToolCallRequest))
	if owner.accepted == nil {
		owner.accepted = map[string]bool{}
	}
	owner.accepted[request.GetMutation().GetIdempotencyKey()] = true
	if owner.cancel != nil {
		owner.cancel()
	}
	index := len(owner.requests) - 1
	if index < len(owner.codes) && owner.codes[index] != codes.OK {
		return nil, status.Error(owner.codes[index], "synthetic receipt failure")
	}
	if owner.empty {
		return &controlplanev1.RecordRunToolCallResponse{}, nil
	}
	return &controlplanev1.RecordRunToolCallResponse{Event: &controlplanev1.RunEvent{Ref: "evt_projection1"}}, nil
}

// ResolveIntegrationInvocation counts the synthetic business dispatch separately from activity recording.
// ResolveIntegrationInvocation считает synthetic business dispatch отдельно от записи activity.
func (owner *toolProjectionOwner) ResolveIntegrationInvocation(_ context.Context, _ *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	owner.invocations++
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: "inv_projection1", State: "SUCCEEDED"}, nil
}

// GetIntegrationInvocation returns only a completed synthetic result; it cannot issue another effect.
// GetIntegrationInvocation возвращает только completed synthetic result и не создаёт новый effect.
func (*toolProjectionOwner) GetIntegrationInvocation(_ context.Context, _ *controlplanev1.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	return &controlplanev1.GetIntegrationInvocationResponse{State: "SUCCEEDED", ResultSummary: "synthetic effect completed"}, nil
}

// toolProjectionServer supplies bounded transport and a discarded logger for the existing callback path.
// toolProjectionServer задаёт bounded transport и discarded logger существующего callback path.
func toolProjectionServer(owner *toolProjectionOwner) *Server {
	return &Server{control: &controlplaneclient.Client{Runtime: owner}, config: Config{RequestTimeout: time.Second}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestTerminalProjectionRetryReusesExactReceipt checks transient failures, lost ACK and the hard attempt bound.
// TestTerminalProjectionRetryReusesExactReceipt проверяет transient failures, lost ACK и жёсткий предел attempts.
func TestTerminalProjectionRetryReusesExactReceipt(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			for _, persistent := range []bool{false, true} {
				owner := &toolProjectionOwner{codes: []codes.Code{code}}
				if persistent {
					owner.codes = append(owner.codes, code)
				}
				request := &controlplanev1.RecordRunToolCallRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: "terminal-projection-synthetic"},
					LeaseRef: "lea_projection1", Fence: "synthetic-fence", Generation: 2, CallRef: "tcl_projection1", Tool: runtimecontract.WorkspacePolicyTool,
					State: controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED, Revision: 2, SafeResult: "completed", DurationMs: 17}
				response, err := toolProjectionServer(owner).recordToolProjection(t.Context(), request)
				if len(owner.requests) != 2 || !proto.Equal(owner.requests[0], request) || !proto.Equal(owner.requests[1], request) || len(owner.accepted) != 1 {
					t.Fatal("retry changed the receipt or exceeded its bound")
				}
				if persistent {
					if status.Code(err) != code || response != nil {
						t.Fatal("persistent failure became success")
					}
				} else if err != nil || response.GetEvent().GetRef() != "evt_projection1" {
					t.Fatal("lost acknowledgement was not recovered")
				}
			}
		})
	}
}

// TestProjectionRetryNeverReplaysAdmissionOrPermanentFailure keeps initial authority and closed denials unchanged.
// TestProjectionRetryNeverReplaysAdmissionOrPermanentFailure сохраняет initial authority и closed denials.
func TestProjectionRetryNeverReplaysAdmissionOrPermanentFailure(t *testing.T) {
	for _, test := range []struct {
		revision int64
		code     codes.Code
	}{
		{1, codes.Unavailable}, {1, codes.DeadlineExceeded},
		{2, codes.PermissionDenied}, {2, codes.Unauthenticated}, {2, codes.FailedPrecondition},
		{2, codes.AlreadyExists}, {2, codes.InvalidArgument}, {2, codes.NotFound},
		{2, codes.Aborted}, {2, codes.ResourceExhausted}, {2, codes.Internal}, {2, codes.Unknown},
	} {
		owner := &toolProjectionOwner{codes: []codes.Code{test.code}}
		_, err := toolProjectionServer(owner).recordToolProjection(t.Context(), &controlplanev1.RecordRunToolCallRequest{Revision: test.revision})
		if status.Code(err) != test.code || len(owner.requests) != 1 {
			t.Fatal("initial admission or permanent failure was retried")
		}
	}
}

// TestProjectionRetryHonorsCallerCancellation prevents another RPC after canceled or expired caller authority.
// TestProjectionRetryHonorsCallerCancellation запрещает новый RPC после отмены либо истечения caller context.
func TestProjectionRetryHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	owner := &toolProjectionOwner{}
	_, err := toolProjectionServer(owner).recordToolProjection(ctx, &controlplanev1.RecordRunToolCallRequest{Revision: 2})
	if status.Code(err) != codes.Canceled || len(owner.requests) != 0 {
		t.Fatal("canceled caller opened a new RPC")
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	owner = &toolProjectionOwner{codes: []codes.Code{codes.Unavailable}, cancel: cancel}
	_, err = toolProjectionServer(owner).recordToolProjection(ctx, &controlplanev1.RecordRunToolCallRequest{Revision: 2})
	if status.Code(err) != codes.Canceled || len(owner.requests) != 1 {
		t.Fatal("cancellation during backoff opened another RPC")
	}
	ctx, stop := context.WithTimeout(t.Context(), terminalProjectionRetryDelay/4)
	defer stop()
	owner = &toolProjectionOwner{codes: []codes.Code{codes.Unavailable}}
	_, err = toolProjectionServer(owner).recordToolProjection(ctx, &controlplanev1.RecordRunToolCallRequest{Revision: 2})
	if status.Code(err) != codes.DeadlineExceeded || len(owner.requests) != 1 {
		t.Fatal("retry extended the caller deadline")
	}
}

// TestMCPProjectionRetryNeverRedispatchesIntegration proves the real callback dispatch is outside the retry loop.
// TestMCPProjectionRetryNeverRedispatchesIntegration доказывает, что actual callback dispatch находится вне retry loop.
func TestMCPProjectionRetryNeverRedispatchesIntegration(t *testing.T) {
	owner := &toolProjectionOwner{codes: []codes.Code{codes.OK, codes.Unavailable, codes.OK}}
	server := toolProjectionServer(owner)
	input := runtimecontract.RunnerInput{LeaseRef: "lea_projection1", LeaseFence: "synthetic-fence", LeaseGeneration: 1, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{integrationGrantFixture()}}
	params, err := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": map[string]any{"grant_ref": input.IntegrationGrants[0].Ref, "input": map[string]any{"value": "synthetic-write"}}})
	if err != nil {
		t.Fatal(err)
	}
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{JSONRPC: "2.0", ID: json.RawMessage(`"request-projection1"`), Params: params}, input)
	var response struct {
		Result struct {
			IsError    bool `json:"isError"`
			Structured struct {
				OK bool `json:"ok"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(writer.Body.Bytes(), &response) != nil || response.Result.IsError || !response.Result.Structured.OK || owner.invocations != 1 || len(owner.requests) != 3 || len(owner.accepted) != 2 || !proto.Equal(owner.requests[1], owner.requests[2]) {
		t.Fatal("terminal activity retry changed dispatch or lost the acknowledged result")
	}
}

// TestProjectionStillRejectsMissingOwnerEvent prevents a transport success from fabricating an activity receipt.
// TestProjectionStillRejectsMissingOwnerEvent запрещает fabricating activity receipt по одному transport success.
func TestProjectionStillRejectsMissingOwnerEvent(t *testing.T) {
	owner := &toolProjectionOwner{empty: true}
	input := runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{integrationGrantFixture()}}
	err := toolProjectionServer(owner).recordToolCallPhase(t.Context(), input, "invoke_integration", map[string]any{"grant_ref": input.IntegrationGrants[0].Ref, "input": map[string]any{"value": "synthetic"}}, nil, nil, json.RawMessage(`"request-empty1"`), 0, 2)
	if err == nil || len(owner.requests) != 1 {
		t.Fatal("empty owner event was accepted or blindly retried")
	}
}
