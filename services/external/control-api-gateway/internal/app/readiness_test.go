package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// readinessFixture independently controls infrastructure and initial key state
// while exposing only the two read-only interfaces used by the real worker.
// readinessFixture независимо задаёт инфраструктуру и начальное состояние ключей
// через два интерфейса чтения, используемых настоящим worker.
type readinessFixture struct {
	initialized atomic.Bool
	failed      atomic.Bool
}

// Check returns the closed infrastructure refusal without network activity.
// Check возвращает закрытый отказ инфраструктуры без обращения к сети.
func (fixture *readinessFixture) Check(context.Context) error {
	if fixture.failed.Load() {
		return errors.New("test infrastructure unavailable")
	}
	return nil
}

// SigningKeysInitialized reads the fixture milestone without refreshing keys.
// SigningKeysInitialized читает состояние fixture без обновления ключей.
func (fixture *readinessFixture) SigningKeysInitialized() bool { return fixture.initialized.Load() }

// TestReadinessWorkerWaitsForFirstSigningKeys exercises the actual worker and
// probe mux through cold start, recovery, infrastructure outage and shutdown.
// Liveness/startup stay healthy while only serving readiness is withheld.
// TestReadinessWorkerWaitsForFirstSigningKeys проверяет настоящий worker и probes
// при холодном старте, восстановлении, отказе инфраструктуры и завершении.
// Liveness/startup остаются успешными, ограничивается только готовность к трафику.
func TestReadinessWorkerWaitsForFirstSigningKeys(t *testing.T) {
	t.Parallel()
	fixture := &readinessFixture{}
	state := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics("control-api-readiness-test", "test", map[string]string{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- readinessWorker(fixture, fixture, fixture, fixture, state, metrics, logger, Config{ReadinessInterval: time.Millisecond, RPCTimeout: time.Second})(ctx)
	}()
	mux := httpserver.ProbeMux(state.Ready)
	await := func(ready bool, reason string) {
		t.Helper()
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			got, why := state.Ready()
			if got == ready && why == reason {
				return
			}
			select {
			case <-deadline.C:
				t.Fatalf("readiness = %v/%s, want %v/%s", got, why, ready, reason)
			case <-tick.C:
			}
		}
	}
	probe := func(path string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Fatalf("%s status = %d, want %d", path, response.Code, want)
		}
	}
	await(false, initialSigningKeysReason)
	probe("/readyz", http.StatusServiceUnavailable)
	probe("/livez", http.StatusNoContent)
	probe("/startupz", http.StatusNoContent)
	fixture.initialized.Store(true)
	await(true, "ready")
	probe("/readyz", http.StatusNoContent)
	fixture.failed.Store(true)
	await(false, "direct_infrastructure_unavailable")
	probe("/readyz", http.StatusServiceUnavailable)
	fixture.failed.Store(false)
	await(true, "ready")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("worker shutdown = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readiness worker did not stop")
	}
	await(false, "shutting_down")
	probe("/readyz", http.StatusServiceUnavailable)
}
