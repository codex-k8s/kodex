package callback

import (
	"bytes"
	"encoding/json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWorkspaceRejectionLogsOnlyClosedMetadata drives the real unauthenticated handler and
// a typed owner-RPC failure. The fixed classifications preserve status while sensitive header,
// body and upstream-error text are absent from structured diagnostics.
// TestWorkspaceRejectionLogsOnlyClosedMetadata вызывает настоящий unauthorized handler и
// typed owner-RPC failure. Fixed classification сохраняет status, не раскрывая sensitive
// header, body либо upstream-error text в structured diagnostics.
func TestWorkspaceRejectionLogsOnlyClosedMetadata(t *testing.T) {
	const private = "synthetic-private-request-marker"
	var log bytes.Buffer
	server := &Server{logger: slog.New(slog.NewJSONHandler(&log, nil))}
	request := httptest.NewRequest(http.MethodPost, "/v1/executions/synthetic/workspace", strings.NewReader(private))
	request.Header.Set("Authorization", "Bearer "+private)
	writer := httptest.NewRecorder()
	server.observeWorkspace(writer, request, "synthetic")
	if writer.Code != http.StatusNotFound {
		t.Fatal("authorization response changed")
	}
	var row map[string]any
	if json.Unmarshal(log.Bytes(), &row) != nil || row["error_class"] != "AUTHORITY" || strings.Contains(log.String(), private) {
		t.Fatal("rejected request diagnostic is missing or exposes private input")
	}
	log.Reset()
	server.logWorkspaceRejection(t.Context(), "OWNER_RPC", status.Error(codes.PermissionDenied, private))
	if json.Unmarshal(log.Bytes(), &row) != nil || row["error_class"] != "OWNER_RPC" || row["rpc_code"] != "PermissionDenied" || strings.Contains(log.String(), private) {
		t.Fatal("owner diagnostic exposes raw error or loses closed code")
	}
}
