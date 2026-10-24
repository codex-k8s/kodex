package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProbeMuxPreservesSnapshotResponses checks original probe methods/status/body/headers and
// verifies only readyz reads the current snapshot, including changes after mux construction.
// TestProbeMuxPreservesSnapshotResponses проверяет прежние методы, status, body и headers probes
// и чтение текущего snapshot только readyz, включая изменения после создания mux.
func TestProbeMuxPreservesSnapshotResponses(t *testing.T) {
	t.Parallel()
	ready, calls := false, 0
	mux := ProbeMux(func() (bool, string) { calls++; return ready, "dependencies_starting" })
	if calls != 0 {
		t.Fatal("constructing probe mux evaluated readiness")
	}
	for _, state := range []bool{false, true} {
		ready = state
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			for _, target := range []string{"/healthz", "/livez", "/startupz", "/readyz", "/readyz?probe=1"} {
				previous := calls
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, httptest.NewRequest(method, target, nil))
				isReady := target == "/readyz" || target == "/readyz?probe=1"
				wantStatus, wantBody, wantCalls := http.StatusNoContent, "", previous
				if isReady {
					wantCalls++
					if !state {
						wantStatus, wantBody = http.StatusServiceUnavailable, "dependencies_starting\n"
					}
				}
				if response.Code != wantStatus || response.Body.String() != wantBody || calls != wantCalls {
					t.Fatal("probe snapshot, method or response contract changed")
				}
				if response.Header().Get("Cache-Control") != "" {
					t.Fatal("probe acquired an unrequested response header")
				}
				contentType, sniffing := "", ""
				if isReady && !state {
					contentType, sniffing = "text/plain; charset=utf-8", "nosniff"
				}
				if response.Header().Get("Content-Type") != contentType || response.Header().Get("X-Content-Type-Options") != sniffing {
					t.Fatal("probe error headers changed")
				}
			}
		}
	}
}

// TestProbeMuxLeavesExtraRoutesToCaller checks the factory does not install metrics or a root
// fallback, and a caller can add its own route without evaluating readiness.
// TestProbeMuxLeavesExtraRoutesToCaller проверяет отсутствие metrics и root fallback у фабрики
// и добавление caller собственного route без вычисления readiness.
func TestProbeMuxLeavesExtraRoutesToCaller(t *testing.T) {
	t.Parallel()
	calls := 0
	mux := ProbeMux(func() (bool, string) { calls++; return true, "ready" })
	for _, path := range []string{"/", "/metrics", "/readyz/extra"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound || response.Body.String() != "404 page not found\n" {
			t.Fatal("probe mux changed unknown-route behavior")
		}
	}
	mux.HandleFunc("/metrics", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusAccepted) })
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if response.Code != http.StatusAccepted || calls != 0 {
		t.Fatal("caller route changed or consulted readiness")
	}
}
