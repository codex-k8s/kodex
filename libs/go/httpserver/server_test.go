package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type readyStub struct{}

func (readyStub) Ready() (bool, string) { return true, "ready" }

func TestExactGETRouteRejectsQueryBodyAndUnsafeRegistration(t *testing.T) {
	server, err := New(testConfig(), readyStub{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ExactGETRoute{
		Path: "/policy", ContentType: "application/json", Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method string
		target string
		body   bool
		status int
	}{
		{http.MethodGet, "/policy", false, http.StatusNoContent},
		{http.MethodGet, "/policy?host=example.com", false, http.StatusBadRequest},
		{http.MethodGet, "/policy", true, http.StatusBadRequest},
		{http.MethodPost, "/policy", false, http.StatusMethodNotAllowed},
	} {
		request := httptest.NewRequest(test.method, test.target, nil)
		if test.body {
			request.ContentLength = 1
		}
		response := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s: got %d, want %d", test.method, test.target, response.Code, test.status)
		}
	}
	if _, err := New(testConfig(), readyStub{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ExactGETRoute{
		Path: "/readyz", ContentType: "text/plain", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}); err == nil {
		t.Fatal("expected standard route collision to fail")
	}
}

func testConfig() Config {
	return Config{
		Address: "127.0.0.1:0", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaximumHeaderBytes: 16 << 10,
		MaximumConnections: 16,
	}
}
