package httptransport

import (
	"encoding/json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderDeviceFailureIsTypedRetryableAndRedacted(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded} {
		response := httptest.NewRecorder()
		writeProviderDeviceProblem(response, status.Error(code, "private-provider-response"))
		var body map[string]any
		if json.Unmarshal(response.Body.Bytes(), &body) != nil || response.Code != 503 || body["code"] != providerDeviceAuthorizationUnavailable || body["retryable"] != true || body["correlationId"] == "" || response.Header().Get("Retry-After") != "1" || strings.Contains(response.Body.String(), "private-provider-response") {
			t.Fatal("unsafe device failure response")
		}
	}
	response := httptest.NewRecorder()
	writeProviderDeviceProblem(response, status.Error(codes.PermissionDenied, "private"))
	if response.Code != 403 || response.Header().Get("Retry-After") != "" {
		t.Fatal("authority failure became retryable")
	}
}
