package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPHandlerHealthReadinessAndMetrics(t *testing.T) {
	handler := newHTTPHandler(nil, time.Second)

	for _, path := range []string{"/health/livez", "/health/readyz", "/metrics"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s status = %d, want service endpoint", path, rec.Code)
		}
	}
}
