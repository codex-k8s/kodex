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

func TestRuntimeProviderStandaloneSearchIsExactHTTPOnly(t *testing.T) {
	// SearchClient rust-v0.160.0 выполняет POST alpha/search относительно
	// default base URL, назначенного типом credential у ModelProviderInfo.
	for _, route := range []struct{ host, path string }{
		{"chatgpt.com", "/backend-api/codex/alpha/search"},
		{"api.openai.com", "/v1/alpha/search"},
	} {
		t.Run(route.host, func(t *testing.T) {
			for _, row := range []struct {
				name, host, path, method string
				allowed                  bool
			}{
				{"exact", route.host, route.path, "POST", true},
				{"get", route.host, route.path, "GET", false},
				{"head", route.host, route.path, "HEAD", false},
				{"put", route.host, route.path, "PUT", false},
				{"patch", route.host, route.path, "PATCH", false},
				{"delete", route.host, route.path, "DELETE", false},
				{"options", route.host, route.path, "OPTIONS", false},
				{"lowercase method", route.host, route.path, "post", false},
				{"whitespace method", route.host, route.path, " POST ", false},
				{"other host", "github.com", route.path, "POST", false},
				{"auth host", "auth.openai.com", route.path, "POST", false},
				{"subdomain", "other." + route.host, route.path, "POST", false},
				{"port in host", route.host + ":443", route.path, "POST", false},
				{"trailing slash", route.host, route.path + "/", "POST", false},
				{"child path", route.host, route.path + "/other", "POST", false},
				{"path prefix", route.host, "/other" + route.path, "POST", false},
				{"dot segments", route.host, route.path + "/../search", "POST", false},
				{"encoded path", route.host, route.path[:len(route.path)-6] + "%73earch", "POST", false},
				{"query in path", route.host, route.path + "?route=/v1/responses", "POST", false},
				{"fragment in path", route.host, route.path + "#/v1/responses", "POST", false},
				{"mock override path", route.host, "/api/codex/alpha/search", "POST", false},
			} {
				t.Run(row.name, func(t *testing.T) {
					if got := RuntimeProviderAllowsRequest(row.host, row.path, row.method); got != row.allowed {
						t.Fatalf("standalone search HTTP route allowed = %t, want %t", got, row.allowed)
					}
					if RuntimeProviderAllowsWebSocket(row.host, row.path, row.method) {
						t.Fatal("standalone search acquired WebSocket authority")
					}
				})
			}
			otherHost := "api.openai.com"
			if route.host == otherHost {
				otherHost = "chatgpt.com"
			}
			if RuntimeProviderAllowsRequest(otherHost, route.path, "POST") {
				t.Fatal("standalone search path was accepted on the other provider host")
			}
			access := RuntimeWebAccess{Mode: RuntimeWebAccessNone}
			if RuntimeWebAccessAllowsHost(access, route.host) || RuntimeWebAccessAllowsRequest(access, route.host, "POST") {
				t.Fatal("provider standalone search granted user web access")
			}
		})
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
