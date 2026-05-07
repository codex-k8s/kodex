package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
)

func TestHealthMuxLivezReturnsNoContent(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/health/livez", nil)
	response := httptest.NewRecorder()

	healthMux(processComponents{}).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("livez status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestHealthMuxReadyzRequiresService(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/health/readyz", nil)
	response := httptest.NewRecorder()

	healthMux(processComponents{}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthMuxReadyzReturnsNoContentWhenServiceExists(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/health/readyz", nil)
	response := httptest.NewRecorder()

	healthMux(processComponents{PackageService: packageservice.New()}).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("readyz status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
