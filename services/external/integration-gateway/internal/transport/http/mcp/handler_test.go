package mcp

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadRequestMethodPreservesStrictEnvelope(t *testing.T) {
	t.Parallel()
	raw := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	request := httptest.NewRequest("POST", "/mcp/v1", strings.NewReader(raw))
	method, err := readRequestMethod(request)
	if err != nil {
		t.Fatal(err)
	}
	if method != "initialize" {
		t.Fatalf("unexpected method: %q", method)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil || string(restored) != raw {
		t.Fatal("MCP request body was not restored")
	}
}

func TestReadRequestMethodRejectsBatchAndTrailingData(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`[{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}]`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}} {}`,
	} {
		request := httptest.NewRequest("POST", "/mcp/v1", strings.NewReader(raw))
		if _, err := readRequestMethod(request); err == nil {
			t.Fatalf("invalid MCP envelope was accepted: %q", raw)
		}
	}
}
