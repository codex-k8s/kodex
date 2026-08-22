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

	request.Header.Set("Origin", "https://control.example.test")
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-MatterCodex-Project-ID")
	called := false
	boundary := &Boundary{origins: map[string]struct{}{"https://control.example.test": {}}}
	response := httptest.NewRecorder()
	boundary.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
	if called || response.Code != http.StatusNoContent ||
		response.Header().Get("Access-Control-Allow-Origin") != "https://control.example.test" ||
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

	const projectID = "prj_AQIDBAUGBwgJCgsMDQ4PEBES"
	tests := []struct {
		name      string
		method    string
		path      string
		header    string
		wantBound bool
		wantErr   bool
	}{
		{name: "HTTP header", method: http.MethodGet, path: "/api/v1/runs", header: projectID, wantBound: true},
		{name: "run stream uses run eligibility", method: http.MethodGet, path: "/api/v1/runs/run_12345678/stream"},
		{name: "exact project update path", method: http.MethodPut, path: "/api/v1/projects/" + projectID, wantBound: true},
		{name: "exact project delete path", method: http.MethodDelete, path: "/api/v1/projects/" + projectID, wantBound: true},
		{name: "matching exact project header", method: http.MethodDelete, path: "/api/v1/projects/" + projectID, header: projectID, wantBound: true},
		{name: "project collection is unbound", method: http.MethodPost, path: "/api/v1/projects"},
		{name: "invalid header", method: http.MethodGet, path: "/api/v1/runs", header: "invalid", wantErr: true},
		{name: "invalid exact project path", method: http.MethodDelete, path: "/api/v1/projects/invalid", wantErr: true},
		{name: "mismatched exact project scope", method: http.MethodDelete, path: "/api/v1/projects/" + projectID, header: "prj_EhEQDw4NDAsKCQgHBgUEAwIB", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(test.method, "https://control.example.test"+test.path, nil)
			if test.header != "" {
				request.Header.Set(ProjectReferenceHeader, test.header)
			}
			base := context.Background()
			result, err := withProjectReference(base, request)
			if (err != nil) != test.wantErr {
				t.Fatalf("withProjectReference() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && (result != base) != test.wantBound {
				t.Fatalf("withProjectReference() bound = %v, want %v", result != base, test.wantBound)
			}
		})
	}
}
