package opencode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeRejectedKeyHasNoFallback runs real OpenCode against a rejecting synthetic provider.
// No other route, account, key or model is configured, and no raw provider error reaches the caller.
// TestNativeRejectedKeyHasNoFallback запускает настоящий OpenCode с отвергающим synthetic provider.
// Другой route/account/key/model не настроен; raw provider error не попадает к вызывающему.
func TestNativeRejectedKeyHasNoFallback(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	var requests atomic.Int64
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"SYNTHETIC_PRIVATE_ERROR_MARKER","type":"authentication_error","code":"invalid_api_key"}}`))
	}))
	t.Cleanup(model.Close)
	config := nativeConfiguration()
	config["provider"].(map[string]any)["synthetic"].(map[string]any)["options"].(map[string]string)["baseURL"] = model.URL + "/v1"
	server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	session, err := server.client.CreateSession(t.Context(), "Native rejected-key qualification")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_41234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Return a synthetic acknowledgement."}}}, func(string) bool { return false }, func(string, string) error { return nil })
	if !errors.Is(err, ErrAuthentication) || requests.Load() != 1 || result.FinalText != "" {
		t.Fatalf("credential rejection lost its closed outcome: requests=%d error=%v", requests.Load(), err)
	}
	if strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_ERROR_MARKER") {
		t.Fatal("provider error body leaked")
	}
}
