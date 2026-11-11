package codex

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestNativeMCPBridgeSeparatesSDKAndReceiptIdentity tests both directions without executing an external effect.
// Proof material must disappear before the owner, while the SDK receives its own transport correlation ID.
// TestNativeMCPBridgeSeparatesSDKAndReceiptIdentity проверяет оба направления без внешнего effect.
// Proof удаляется до owner, SDK получает свой transport correlation ID.
func TestNativeMCPBridgeSeparatesSDKAndReceiptIdentity(t *testing.T) {
	input := model.Input{Provider: "opencode", RuntimeRevisionDigest: strings.Repeat("b", 64)}
	key := strings.Repeat("a", 64)
	payload := `{"session_id":"ses_native","call_id":"call_1","tool":"kodex_read_task_session","revision":"` + input.RuntimeRevisionDigest + `","input":{}}`
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(payload))
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": "read_task_session", "arguments": map[string]any{"__kodex_native": map[string]string{"payload": payload, "mac": hex.EncodeToString(mac.Sum(nil))}}}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:12345/mcp", strings.NewReader(string(raw)))
	if err := bindNativeMCPRequest(request, input, key); err != nil {
		t.Fatal(err)
	}
	forwarded, err := io.ReadAll(request.Body)
	var rpc struct {
		ID string `json:"id"`
	}
	if err != nil || json.Unmarshal(forwarded, &rpc) != nil || !strings.HasPrefix(rpc.ID, "ncall_") || strings.Contains(string(forwarded), "__kodex_native") {
		t.Fatal("bridge forwarded unverified identity or private proof")
	}
	response := &http.Response{Request: request, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":"` + rpc.ID + `","result":{"structuredContent":{"state":"CONFIRMED"}}}`))}
	if err := restoreNativeMCPResponse(response); err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(reply), `"id":7`) || !strings.Contains(string(reply), `"state":"CONFIRMED"`) || strings.Contains(string(reply), "ncall_") {
		t.Fatal("SDK response correlation or result changed")
	}
	forged := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:12345/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"ncall_`+strings.Repeat("c", 64)+`","method":"tools/call","params":{"name":"read_task_session","arguments":{}}}`))
	if bindNativeMCPRequest(forged, input, key) == nil {
		t.Fatal("caller chose native receipt identity without image hook")
	}
}
