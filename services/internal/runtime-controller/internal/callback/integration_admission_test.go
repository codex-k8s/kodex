package callback

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestIntegrationAdmissionBlockedWireAndOwnerProjection covers the actual MCP call path,
// not just a serializer. READY returns attention without polling for an impossible lease;
// UNKNOWN preserves its original outcome, while private provider text never enters the response.
// TestIntegrationAdmissionBlockedWireAndOwnerProjection проверяет настоящий MCP call path,
// а не только serializer. READY возвращает attention без ожидания запрещённого lease;
// UNKNOWN сохраняет исходный outcome, private provider text не попадает в ответ.
func TestIntegrationAdmissionBlockedWireAndOwnerProjection(t *testing.T) {
	for _, state := range []string{"READY", "UNKNOWN_OUTCOME"} {
		t.Run(state, func(t *testing.T) {
			originalError := ""
			if state == "UNKNOWN_OUTCOME" {
				originalError = "INTEGRATION_OUTCOME_UNKNOWN"
			}
			client := &integrationResultClient{invocation: "inv_fixture", admissionResponse: &cp.GetIntegrationInvocationResponse{
				State: state, SafeErrorCode: originalError, ResultSummary: "private provider result", AdmissionBlocked: true,
				AdmissionBlockReason: cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED,
			}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			grant := integrationGrantFixture()
			input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "node_fixture", LeaseRef: "lease_fixture", LeaseFence: "fence_fixture", LeaseGeneration: 2, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
			params, _ := json.Marshal(map[string]any{"name": "invoke_integration", "arguments": integrationArguments(grant, "fixture")})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/", nil).WithContext(ctx), mcpRequest{ID: json.RawMessage(`"blocked-call"`), Params: params}, input)
			var wire struct {
				Result struct {
					Structured map[string]any `json:"structuredContent"`
					Content    []struct {
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || wire.Result.IsError || ctx.Err() != nil || len(client.reads) != 1 {
				t.Fatal("admission block did not stop state polling immediately")
			}
			value := wire.Result.Structured
			if value["ok"] != false || value["admissionBlocked"] != true || value["admissionBlockReason"] != integrationAdmissionCurrentDenied ||
				value["error_code"] != originalError || value["owner_decision_required"] != true {
				t.Fatal("MCP attention result changed the owner outcome or block reason")
			}
			var text map[string]any
			if len(wire.Result.Content) != 1 || json.Unmarshal([]byte(wire.Result.Content[0].Text), &text) != nil ||
				text["admissionBlocked"] != true || text["admissionBlockReason"] != integrationAdmissionCurrentDenied {
				t.Fatal("MCP text compatibility lost blocked admission")
			}
			if strings.Contains(writer.Body.String(), "private provider result") || value["state"] != nil || value["receiptRef"] != nil || client.projection == nil {
				t.Fatal("attention result exposed private content or invented receipt metadata")
			}
			var receipt map[string]any
			if json.Unmarshal([]byte(client.projection.GetSafeResult()), &receipt) != nil || receipt["state"] != state ||
				receipt["admissionBlocked"] != true || receipt["admissionBlockReason"] != integrationAdmissionCurrentDenied {
				t.Fatal("durable safe result lost the exact owner state or reason")
			}
		})
	}
}

// TestIntegrationAdmissionMarkerRejectsContradictions denies unknown or inconsistent owner markers
// before polling, and prevents fabricated attention metadata from becoming a successful receipt.
// TestIntegrationAdmissionMarkerRejectsContradictions отклоняет неизвестные и противоречивые
// owner markers до polling и не превращает fabricated attention metadata в successful receipt.
func TestIntegrationAdmissionMarkerRejectsContradictions(t *testing.T) {
	for _, value := range []*cp.GetIntegrationInvocationResponse{
		nil,
		{State: "READY", AdmissionBlocked: true},
		{State: "READY", AdmissionBlockReason: cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED},
		{State: "READY", AdmissionBlocked: true, AdmissionBlockReason: cp.SSHComposeAdmissionBlockReason(77)},
		{State: "SUCCEEDED", AdmissionBlocked: true, AdmissionBlockReason: cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED},
		{State: "UNKNOWN_OUTCOME", AdmissionBlocked: true, AdmissionBlockReason: cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED, EffectReceiptRef: "erc_unexpected_fixture"},
	} {
		if _, _, err := ownerIntegrationAdmissionMarker(value); err != errIntegrationAdmissionMarkerInvalid {
			t.Fatal("invalid owner admission marker accepted")
		}
	}
	for _, result := range []integrationToolResult{
		{OK: true, AdmissionBlocked: true, AdmissionBlockReason: integrationAdmissionCurrentDenied, state: "SUCCEEDED"},
		{AdmissionBlockReason: integrationAdmissionCurrentDenied, state: "READY"},
		{AdmissionBlocked: true, AdmissionBlockReason: "private-unrecognized-cause", state: "UNKNOWN_OUTCOME"},
	} {
		if _, err := json.Marshal(result); err == nil {
			t.Fatal("invalid admission marker serialized as a tool result")
		}
	}
}
