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

// TestTechnicalReadinessUsesOnlyCachedSnapshot checks authority and the three snapshot-profile consumers against the same in-memory readiness transitions. No application instance is available to the snapshot factory; the test proves handler behavior, not a live dependency probe or listener.
// TestTechnicalReadinessUsesOnlyCachedSnapshot проверяет authority и три потребителя профиля снимка при одинаковых переходах готовности в памяти. Фабрика снимка не получает экземпляр приложения; тест доказывает поведение обработчика, а не живую проверку зависимости или listener.
func TestTechnicalReadinessUsesOnlyCachedSnapshot(t *testing.T) {
	t.Parallel()

	readiness := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("authority_readiness_test", "test", nil)
	servers := map[string]*http.Server{
		"authority": newTechnicalServer(Config{}, readiness, metrics, nil),
		"publisher": newSnapshotTechnicalServer(readiness, metrics),
		"readback":  newSnapshotTechnicalServer(readiness, metrics),
		"restore":   newSnapshotTechnicalServer(readiness, metrics),
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

// restoreStartupReadinessStub returns a chosen synthetic infrastructure error and an empty restore state without external I/O.
// restoreStartupReadinessStub возвращает выбранную синтетическую ошибку инфраструктуры и пустое состояние восстановления без внешнего I/O.
type restoreStartupReadinessStub struct{ err error }

// StartupReady ignores the test context and returns the configured result, allowing the readiness cache to be exercised without PostgreSQL.
// StartupReady игнорирует тестовый контекст и возвращает настроенный результат для проверки кэша готовности без PostgreSQL.
func (stub restoreStartupReadinessStub) StartupReady(context.Context) (model.RestoreState, error) {
	return model.RestoreState{}, stub.err
}

// TestRestoreReadinessCachesOnlyDirectInfrastructureProbe checks that a failed synthetic startup probe publishes the fixed unavailable reason and that the next successful probe restores ready. Logging is discarded and no database is contacted.
// TestRestoreReadinessCachesOnlyDirectInfrastructureProbe проверяет публикацию фиксированной причины недоступности при неуспешной синтетической стартовой пробе и восстановление готовности при следующей успешной. Логи отбрасываются, обращений к БД нет.
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

// TestTechnicalHealthDoesNotDependOnReadiness checks that authority health returns 204 while its local snapshot is initially not ready; this is not proof of dependency readiness.
// TestTechnicalHealthDoesNotDependOnReadiness проверяет ответ 204 от health authority при исходно неготовом локальном снимке; готовность зависимостей этим не доказывается.
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

// TestAuthorityReadinessAggregatesIndependentLocalConditions checks conjunction and priority of snapshot and replay conditions, single-edge reporting and recovery. One successful condition must not hide another failing condition; only in-memory state is exercised.
// TestAuthorityReadinessAggregatesIndependentLocalConditions проверяет конъюнкцию и приоритет условий снимка и replay, однократное сообщение перехода и восстановление. Успех одного условия не должен скрывать сбой другого; проверяется только состояние в памяти.
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
