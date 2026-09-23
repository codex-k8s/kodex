package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPSJSONReadUsesOnlyPinnedEndpointAndCredential(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "fixture-token")
	request := invocationRequest(t, adapter.definitions["https-json"], "https_json.resource.read", map[string]any{}, credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(outbound *http.Request) (*http.Response, error) {
		calls++
		if outbound.Method != http.MethodGet || outbound.URL.String() != "https://api.example.test/v1/status" ||
			outbound.Header.Get("Authorization") != "Bearer fixture-token" || outbound.Header.Get("Accept") != "application/json" {
			t.Fatal("HTTPS JSON read escaped pinned endpoint or credential binding")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ready","large_id":9223372036854775807}`))}, nil
	})}
	result, err := adapter.Execute(t.Context(), request)
	if err != nil || calls != 1 || !strings.Contains(result.Summary, `9223372036854775807`) || !strings.HasPrefix(result.Receipt.ProviderEffectRef, "https-json:") {
		t.Fatalf("fixed JSON read failed: err=%v calls=%d", err, calls)
	}
	if _, err := adapter.Test(t.Context(), request); err != nil || calls != 2 {
		t.Fatalf("connection test missed exact read path: err=%v calls=%d", err, calls)
	}
	request.Configuration["resource_path"] = "/v1/other"
	if _, err := adapter.Execute(t.Context(), request); err == nil || calls != 2 {
		t.Fatal("changed path bypassed owner resource scope")
	}
	request = invocationRequest(t, adapter.definitions["https-json"], "https_json.resource.read", map[string]any{}, nil)
	if _, err := adapter.Execute(t.Context(), request); err == nil || calls != 2 {
		t.Fatal("missing credential reached provider")
	}
}

func TestHTTPSJSONReadRejectsUnboundedOrInvalidResponse(t *testing.T) {
	for _, body := range []string{`null`, `1`, `"text"`, `{"ok":true} {"extra":true}`, `{"data":"` + strings.Repeat("x", maximumHTTPSJSONBodyBytes) + `"}`} {
		t.Run(body[:min(len(body), 16)], func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "fixture-token")
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			request := invocationRequest(t, adapter.definitions["https-json"], "https_json.resource.read", map[string]any{}, credential)
			if _, err := adapter.Execute(t.Context(), request); err == nil {
				t.Fatal("invalid JSON response accepted")
			}
		})
	}
}
