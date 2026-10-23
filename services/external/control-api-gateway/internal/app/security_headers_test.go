package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecureHeadersAllowMicrophoneOnlyForSameOrigin checks the exact permissions policy and presence of selected API security headers using an in-memory recorder, not browser microphone permissions or a live TLS endpoint.
// TestSecureHeadersAllowMicrophoneOnlyForSameOrigin через recorder в памяти проверяет точную permissions policy и наличие выбранных API security headers, а не browser microphone permissions и живой TLS endpoint.
func TestSecureHeadersAllowMicrophoneOnlyForSameOrigin(t *testing.T) {
	handler := secureHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil))

	if value := response.Header().Get("Permissions-Policy"); value != "camera=(), microphone=(self), geolocation=()" {
		t.Fatalf("Permissions-Policy = %q", value)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("security headers are incomplete: %v", response.Header())
	}
}
