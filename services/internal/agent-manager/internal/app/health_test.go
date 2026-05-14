package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
)

func TestHealthMuxLivezReturnsNoContent(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/health/livez", nil)
	response := httptest.NewRecorder()

	serviceprocess.NewHealthMux(readinessChecks(nil, nil, nil), 2*time.Second).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("livez status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestHealthMuxReadyzRequiresService(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/health/readyz", nil)
	response := httptest.NewRecorder()

	serviceprocess.NewHealthMux(readinessChecks(nil, nil, nil), 2*time.Second).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthMuxReadyzReturnsNoContentWhenServiceExists(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/health/readyz", nil)
	response := httptest.NewRecorder()
	agentService := agentservice.New(agentservice.Config{EventPublisher: agentservice.DisabledEventPublisher{}})

	serviceprocess.NewHealthMux(readinessChecks(agentService, pingStore{}, nil), 2*time.Second).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("readyz status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

type pingStore struct{}

func (pingStore) Ping(context.Context) error {
	return nil
}
