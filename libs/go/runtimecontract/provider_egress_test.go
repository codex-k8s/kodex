package runtimecontract

import "testing"

// TestImplicitProviderRoutesNeverGrantAccess rejects every retired implicit route, including a real provider hostname; only configured signed routes may authorize inference.
// TestImplicitProviderRoutesNeverGrantAccess отклоняет все retired implicit routes, включая настоящий provider hostname; inference разрешают лишь configured signed routes.
func TestImplicitProviderRoutesNeverGrantAccess(t *testing.T) {
	for _, host := range []string{"api.openai.com", "chatgpt.com", "auth.openai.com", "gateway.example.invalid", ""} {
		for _, path := range []string{"/v1/responses", "/v1/models", "/backend-api/codex/responses", "/backend-api/wham/accounts/check", "/backend-api/wham/usage", "/oauth/token", "/v1/alpha/search"} {
			for _, method := range []string{"POST", "GET", "HEAD", "DELETE"} {
				if RuntimeProviderAllowsHost(host) || RuntimeProviderAllowsRequest(host, path, method) || RuntimeProviderAllowsWebSocket(host, path, method) {
					t.Fatal("unconfigured model route acquired authority")
				}
			}
		}
	}
}

// TestRuntimeEnvironmentRejectsTrustAndVerificationOverrides prevents user variables from replacing CA trust, disabling TLS checks or injecting a bypass proxy.
// TestRuntimeEnvironmentRejectsTrustAndVerificationOverrides запрещает пользовательским переменным заменять доверие CA, отключать TLS-проверки или внедрять обходной прокси.
func TestRuntimeEnvironmentRejectsTrustAndVerificationOverrides(t *testing.T) {
	for _, name := range []string{"CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO",
		"NODE_TLS_REJECT_UNAUTHORIZED", "GIT_SSL_NO_VERIFY", "PYTHONHTTPSVERIFY", "CURL_SSL_BACKEND", "ALL_PROXY"} {
		if ValidRuntimeEnvironmentName(name) {
			t.Fatalf("reserved transport environment name accepted: %s", name)
		}
	}
}
