package callback

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestMCPAcceptsInitializedNotificationWithoutAResponse checks standard no-ID initialized
// notification returns HTTP202 with no body rather than a JSON-RPC result envelope.
// TestMCPAcceptsInitializedNotificationWithoutAResponse проверяет HTTP202/empty body
// стандартного no-ID initialized notification вместо JSON-RPC result envelope.
func TestMCPAcceptsInitializedNotificationWithoutAResponse(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	))
	response := httptest.NewRecorder()

	server.serveMCP(response, request, runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem})

	if response.Code != http.StatusAccepted || response.Body.Len() != 0 {
		t.Fatalf("initialized notification response = %d %q", response.Code, response.Body.String())
	}
}

// TestMCPRejectsInitializedNotificationWithAuthorityLikeParams checks an actor-like
// parameter in initialized notification yields HTTP400 instead of caller authority.
// TestMCPRejectsInitializedNotificationWithAuthorityLikeParams проверяет HTTP400 actor-like
// parameter initialized notification вместо caller authority.
func TestMCPRejectsInitializedNotificationWithAuthorityLikeParams(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{"actor":"owner"}}`,
	))
	response := httptest.NewRecorder()

	server.serveMCP(response, request, runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid initialized notification response = %d", response.Code)
	}
}

// TestMCPStillReturnsSystemAssistantToolsAfterInitialization checks a tools/list
// request with normal assistant scope retains configuration discovery; despite its
// historical name it does not carry state from a preceding initialized notification.
// TestMCPStillReturnsSystemAssistantToolsAfterInitialization проверяет configuration
// discovery tools/list normal assistant scope; несмотря на историческое имя,
// состояние предыдущего initialized notification в тест не переносится.
func TestMCPStillReturnsSystemAssistantToolsAfterInitialization(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`,
	))
	response := httptest.NewRecorder()

	server.serveMCP(response, request, runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem})

	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"name":"get_configuration_catalog"`)) {
		t.Fatalf("tools/list response = %d %q", response.Code, response.Body.String())
	}
}

// TestControlFailureClassPreservesAuthoritySnapshotRollback checks the registered
// FailedPrecondition rollback diagnostic maps to its closed failure class.
// It tests classification only, not actual replay storage or rollback enforcement.
// TestControlFailureClassPreservesAuthoritySnapshotRollback проверяет closed failure class
// registered FailedPrecondition rollback diagnostic; это лишь classification,
// не actual replay storage и не rollback enforcement.
func TestControlFailureClassPreservesAuthoritySnapshotRollback(t *testing.T) {
	t.Parallel()

	err := status.Error(codes.FailedPrecondition, "authorization snapshot rollback rejected")
	if got := controlFailureClass(err); got != "authority_snapshot_rollback" {
		t.Fatalf("control failure class = %q, want authority_snapshot_rollback", got)
	}
}
