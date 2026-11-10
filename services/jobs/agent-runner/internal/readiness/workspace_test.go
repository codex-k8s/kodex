package readiness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestWorkspaceOwnerAdmissionRequiresExactObservedStructuredResult rejects revoked grants,
// mismatched responses and raw fallback content before a local operation can be admitted.
// TestWorkspaceOwnerAdmissionRequiresExactObservedStructuredResult отклоняет отозванные права,
// чужие ответы и текст вместо структурированного результата до локальной операции.
func TestWorkspaceOwnerAdmissionRequiresExactObservedStructuredResult(t *testing.T) {
	for name, fixture := range map[string]struct {
		status   int
		body     string
		accepted bool
	}{
		"observed":       {200, `{"jsonrpc":"2.0","id":"workspace-admission","result":{"structuredContent":{"profile":"managed-workspace-v1"}}}`, true},
		"revoked":        {403, `{}`, false},
		"unknown":        {503, `{}`, false},
		"different-call": {200, `{"jsonrpc":"2.0","id":"another-call","result":{"structuredContent":{"profile":"managed-workspace-v1"}}}`, false},
		"raw-fallback":   {200, `{"jsonrpc":"2.0","id":"workspace-admission","result":{"content":[{"type":"text","text":"allowed"}]}}`, false},
		"tool-error":     {200, `{"jsonrpc":"2.0","id":"workspace-admission","result":{"isError":true,"structuredContent":{"profile":"managed-workspace-v1"}}}`, false},
		"duplicate":      {200, `{"jsonrpc":"2.0","id":"workspace-admission","result":{"structuredContent":{},"structuredContent":{"profile":"managed-workspace-v1"}}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer synthetic-local-token" {
					t.Error("owner transport lost its fixed local authentication")
				}
				var request struct {
					ID     string `json:"id"`
					Params struct {
						Name      string         `json:"name"`
						Arguments map[string]any `json:"arguments"`
					} `json:"params"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.ID != "workspace-admission" || request.Params.Name != "get_workspace_policy" || len(request.Params.Arguments) != 0 {
					t.Error("owner admission changed scope")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(fixture.status)
				_, _ = w.Write([]byte(fixture.body))
			}))
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			value, err := workspaceOwnerCall(server.Client(), endpoint, "synthetic-local-token")(t.Context(), "workspace-admission", "get_workspace_policy", map[string]any{})
			if (err == nil) != fixture.accepted || calls != 1 {
				t.Fatalf("incorrect admission/retry: accepted=%t calls=%d error=%v", err == nil, calls, err)
			}
			if fixture.accepted && value["profile"] != "managed-workspace-v1" {
				t.Fatal("observed policy was lost")
			}
		})
	}
}
