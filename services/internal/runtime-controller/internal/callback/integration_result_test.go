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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// integrationResultClient records synthetic resolve/read/projection calls, consumes
// queued resolve errors and returns a configured state; it is not a live owner ledger
// and deliberately does not authenticate transport or persist approval decisions.
// integrationResultClient записывает synthetic resolve/read/projection calls, расходует
// очередь resolve errors и возвращает заданный state; это не live owner ledger,
// transport не аутентифицируется и approval decisions не сохраняются.
type integrationResultClient struct {
	controlplanev1.RuntimeWorkServiceClient
	state, invocation string
	resolves          []*controlplanev1.ResolveIntegrationInvocationRequest
	reads             []string
	projection        *controlplanev1.RecordRunToolCallRequest
	resolveErrors     []error
}

// ResolveIntegrationInvocation records the original request pointer, consumes one
// queued error if present, otherwise returns the fixed locator. No defensive copy,
// idempotency enforcement or integration effect is provided by this fake.
// ResolveIntegrationInvocation записывает исходный request pointer, расходует одну
// queued error при наличии, иначе выдаёт фиксированный locator. Этот fake
// не копирует request, не исполняет idempotency и integration effect.
func (c *integrationResultClient) ResolveIntegrationInvocation(_ context.Context, r *controlplanev1.ResolveIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	c.resolves = append(c.resolves, r)
	if len(c.resolveErrors) > 0 {
		err := c.resolveErrors[0]
		c.resolveErrors = c.resolveErrors[1:]
		return nil, err
	}
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: c.invocation}, nil
}

// GetIntegrationInvocation records the requested ref and returns configured state
// with deliberately private result text, enabling wire/redaction assertions without
// reading an actual provider or checking invocation ownership.
// GetIntegrationInvocation записывает ref и возвращает заданный state с намеренно
// private result text для wire/redaction assertions без чтения реального provider
// и проверки владения invocation.
func (c *integrationResultClient) GetIntegrationInvocation(_ context.Context, r *controlplanev1.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	c.reads = append(c.reads, r.InvocationRef)
	return &controlplanev1.GetIntegrationInvocationResponse{State: c.state, ResultSummary: "private provider result", SafeErrorCode: "INTEGRATION_REJECTED_BY_OWNER"}, nil
}

// TestIntegrationResolveRetriesSameIntentOnce injects one Unavailable and verifies
// the two resolve requests retain the same idempotency key; it proves retry intent
// stability, not durable deduplication by an owner or external provider.
// TestIntegrationResolveRetriesSameIntentOnce подставляет один Unavailable и проверяет
// одинаковый idempotency key двух resolve requests; доказана стабильность retry intent,
// а не durable deduplication owner или внешнего provider.
func TestIntegrationResolveRetriesSameIntentOnce(t *testing.T) {
	client := &integrationResultClient{
		state: "SUCCEEDED", invocation: "inv_fixture",
		resolveErrors: []error{status.Error(codes.Unavailable, "temporary")},
	}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	if _, err := server.invoke(t.Context(), input, integrationArguments(grant, "x"), json.RawMessage(`"call"`)); err != nil {
		t.Fatal(err)
	}
	if len(client.resolves) != 2 || client.resolves[0].IdempotencyKey != client.resolves[1].IdempotencyKey {
		t.Fatalf("resolve retry changed intent: %#v", client.resolves)
	}
}

// RecordRunToolCall retains the latest projection request and returns a synthetic
// event locator; accepting it does not prove a real durable audit commit.
// RecordRunToolCall сохраняет последний projection request и возвращает synthetic
// event locator; его принятие не доказывает реальный durable audit commit.
func (c *integrationResultClient) RecordRunToolCall(_ context.Context, r *controlplanev1.RecordRunToolCallRequest, _ ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	c.projection = r
	return &controlplanev1.RecordRunToolCallResponse{Event: &controlplanev1.RunEvent{Ref: "evt_fixture"}}, nil
}

