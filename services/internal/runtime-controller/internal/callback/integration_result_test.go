package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

type integrationResultClient struct {
	controlplanev1.RuntimeWorkServiceClient
	state, invocation string
	resolves          []*controlplanev1.ResolveIntegrationInvocationRequest
	reads             []string
	projection        *controlplanev1.RecordRunToolCallRequest
}

func (c *integrationResultClient) ResolveIntegrationInvocation(_ context.Context, r *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	c.resolves = append(c.resolves, r)
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: c.invocation}, nil
}
func (c *integrationResultClient) GetIntegrationInvocation(_ context.Context, r *controlplanev1.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	c.reads = append(c.reads, r.InvocationRef)
	return &controlplanev1.GetIntegrationInvocationResponse{State: c.state, ResultSummary: "private provider result", SafeErrorCode: "INTEGRATION_REJECTED_BY_OWNER"}, nil
}
func (c *integrationResultClient) RecordRunToolCall(_ context.Context, r *controlplanev1.RecordRunToolCallRequest, _ ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	c.projection = r
	return &controlplanev1.RecordRunToolCallResponse{Event: &controlplanev1.RunEvent{Ref: "evt_fixture"}}, nil
}
func TestIntegrationTerminalWireAndOwnerProjection(t *testing.T) {
	for _, state := range []string{"SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME"} {
		t.Run(state, func(t *testing.T) {
			client := &integrationResultClient{state: state, invocation: "inv_fixture"}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			grant := integrationGrantFixture()
			input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", LeaseFence: "fence_fixture", LeaseGeneration: 2, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
			arguments := integrationArguments(grant, "private <input> & Пример")
			params, _ := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": arguments})
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`"call1"`), Params: params}, input)
			var wire struct {
				Result struct {
					StructuredContent map[string]any `json:"structuredContent"`
					Content           []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || wire.Result.IsError || wire.Result.StructuredContent["invocationRef"] != "inv_fixture" {
				t.Fatalf("terminal wire mismatch: %s", writer.Body.String())
			}
			var textResult map[string]any
			if len(wire.Result.Content) != 1 || wire.Result.Content[0].Type != "text" || json.Unmarshal([]byte(wire.Result.Content[0].Text), &textResult) != nil || textResult["invocationRef"] != "inv_fixture" {
				t.Fatal("MCP text compatibility is missing")
			}
			if wire.Result.StructuredContent["ok"] != (state == "SUCCEEDED") {
				t.Fatal("legacy ok result changed")
			}
			if state == "SUCCEEDED" && wire.Result.StructuredContent["result"] != "private provider result" || state == "UNKNOWN_OUTCOME" && wire.Result.StructuredContent["owner_decision_required"] != true {
				t.Fatal("legacy terminal shape changed")
			}
			if _, exists := wire.Result.StructuredContent["state"]; exists {
				t.Fatal("internal state leaked into MCP wire")
			}
			if _, exists := wire.Result.StructuredContent["inputSHA256"]; exists {
				t.Fatal("internal digest leaked into MCP wire")
			}
			r := client.resolves[0]
			if r.RunRef != input.RunRef || r.NodeRef != input.NodeRef || r.ConnectionRef != grant.ConnectionRef || r.CapabilityKey != grant.CapabilityKey || r.IdempotencyKey != stableKey(input.LeaseRef, `"call1"`) || len(client.reads) != 1 || client.reads[0] != "inv_fixture" {
				t.Fatal("owner invocation linkage changed")
			}
			p := client.projection
			if p == nil || p.LeaseRef != input.LeaseRef || p.Fence != input.LeaseFence || p.Generation != 2 || p.GrantRef != grant.Ref || p.CapabilityRef != grant.CapabilityKey {
				t.Fatal("owner event authority changed")
			}
			var observed struct {
				Version       int    `json:"version"`
				InvocationRef string `json:"invocationRef"`
				State         string `json:"state"`
				InputSHA256   string `json:"inputSHA256"`
			}
			if json.Unmarshal([]byte(p.SafeResult), &observed) != nil || observed.Version != 1 || observed.InvocationRef != "inv_fixture" || observed.State != state {
				t.Fatal("closed event result differs")
			}
			raw, _ := json.Marshal(arguments["input"])
			digest := sha256.Sum256(raw)
			if observed.InputSHA256 != hex.EncodeToString(digest[:]) || strings.Contains(p.SafeResult, "private") || len(p.SafeParameters.AsMap()) != 2 {
				t.Fatal("input digest or safe projection differs")
			}
		})
	}
}
func TestIntegrationRefAndGrantFailuresHaveNoFalseReceipt(t *testing.T) {
	for _, invalid := range []string{"", "short", "inv/foreign", "inv\nprivate", strings.Repeat("x", 129)} {
		t.Run("invalid-ref", func(t *testing.T) {
			c := &integrationResultClient{invocation: invalid, state: "SUCCEEDED"}
			s := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: c}}
			grant := integrationGrantFixture()
			input := runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
			if _, err := s.invoke(t.Context(), input, integrationArguments(grant, "x"), json.RawMessage(`1`)); err == nil || len(c.reads) != 0 {
				t.Fatal("invalid owner reference accepted")
			}
		})
	}
	c := &integrationResultClient{invocation: "inv_fixture", state: "SUCCEEDED"}
	s := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: c}}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	for _, field := range []string{"connection_ref", "capability_key", "definition_version", "definition_digest", "input_schema_sha256"} {
		args := integrationArguments(grant, "x")
		args[field] = "foreign"
		if _, err := s.invoke(t.Context(), input, args, json.RawMessage(`1`)); err == nil {
			t.Fatal("changed grant accepted")
		}
	}
	if len(c.resolves) != 0 {
		t.Fatal("foreign scope reached owner mutation")
	}
	if safeToolCallResult("invoke_integration", map[string]any{"invocationRef": "inv_forged"}, nil) != "TOOL_UNAVAILABLE" {
		t.Fatal("untyped result became authoritative projection")
	}
}
