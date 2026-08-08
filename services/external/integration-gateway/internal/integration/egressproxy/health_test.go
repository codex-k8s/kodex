package egressproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckUsesCompatibilityReadinessOnPlatformPort(t *testing.T) {
	t.Parallel()

	var path, query string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		path, query = request.URL.Path, request.URL.RawQuery
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := Check(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if path != "/readyz" || query != "" {
		t.Fatalf("unexpected platform readiness mapping: path=%q query=%q", path, query)
	}
}

func TestCheckRejectsNonReadyPlatformResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := Check(context.Background(), server.URL); err == nil {
		t.Fatal("non-ready platform egress response was accepted")
	}
}
