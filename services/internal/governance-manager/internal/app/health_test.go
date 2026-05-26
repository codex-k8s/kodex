package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceprocess"
)

func TestReadinessChecksHealthyService(t *testing.T) {
	t.Parallel()

	mux := serviceprocess.NewHealthMux(readinessChecks(fakeReadyService(true), fakePingStore{}, nil), time.Second)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/readyz", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestReadinessChecksMissingService(t *testing.T) {
	t.Parallel()

	mux := serviceprocess.NewHealthMux(readinessChecks(nil, fakePingStore{}, nil), time.Second)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

type fakeReadyService bool

func (service fakeReadyService) Ready() bool {
	return bool(service)
}

type fakePingStore struct{}

func (fakePingStore) Ping(context.Context) error {
	return nil
}
