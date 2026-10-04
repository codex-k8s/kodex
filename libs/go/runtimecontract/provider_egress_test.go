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
		{"chatgpt.com", "/backend-api/wham/accounts/check", "GET", true, false},
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

func TestRuntimeProviderAccountsCheckIsExactHTTPOnly(t *testing.T) {
	const path = "/backend-api/wham/accounts/check"
	for _, row := range []struct {
		name, host, path, method string
		allowed                  bool
	}{
		{"exact", "chatgpt.com", path, "GET", true},
		{"post", "chatgpt.com", path, "POST", false},
		{"head", "chatgpt.com", path, "HEAD", false},
		{"put", "chatgpt.com", path, "PUT", false},
		{"patch", "chatgpt.com", path, "PATCH", false},
		{"delete", "chatgpt.com", path, "DELETE", false},
		{"options", "chatgpt.com", path, "OPTIONS", false},
		{"lowercase method", "chatgpt.com", path, "get", false},
		{"api host", "api.openai.com", path, "GET", false},
		{"auth host", "auth.openai.com", path, "GET", false},
		{"legacy host", "chat.openai.com", path, "GET", false},
		{"subdomain", "other.chatgpt.com", path, "GET", false},
		{"unrelated host", "github.com", path, "GET", false},
		{"trailing slash", "chatgpt.com", path + "/", "GET", false},
		{"child path", "chatgpt.com", path + "/other", "GET", false},
		{"unconfirmed versioned path", "chatgpt.com", "/backend-api/accounts/check/v4-2023-04-27", "GET", false},
		{"codex api style", "chatgpt.com", "/api/codex/accounts/check", "GET", false},
		{"config bundle", "chatgpt.com", "/backend-api/wham/config/bundle", "GET", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if actual := RuntimeProviderAllowsRequest(row.host, row.path, row.method); actual != row.allowed {
				t.Fatalf("accounts check HTTP route allowed = %t, want %t", actual, row.allowed)
			}
			if RuntimeProviderAllowsWebSocket(row.host, row.path, row.method) {
				t.Fatal("accounts check route must not allow WebSocket")
			}
		})
	}
	if RuntimeWebAccessAllowsRequest(RuntimeWebAccess{Mode: RuntimeWebAccessNone}, "chatgpt.com", "GET") {
		t.Fatal("provider accounts check route must not grant user web access")
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
