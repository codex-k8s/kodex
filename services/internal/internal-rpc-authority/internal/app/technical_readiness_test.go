package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
)

func TestTechnicalReadinessUsesOnlyCachedSnapshot(t *testing.T) {
	t.Parallel()

	readiness := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("authority_readiness_test", "test", nil)
	servers := map[string]*http.Server{
		"authority":  newTechnicalServer(Config{}, readiness, metrics, nil),
		"credential": newCredentialTechnicalServer(readiness, metrics, nil),
		"publisher":  newPublisherTechnicalServer(readiness, metrics, nil),
		"readback":   newReadbackTechnicalServer(readiness, metrics, nil),
		"restore":    newRestoreTechnicalServer(readiness, metrics, nil),
	}

	for name, server := range servers {
		server := server
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			unready := httptest.NewRecorder()
			server.Handler.ServeHTTP(unready, request)
			if unready.Code != http.StatusServiceUnavailable {
				t.Fatalf("неожиданный unready status: %d", unready.Code)
			}

			readiness.Set(true, "ready")
			ready := httptest.NewRecorder()
			server.Handler.ServeHTTP(ready, request)
			if ready.Code != http.StatusOK {
				t.Fatalf("неожиданный ready status: %d", ready.Code)
			}
			readiness.Set(false, "next_case")
		})
	}
}

func TestTechnicalHealthDoesNotDependOnReadiness(t *testing.T) {
	t.Parallel()

	readiness := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("authority_health_test", "test", nil)
	server := newTechnicalServer(Config{}, readiness, metrics, nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("healthz зависит от readiness: %d", response.Code)
	}
}
