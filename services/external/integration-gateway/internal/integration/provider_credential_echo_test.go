package integration

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestProviderCredentialEchoBoundary prevents raw, JSON-escaped and Basic-encoded authentication echoes from reaching any typed provider result, preserving UNKNOWN after a potentially accepted mutation.
// TestProviderCredentialEchoBoundary запрещает raw, JSON-escaped и Basic-encoded authentication echo в typed provider result, сохраняя UNKNOWN после потенциально принятого изменения.
func TestProviderCredentialEchoBoundary(t *testing.T) {
	for _, scenario := range []string{"raw", "escaped", "basic", "unrelated", "auth-denial"} {
		t.Run(scenario, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "synthetic-echo-token")
				request := invocationRequest(t, adapter.definitions["jira"], "jira.project.read", map[string]any{}, credential)
				capability, _ := adapter.definitions["jira"].Capability(request.CapabilityKey)
				calls := 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
					calls++
					body := `{"name":"synthetic-echo-token"}`
					status := 200
					if scenario == "escaped" {
						body = `{"unused":"synthetic-echo-\u0074oken"}`
					}
					if scenario == "basic" {
						body = `{"authorization":"` + base64.StdEncoding.EncodeToString([]byte("synthetic-user:synthetic-echo-token")) + `"}`
					}
					if scenario == "unrelated" {
						body = `{"name":"Synthetic safe data"}`
					}
					if scenario == "auth-denial" {
						status = 403
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: wire}, nil
				})}
				scheme, username := "BEARER", ""
				if scenario == "basic" {
					scheme, username = "BASIC", "synthetic-user"
				}
				_, err := adapter.callProvider(t.Context(), providerCall{BaseURL: "https://provider.example.invalid", Path: "/fixed", Method: method, AuthScheme: scheme, Username: username, Credential: credential, Capability: capability})
				if scenario == "unrelated" {
					if err != nil {
						t.Fatal("safe provider data refused", err)
					}
					continue
				}
				if scenario == "auth-denial" {
					if ObservedProviderHTTPStatus(err) != 403 || IsUnknownOutcome(err) {
						t.Fatal("auth classification changed")
					}
					continue
				}
				if err == nil || IsUnknownOutcome(err) != (method == http.MethodPost) || calls != 1 {
					t.Fatalf("authentication echo escaped the boundary: err=%v calls=%d", err, calls)
				}
			}
		})
	}
}
