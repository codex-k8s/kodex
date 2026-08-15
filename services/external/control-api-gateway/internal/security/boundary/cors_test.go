package boundary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestCredentialedPreflightAllowsExactOwnerHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "https://control-api.mattercodex.local/api/v1/session", nil)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-MatterCodex-Project-ID")
	if !allowedPreflight(request) {
		t.Fatal("exact credentialed owner preflight was rejected")
	}
	request.Header.Set("Access-Control-Request-Headers", "Authorization, X-Caller-Owner")
	if allowedPreflight(request) {
		t.Fatal("caller-supplied authority header was accepted")
	}

	request.Header.Set("Origin", "https://control.kodex.works")
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-MatterCodex-Project-ID")
	called := false
	boundary := &Boundary{origins: map[string]struct{}{"https://control.kodex.works": {}}}
	response := httptest.NewRecorder()
	boundary.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
	if called || response.Code != http.StatusNoContent ||
		response.Header().Get("Access-Control-Allow-Origin") != "https://control.kodex.works" ||
		response.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		response.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-MatterCodex-Project-ID" {
		t.Fatalf("credentialed preflight response is incomplete: status=%d headers=%v", response.Code, response.Header())
	}
	vary := make([]string, 0)
	for _, header := range response.Header().Values("Vary") {
		for _, value := range strings.Split(header, ",") {
			vary = append(vary, strings.TrimSpace(value))
		}
	}
	for _, required := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !slices.Contains(vary, required) {
			t.Fatalf("preflight Vary misses %s: %v", required, vary)
		}
	}
}

func TestProjectReferenceBinding(t *testing.T) {
	t.Parallel()

	const projectID = "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d"
	tests := []struct {
		name    string
		path    string
		header  string
		wantErr bool
	}{
		{name: "HTTP header", path: "/api/v1/runs", header: projectID},
		{name: "realtime query", path: "/api/v1/realtime?projectId=" + projectID},
		{name: "invalid header", path: "/api/v1/runs", header: "invalid", wantErr: true},
		{name: "mismatched realtime scope", path: "/api/v1/realtime?projectId=" + projectID, header: "bcda470d-95dd-4839-bd59-55e1032d61f7", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "https://control.kodex.works"+test.path, nil)
			if test.header != "" {
				request.Header.Set(ProjectReferenceHeader, test.header)
			}
			_, err := withProjectReference(context.Background(), request)
			if (err != nil) != test.wantErr {
				t.Fatalf("withProjectReference() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
