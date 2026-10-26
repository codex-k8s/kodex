package callback

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestIntegrationReceiptToolWire exercises the actual MCP response, preserving the same owner
// locator in text/structured results and replay without creating or inferring a receipt locally.
// TestIntegrationReceiptToolWire проверяет настоящий MCP response: один owner locator сохраняется
// в text/structured results и replay; локального создания либо вывода receipt нет.
func TestIntegrationReceiptToolWire(t *testing.T) {
	for _, state := range []string{"SUCCEEDED", "FAILED", "WAITING_APPROVAL", "UNKNOWN_OUTCOME"} {
		t.Run(state, func(t *testing.T) {
			client := &integrationResultClient{state: state, invocation: "inv_fixture123", receipt: "erc_fixture123"}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			grant := integrationGrantFixture()
			input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", LeaseGeneration: 2, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
			params, _ := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": integrationArguments(grant, "PRIVATE_INPUT")})
			var previous map[string]any
			for attempt := 0; attempt < 2; attempt++ {
				writer := httptest.NewRecorder()
				server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`"call1"`), Params: params}, input)
				var wire struct {
					Result struct {
						StructuredContent map[string]any `json:"structuredContent"`
						Content           []struct {
							Text string `json:"text"`
						} `json:"content"`
						IsError bool `json:"isError"`
					} `json:"result"`
				}
				if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || wire.Result.IsError || len(wire.Result.Content) != 1 {
					t.Fatal("integration tool wire invalid")
				}
				var text map[string]any
				if json.Unmarshal([]byte(wire.Result.Content[0].Text), &text) != nil || !reflect.DeepEqual(text, wire.Result.StructuredContent) {
					t.Fatal("integration text and structured receipt differ")
				}
				if state == "SUCCEEDED" {
					if text["receiptRef"] != client.receipt {
						t.Fatal("owner effect receipt locator missing")
					}
				} else if _, exists := text["receiptRef"]; exists {
					t.Fatal("non-success gained effect receipt")
				}
				if text["invocationRef"] != client.invocation || strings.Contains(writer.Body.String(), "PRIVATE_INPUT") {
					t.Fatal("integration locator changed or input exposed")
				}
				if attempt == 1 && !reflect.DeepEqual(previous, text) {
					t.Fatal("replay changed owner receipt projection")
				}
				previous = text
			}
			if len(client.resolves) != 2 || client.resolves[0].IdempotencyKey != client.resolves[1].IdempotencyKey {
				t.Fatal("receipt projection changed invocation identity")
			}
		})
	}
}

// TestIntegrationReceiptRejectsMalformedOwnerRef rejects credential-like, injected or wrong-prefix
// owner metadata without echoing it, while preserving empty receipt compatibility for legacy reads.
// TestIntegrationReceiptRejectsMalformedOwnerRef закрыто отклоняет credential-like, injected и
// wrong-prefix metadata без отражения; legacy read без receipt сохраняет прежнюю форму.
func TestIntegrationReceiptRejectsMalformedOwnerRef(t *testing.T) {
	for _, ref := range []string{"inv_fixture123", "erc_short", "erc_" + strings.Repeat("x", 93), "erc_fixture/secret", "erc_fixture\nsecret", "credential=PRIVATE_SECRET"} {
		t.Run("malformed", func(t *testing.T) {
			client := &integrationResultClient{state: "SUCCEEDED", invocation: "inv_fixture123", receipt: ref}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			grant := integrationGrantFixture()
			input := runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
			result, err := server.invoke(t.Context(), input, integrationArguments(grant, "PRIVATE_INPUT"), json.RawMessage(`1`))
			if err == nil || result != nil || strings.Contains(err.Error(), ref) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("malformed owner receipt accepted or exposed")
			}
			params, _ := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": integrationArguments(grant, "PRIVATE_INPUT")})
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`1`), Params: params}, input)
			if strings.Contains(writer.Body.String(), ref) || strings.Contains(writer.Body.String(), "PRIVATE") || strings.Contains(writer.Body.String(), `"receiptRef"`) {
				t.Fatal("malformed owner receipt leaked through MCP error")
			}
		})
	}
	client := &integrationResultClient{state: "SUCCEEDED", invocation: "inv_fixture123"}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	grant := integrationGrantFixture()
	grant.Risk = "READ"
	result, err := server.invoke(t.Context(), runtimecontract.RunnerInput{IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}, integrationArguments(grant, "legacy"), json.RawMessage(`1`))
	raw, marshalErr := json.Marshal(result)
	if err != nil || marshalErr != nil || strings.Contains(string(raw), "receiptRef") {
		t.Fatal("legacy read invented a receipt or lost success")
	}
}

// TestIntegrationReceiptMarshalClosed validates the actual custom serializer, including boundary
// locators and a fabricated failed result, without reflecting malformed owner bytes in errors.
// TestIntegrationReceiptMarshalClosed проверяет настоящий custom serializer, граничные locators
// и fabricated failed result без отражения повреждённых owner bytes в ошибках.
func TestIntegrationReceiptMarshalClosed(t *testing.T) {
	for _, tc := range []struct {
		ref      string
		ok, want bool
	}{
		{"erc_12345678", true, true}, {"erc_" + strings.Repeat("x", 92), true, true}, {"", true, true},
		{"erc_short", true, false}, {"erc_" + strings.Repeat("x", 93), true, false}, {"erc_PRIVATE/secret", true, false}, {"erc_12345678", false, false},
	} {
		t.Run("marshal", func(t *testing.T) {
			raw, err := json.Marshal(integrationToolResult{OK: tc.ok, InvocationRef: "inv_fixture123", ReceiptRef: tc.ref})
			if tc.want {
				if err != nil {
					t.Fatal("valid receipt serialization rejected")
				}
				return
			}
			if err == nil || len(raw) != 0 || strings.Contains(err.Error(), tc.ref) {
				t.Fatal("invalid receipt serialized or reflected")
			}
		})
	}
}
