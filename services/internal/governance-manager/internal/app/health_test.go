package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceprocess"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	governancestub "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/repository/stub/governance"
)

func TestReadinessChecksHealthyService(t *testing.T) {
	t.Parallel()

	service := governanceservice.New(governancestub.NewRepository())
	mux := serviceprocess.NewHealthMux(readinessChecks(service), time.Second)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/readyz", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestReadinessChecksMissingService(t *testing.T) {
	t.Parallel()

	mux := serviceprocess.NewHealthMux(readinessChecks(nil), time.Second)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
