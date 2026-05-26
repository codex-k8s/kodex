package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	serviceprocess "github.com/codex-k8s/kodex/libs/go/serviceprocess"
	hookservice "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/service"
	hookstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/hook"
	commandtransport "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/transport/command"
)

func TestReadinessReportsReadyWhenSkeletonIsComposed(t *testing.T) {
	t.Parallel()

	service := hookservice.New(hookstub.NewRepository(), hookservice.Config{}, hookservice.Dependencies{})
	handler := commandtransport.NewHandler(service)
	request := httptest.NewRequest(http.MethodGet, "/health/readyz", nil)
	response := httptest.NewRecorder()

	serviceprocess.NewHealthMux(readinessChecks(service, handler), time.Second).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("readyz status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestReadinessRejectsMissingLogicalHandler(t *testing.T) {
	t.Parallel()

	service := hookservice.New(hookstub.NewRepository(), hookservice.Config{}, hookservice.Dependencies{})
	request := httptest.NewRequest(http.MethodGet, "/health/readyz", nil)
	response := httptest.NewRecorder()

	serviceprocess.NewHealthMux(readinessChecks(service, nil), time.Second).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