// TestIntegrationTerminalWireAndOwnerProjection checks structured/text MCP results
// for five configured owner states, including UNKNOWN_OUTCOME needing a decision,
// while internal state/digest remain only in a lease-fenced safe projection.
// Input digests are computed from actual fixture JSON; no external effect occurs.
// TestIntegrationTerminalWireAndOwnerProjection проверяет structured/text MCP results
// пяти заданных owner states, включая UNKNOWN_OUTCOME с необходимым решением;
// internal state/digest остаются лишь в lease-fenced safe projection. Input digests
// считаются по реальному fixture JSON; внешних эффектов нет.
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

// TestIntegrationCatalogInvalidInputReturnsRetryableMCPError checks mixed catalog
// selectors yield closed retryable guidance and a failed safe owner projection,
// rather than treating invalid input as a successful catalog read.
// TestIntegrationCatalogInvalidInputReturnsRetryableMCPError проверяет closed retryable
// guidance и failed safe owner projection смешанных catalog selectors
// вместо признания неверного input успешным catalog read.
func TestIntegrationCatalogInvalidInputReturnsRetryableMCPError(t *testing.T) {
	client := &integrationResultClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", LeaseFence: "fence_fixture", LeaseGeneration: 2,
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{integrationGrantFixture()}}
	params, _ := json.Marshal(map[string]any{"name": "get_integration_catalog", "arguments": map[string]any{
		"query": "test", "connection_ref": input.IntegrationGrants[0].ConnectionRef,
	}})
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`"call1"`), Params: params}, input)
	var wire struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
			IsError           bool           `json:"isError"`
		} `json:"result"`
	}
	if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || !wire.Result.IsError ||
		wire.Result.StructuredContent["error_code"] != "CATALOG_INPUT_INVALID" || wire.Result.StructuredContent["retryable"] != true {
		t.Fatalf("invalid catalog input did not return retry guidance: %s", writer.Body.String())
	}
	if client.projection == nil || client.projection.GetCapabilityRef() != "platform.integration.catalog" ||
		client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.projection.GetSafeResult() != "TOOL_UNAVAILABLE" {
		t.Fatalf("invalid catalog input lost safe owner projection: %#v", client.projection)
	}
}

// TestIntegrationCatalogMissingSelectionPointsToBoundIndex checks a guessed operation
// name produces guidance back to the bound grant index and a failed projection;
// it neither discovers arbitrary remote operations nor executes them.
// TestIntegrationCatalogMissingSelectionPointsToBoundIndex проверяет guidance к bound
// grant index и failed projection при выдуманном operation name; произвольные
// remote operations не обнаруживаются и не выполняются.
func TestIntegrationCatalogMissingSelectionPointsToBoundIndex(t *testing.T) {
	client := &integrationResultClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", LeaseFence: "fence_fixture", LeaseGeneration: 2,
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	params, _ := json.Marshal(map[string]any{"name": "get_integration_catalog", "arguments": map[string]any{
		"connection_ref": grant.ConnectionRef, "capability_key": "guessedOperationId",
	}})
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`"call1"`), Params: params}, input)
	var wire struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
			IsError           bool           `json:"isError"`
		} `json:"result"`
	}
	if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || !wire.Result.IsError ||
		wire.Result.StructuredContent["error_code"] != "CATALOG_INPUT_INVALID" || wire.Result.StructuredContent["retryable"] != true {
		t.Fatal("missing integration selection did not return a retryable catalog error")
	}
	guidance, _ := wire.Result.StructuredContent["guidance"].(string)
	if !strings.Contains(guidance, "get_integration_catalog with {}") ||
		!strings.Contains(guidance, "grant_ref") || !strings.Contains(guidance, "operationId") {
		t.Fatal("missing integration selection did not direct the caller to exact bound identifiers")
	}
	if client.projection == nil || client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED {
		t.Fatal("missing integration selection lost its owner projection")
	}
}

