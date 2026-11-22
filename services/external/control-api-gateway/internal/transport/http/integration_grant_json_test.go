package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// TestIntegrationGrantJSONStopsBeforeBothOwnerRPCs exercises real endpoint decoding with no
// configured owner client: reaching an RPC would panic and invalidate the no-effect proof.
// TestIntegrationGrantJSONStopsBeforeBothOwnerRPCs проверяет real endpoint decoding без owner
// client: достижение RPC вызывает panic и опровергает no-effect proof.
func TestIntegrationGrantJSONStopsBeforeBothOwnerRPCs(t *testing.T) {
	base := `"capabilityKey":"kubernetes.get","approvalPolicy":"NONE","enabled":true`
	for _, testCase := range []struct{ name, suffix string }{
		{"duplicate namespace", `,"kubernetesNamespace":"first","kubernetesNamespace":"second"`},
		{"case alias", `,"KubernetesNamespace":"second"`},
		{"mixed alias", `,"kubernetesNamespace":"first","KubernetesNamespace":"second"`},
		{"null namespace", `,"kubernetesNamespace":null`},
		{"null ref", `,"kubernetesGrantRef":null`},
		{"unknown authority", `,"owner":"private-sentinel"`},
		{"malformed UTF8", ",\"kubernetesNamespace\":\"" + string([]byte{0xff}) + "\""},
	} {
		for _, system := range []bool{false, true} {
			t.Run(testCase.name+map[bool]string{false: "/ordinary", true: "/system"}[system], func(t *testing.T) {
				raw := "{" + base + testCase.suffix + "}"
				if system {
					raw = `{"connectionRef":"connection_example",` + base + testCase.suffix + "}"
				}
				assertGrantSyntaxRejectedBeforeOwner(t, raw, system)
			})
		}
	}
	for _, system := range []bool{false, true} {
		prefix := ""
		if system {
			prefix = `"connectionRef":"connection_example",`
		}
		assertGrantSyntaxRejectedBeforeOwner(t, "{"+prefix+base+"}"+strings.Repeat(" ", int(maximumJSONBody)), system)
		assertGrantSyntaxRejectedBeforeOwner(t, "{"+prefix+`"capabilityKey":"kubernetes.get","approvalPolicy":"NONE"}`, system)
		assertGrantSyntaxRejectedBeforeOwner(t, "{"+prefix+`"capabilityKey":"kubernetes.get","approvalPolicy":"NONE","enabled":null}`, system)
	}
}

// assertGrantSyntaxRejectedBeforeOwner invokes the actual endpoint and checks closed diagnostics.
// assertGrantSyntaxRejectedBeforeOwner вызывает actual endpoint и проверяет closed diagnostics.
func assertGrantSyntaxRejectedBeforeOwner(t *testing.T, raw string, system bool) {
	t.Helper()
	server := &Server{}
	writer := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
	if system {
		server.ChangeSystemAssistantIntegrationGrant(writer, request, generated.ChangeSystemAssistantIntegrationGrantParams{IdempotencyKey: "test-grant-command", IfMatch: `"9"`})
	} else {
		server.ChangeIntegrationGrant(writer, request, "connection_example", generated.ChangeIntegrationGrantParams{IdempotencyKey: "test-grant-command", IfMatch: `"9"`})
	}
	if writer.Code != http.StatusBadRequest || !strings.Contains(writer.Body.String(), "INVALID_REQUEST") || strings.Contains(writer.Body.String(), "private-sentinel") {
		t.Fatal("invalid grant request escaped closed local admission")
	}
}

// TestIntegrationGrantJSONPreservesExplicitIntent accepts canonical false/omitted optional fields
// and exact Kubernetes scope/ref without converting them into actor or provider authority.
// TestIntegrationGrantJSONPreservesExplicitIntent принимает canonical false/omitted optional fields
// и exact Kubernetes scope/ref без преобразования в actor либо provider authority.
func TestIntegrationGrantJSONPreservesExplicitIntent(t *testing.T) {
	for _, raw := range []string{
		`{"capabilityKey":"github.issue.read","approvalPolicy":"NONE","enabled":false,"agentRef":"agent_example"}`,
		`{"capabilityKey":"kubernetes.get","approvalPolicy":"NONE","enabled":true,"agentRef":"agent_example","kubernetesNamespace":"example","kubernetesGrantRef":"grant_example"}`,
	} {
		writer := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
		body, ok := decodeIntegrationGrantJSON[generated.IntegrationGrantInput](writer, request)
		if !ok || body.CapabilityKey == "" || body.AgentRef == nil {
			t.Fatal("canonical explicit grant intent was rejected")
		}
		if body.CapabilityKey == "kubernetes.get" && (stringValue(body.KubernetesNamespace) != "example" || stringValue(body.KubernetesGrantRef) != "grant_example") {
			t.Fatal("exact Kubernetes selectors changed")
		}
	}
}
