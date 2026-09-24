package integration

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

func openAPITestCapability() integrationpackage.Capability {
	return integrationpackage.Capability{
		Operation: "openapi.ticket.update", Risk: "WRITE", ApprovalPolicy: "HUMAN_SCOPED",
		Execution: integrationpackage.Execution{Idempotency: "EFFECT_KEY", TimeoutSeconds: 10, MaxAttempts: 1, RetryBackoffMilliseconds: 250},
		OpenAPI: &integrationpackage.OpenAPIHTTP{
			OperationID: "updateTicket", Method: "PATCH", Path: "/tickets/{id}",
			ServerOrigin: "https://api.example.test",
			AuthScheme:   "API_KEY_HEADER", AuthHeader: "X-Api-Key", IdempotencyHeader: "X-Request-Key",
		},
	}
}

func TestOpenAPIExecutionRejectsChangedOriginBeforeCredentialReadOrNetwork(t *testing.T) {
	adapter := testAdapter(t)
	capability := openAPITestCapability()
	requests := 0
	adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected outbound request")
	})}
	for _, origin := range []string{"https://other.example.test", "https://api.example.test/", ""} {
		_, err := adapter.executeOpenAPI(t.Context(), Request{}, capability,
			map[string]string{"base_url": origin}, []byte(`{"path":{"id":3}}`))
		var safe *SafeError
		if !errors.As(err, &safe) || safe.Code != "INTEGRATION_CONFIGURATION_INVALID" || requests != 0 {
			t.Fatalf("changed origin was accepted: code=%v requests=%d", err, requests)
		}
	}
}

func TestOpenAPIExecutionPinsEndpointHeadersAndOneEffect(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "fixture-key")
	capability := openAPITestCapability()
	request := Request{EffectKey: "eff_exact", InputDigest: strings.Repeat("a", 64), Credential: credential}
	requests := 0
	adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(outbound *http.Request) (*http.Response, error) {
		requests++
		if outbound.Method != "PATCH" || outbound.URL.String() != "https://api.example.test/tickets/3?view=full" ||
			outbound.Header.Get("X-Api-Key") != "fixture-key" || outbound.Header.Get("X-Request-Key") != "eff_exact" ||
			outbound.Header.Get("Idempotency-Key") != "" || outbound.Header.Get("Authorization") != "" || outbound.GetBody != nil {
			t.Fatal("OpenAPI request escaped its published binding")
		}
		body, err := io.ReadAll(outbound.Body)
		if err != nil || string(body) != `{"title":"updated"}` {
			t.Fatal("OpenAPI request body changed")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	result, err := adapter.executeOpenAPI(t.Context(), request, capability, map[string]string{"base_url": "https://api.example.test"},
		[]byte(`{"path":{"id":3},"query":{"view":"full"},"body":{"title":"updated"}}`))
	if err != nil || requests != 1 || result.Receipt.EffectKey != "eff_exact" || !strings.Contains(result.Summary, `"body_json":"{\"ok\":true}"`) {
		t.Fatalf("OpenAPI execution failed: %v, requests=%d", err, requests)
	}
}

func TestOpenAPIRejectsPathEscapeBeforeNetwork(t *testing.T) {
	adapter := testAdapter(t)
	capability := openAPITestCapability()
	for _, input := range []string{
		`{"path":{"id":"../admin"}}`,
		`{"path":{"id":"other/id"}}`,
		`{"path":{"wrong":3}}`,
		`{"query":{"view":{"nested":true}},"path":{"id":3}}`,
	} {
		_, err := adapter.executeOpenAPI(t.Context(), Request{}, capability,
			map[string]string{"base_url": "https://api.example.test"}, []byte(input))
		if err == nil {
			t.Fatalf("unsafe OpenAPI input reached network: %s", input)
		}
	}
}

func TestOpenAPIWriteDoesNotRetryUnknownOutcome(t *testing.T) {
	adapter := testAdapter(t)
	capability := openAPITestCapability()
	capability.OpenAPI.AuthScheme, capability.OpenAPI.AuthHeader, capability.OpenAPI.IdempotencyHeader = "NONE", "", ""
	request := Request{EffectKey: "eff_once", InputDigest: strings.Repeat("b", 64)}
	requests := 0
	adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("synthetic connection failure")
	})}
	_, err := adapter.executeOpenAPI(t.Context(), request, capability,
		map[string]string{"base_url": "https://api.example.test"}, []byte(`{"path":{"id":3}}`))
	if !IsUnknownOutcome(err) || requests != 1 {
		t.Fatalf("ambiguous write was retried: err=%v requests=%d", err, requests)
	}
}