// TestIntegrationInvalidGrantReturnsBoundedRetryGuidance checks a foreign grant yields
// a closed retryable error with no reflected private input, owner resolve or projection.
// TestIntegrationInvalidGrantReturnsBoundedRetryGuidance проверяет closed retryable
// ошибку чужого grant без отражения private input, owner resolve и projection.
func TestIntegrationInvalidGrantReturnsBoundedRetryGuidance(t *testing.T) {
	client := &integrationResultClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	grant := integrationGrantFixture()
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", LeaseFence: "fence_fixture", LeaseGeneration: 2,
		IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	params, _ := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": map[string]any{
		"grant_ref": "igr_foreign", "input": map[string]any{"value": "private-input"},
	}})
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`"call1"`), Params: params}, input)
	var wire struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
			IsError           bool           `json:"isError"`
		} `json:"result"`
	}
	if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || !wire.Result.IsError ||
		wire.Result.StructuredContent["error_code"] != "INTEGRATION_INPUT_INVALID" || wire.Result.StructuredContent["retryable"] != true {
		t.Fatalf("invalid integration grant did not return bounded guidance: %s", writer.Body.String())
	}
	if strings.Contains(writer.Body.String(), "private-input") || len(client.resolves) != 0 || client.projection != nil {
		t.Fatal("invalid grant reached owner mutation or leaked input")
	}
}

// TestIntegrationRefAndGrantFailuresHaveNoFalseReceipt checks malformed owner locators
// prevent subsequent reads, extra/missing/foreign grant selectors prevent resolve,
// and an untyped fabricated result cannot become an authoritative safe projection.
// TestIntegrationRefAndGrantFailuresHaveNoFalseReceipt проверяет запрет subsequent reads
// для malformed owner locators, запрет resolve для лишних/missing/foreign selectors
// и невозможность authoritative safe projection из untyped fabricated result.
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
	for _, ref := range []string{"", "igr_foreign"} {
		args := integrationArguments(grant, "x")
		args["grant_ref"] = ref
		if _, err := s.invoke(t.Context(), input, args, json.RawMessage(`1`)); err == nil {
			t.Fatal("foreign grant reference accepted")
		}
	}
	if len(c.resolves) != 0 {
		t.Fatal("foreign grant reference reached owner mutation")
	}
	if safeToolCallResult("invoke_integration", map[string]any{"invocationRef": "inv_forged"}, nil) != "TOOL_UNAVAILABLE" {
		t.Fatal("untyped result became authoritative projection")
	}
}

// TestIntegrationLegacyLocatorIsRejectedBeforeOwnerMutation exercises obsolete and
// mixed selector shapes as negative fixtures only: both must fail before owner RPC
// or projection and must not reflect private input; no legacy decoder is restored.
// TestIntegrationLegacyLocatorIsRejectedBeforeOwnerMutation использует старую и mixed
// selector shapes только как negative fixtures: обе отвергаются до owner RPC/projection
// без отражения private input; legacy decoder не восстанавливается.
func TestIntegrationLegacyLocatorIsRejectedBeforeOwnerMutation(t *testing.T) {
	grant := integrationGrantFixture()
	legacy := map[string]any{
		"connection_ref": grant.ConnectionRef, "capability_key": grant.CapabilityKey,
		"definition_version": grant.DefinitionVersion, "definition_digest": grant.DefinitionDigest,
		"input_schema_sha256": grant.InputSchemaSHA256, "input": map[string]any{"value": "private-input"},
	}
	for _, withRef := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "mixed"}[withRef], func(t *testing.T) {
			args := make(map[string]any, len(legacy)+1)
			for key, value := range legacy {
				args[key] = value
			}
			if withRef {
				args["grant_ref"] = grant.Ref
			}
			client := &integrationResultClient{}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			input := runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
			params, _ := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": args})
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`1`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || !wire.Result.IsError || wire.Result.StructuredContent["error_code"] != "INTEGRATION_INPUT_INVALID" {
				t.Fatalf("legacy locator was not rejected: %s", writer.Body.String())
			}
			if len(client.resolves) != 0 || len(client.reads) != 0 || client.projection != nil || strings.Contains(writer.Body.String(), "private-input") {
				t.Fatal("legacy locator reached owner state or leaked input")
			}
		})
	}
}
