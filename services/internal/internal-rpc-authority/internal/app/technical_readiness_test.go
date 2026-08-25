package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
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

type restoreStartupReadinessStub struct{ err error }

func (stub restoreStartupReadinessStub) StartupReady(context.Context) (model.RestoreState, error) {
	return model.RestoreState{}, stub.err
}

func TestRestoreReadinessCachesOnlyDirectInfrastructureProbe(t *testing.T) {
	t.Parallel()

	readiness := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("restore_readiness_test", "test", nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	refreshRestoreReadiness(context.Background(), restoreStartupReadinessStub{err: errors.New("database unavailable")}, readiness, metrics, logger)
	if ready, reason := readiness.Ready(); ready || reason != "direct-infrastructure-unavailable" {
		t.Fatalf("readiness = %t/%s", ready, reason)
	}
	refreshRestoreReadiness(context.Background(), restoreStartupReadinessStub{}, readiness, metrics, logger)
	if ready, reason := readiness.Ready(); !ready || reason != "ready" {
		t.Fatalf("recovered readiness = %t/%s", ready, reason)
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

func TestAuthorityReadinessAggregatesIndependentLocalConditions(t *testing.T) {
	t.Parallel()

	snapshot := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("authority_readiness_aggregate_test", "test", nil)
	state := newAuthorityReadiness(snapshot, metrics)
	assertReadiness := func(expected bool, reason string) {
		t.Helper()
		actual, actualReason := snapshot.Ready()
		if actual != expected || actualReason != reason {
			t.Fatalf("readiness = %t/%s, want %t/%s", actual, actualReason, expected, reason)
		}
	}

	assertReadiness(true, "ready")
	if !state.Set(conditionReplay, false) || state.Set(conditionReplay, false) {
		t.Fatal("replay condition edge was not reported exactly once")
	}
	assertReadiness(false, "replay_cleanup_unavailable")
	if !state.Set(conditionSnapshot, false) {
		t.Fatal("snapshot condition edge was not reported")
	}
	assertReadiness(false, "snapshot_unavailable")
	state.Set(conditionReplay, true)
	assertReadiness(false, "snapshot_unavailable")
	state.Set(conditionSnapshot, true)
	assertReadiness(true, "ready")
}
