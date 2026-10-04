package runtimecontract

import "testing"

func TestRuntimeProviderRoutesAreExactAndIndependentOfUserAccess(t *testing.T) {
	for _, row := range []struct {
		host, path, method string
		http, websocket    bool
	}{
		{"api.openai.com", "/v1/responses", "POST", true, false},
		{"api.openai.com", "/v1/responses", "GET", false, true},
		{"api.openai.com", "/v1/models", "GET", true, false},
		{"chatgpt.com", "/backend-api/codex/responses", "POST", true, false},
		{"chatgpt.com", "/backend-api/codex/responses", "GET", false, true},
		{"chatgpt.com", "/backend-api/codex/models", "GET", true, false},
		{"chatgpt.com", "/backend-api/wham/usage", "GET", true, false},
		{"auth.openai.com", "/oauth/token", "POST", true, false},
		{"auth.openai.com", "/oauth/token", "GET", false, false},
		{"chatgpt.com", "/", "GET", false, false},
		{"chatgpt.com", "/backend-api/codex/responses/other", "POST", false, false},
		{"api.openai.com", "/v1/responses", "DELETE", false, false},
		{"other.api.openai.com", "/v1/responses", "POST", false, false},
		{"github.com", "/v1/responses", "POST", false, false},
	} {
		if RuntimeProviderAllowsRequest(row.host, row.path, row.method) != row.http ||
			RuntimeProviderAllowsWebSocket(row.host, row.path, row.method) != row.websocket {
			t.Fatalf("provider route mismatch for %s %s %s", row.host, row.method, row.path)
		}
	}
}

func TestRuntimeEnvironmentRejectsTrustAndVerificationOverrides(t *testing.T) {
	for _, name := range []string{"CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO",
		"NODE_TLS_REJECT_UNAUTHORIZED", "GIT_SSL_NO_VERIFY", "PYTHONHTTPSVERIFY", "CURL_SSL_BACKEND", "ALL_PROXY"} {
		if ValidRuntimeEnvironmentName(name) {
			t.Fatalf("reserved transport environment name accepted: %s", name)
		}
	}
}
