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

func TestMCPAcceptsInitializedNotificationWithoutAResponse(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	))
	response := httptest.NewRecorder()

	server.serveMCP(response, request, runtimecontract.RunnerInput{SystemAssistant: true})

	if response.Code != http.StatusAccepted || response.Body.Len() != 0 {
		t.Fatalf("initialized notification response = %d %q", response.Code, response.Body.String())
	}
}

func TestMCPRejectsInitializedNotificationWithAuthorityLikeParams(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{"actor":"owner"}}`,
	))
	response := httptest.NewRecorder()

	server.serveMCP(response, request, runtimecontract.RunnerInput{SystemAssistant: true})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid initialized notification response = %d", response.Code)
	}
}

func TestMCPStillReturnsSystemAssistantToolsAfterInitialization(t *testing.T) {
	t.Parallel()
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`,
	))
	response := httptest.NewRecorder()

	server.serveMCP(response, request, runtimecontract.RunnerInput{SystemAssistant: true})

	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"name":"get_configuration_catalog"`)) {
		t.Fatalf("tools/list response = %d %q", response.Code, response.Body.String())
	}
}

func TestControlFailureClassPreservesAuthoritySnapshotRollback(t *testing.T) {
	t.Parallel()

	err := status.Error(codes.FailedPrecondition, "authorization snapshot rollback rejected")
	if got := controlFailureClass(err); got != "authority_snapshot_rollback" {
		t.Fatalf("control failure class = %q, want authority_snapshot_rollback", got)
	}
}
