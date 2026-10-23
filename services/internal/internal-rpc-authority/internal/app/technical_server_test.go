package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// TestSnapshotTechnicalServerKeepsConstructionProfile checks the original four timeout values, unset address and TLS/lifecycle hooks, and fresh server/mux identity on each call. It constructs servers without opening a listener or claiming that timeouts have been exercised on a real connection.
// TestSnapshotTechnicalServerKeepsConstructionProfile проверяет четыре исходных таймаута, незаданные адрес и TLS/lifecycle hooks и новые сервер и mux при каждом вызове. Серверы создаются без listener; срабатывание таймаутов на реальном соединении не заявляется.
func TestSnapshotTechnicalServerKeepsConstructionProfile(t *testing.T) {
	t.Parallel()
	readiness := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("snapshot_construction_test", "test", nil)
	first := newSnapshotTechnicalServer(readiness, metrics)
	second := newSnapshotTechnicalServer(readiness, metrics)
	for _, server := range []*http.Server{first, second} {
		if server.ReadHeaderTimeout != 2*time.Second || server.ReadTimeout != 5*time.Second ||
			server.WriteTimeout != 5*time.Second || server.IdleTimeout != 30*time.Second ||
			server.Addr != "" || server.MaxHeaderBytes != 0 || server.TLSConfig != nil ||
			server.BaseContext != nil || server.ConnContext != nil || server.ErrorLog != nil {
			t.Fatal("technical server construction profile changed")
		}
		if _, ok := server.Handler.(*http.ServeMux); !ok {
			t.Fatal("technical server no longer owns its own mux")
		}
	}
	if first == second || first.Handler == second.Handler {
		t.Fatal("technical server construction reused mutable server state")
	}
}

// TestSnapshotTechnicalServerKeepsProbeResponses checks fifty-four direct handler requests across false/true/false cached states, six paths and GET/POST/HEAD, plus a metrics read in each state. Status, body, content type and private-reason suppression stay fixed; HEAD uses ResponseRecorder, not transport-level body suppression. No application, network listener or dependency probe runs.
// TestSnapshotTechnicalServerKeepsProbeResponses проверяет пятьдесят четыре прямых вызова обработчика для сохранённых состояний false/true/false, шести путей и GET/POST/HEAD, а также чтение метрик в каждом состоянии. Сохраняются статус, тело, тип содержимого и скрытие закрытой причины; HEAD использует ResponseRecorder, а не транспортное подавление тела. Приложение, сетевой listener и проверка зависимостей не запускаются.
func TestSnapshotTechnicalServerKeepsProbeResponses(t *testing.T) {
	t.Parallel()
	readiness := serviceruntime.NewReadiness()
	metrics := observability.NewMetrics("snapshot_responses_test", "test", nil)
	server := newSnapshotTechnicalServer(readiness, metrics)
	for state, ready := range []bool{false, true, false} {
		readiness.Set(ready, "PRIVATE_READINESS_REASON")
		for _, test := range []struct {
			path string
			code int
			body string
		}{
			{path: "/livez", code: http.StatusOK, body: "ok\n"},
			{path: "/healthz", code: http.StatusNoContent},
			{path: "/readyz", code: http.StatusOK, body: "ready\n"},
			{path: "/readyz?probe=1", code: http.StatusOK, body: "ready\n"},
			{path: "/readyz/child", code: http.StatusNotFound, body: "404 page not found\n"},
			{path: "/unknown", code: http.StatusNotFound, body: "404 page not found\n"},
		} {
			for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
				t.Run(fmt.Sprintf("%d/%s/%s", state, method, test.path), func(t *testing.T) {
					code, body := test.code, test.body
					if !ready && strings.HasPrefix(test.path, "/readyz") && code == http.StatusOK {
						code, body = http.StatusServiceUnavailable, "not ready\n"
					}
					response := httptest.NewRecorder()
					server.Handler.ServeHTTP(response, httptest.NewRequest(method, test.path, nil))
					contentType := "text/plain; charset=utf-8"
					if code == http.StatusNoContent {
						contentType = ""
					}
					if response.Code != code || response.Body.String() != body || response.Result().Header.Get("Content-Type") != contentType ||
						strings.Contains(response.Body.String(), "PRIVATE_READINESS_REASON") || response.Header().Get("Allow") != "" {
						t.Fatal("technical probe response profile changed")
					}
				})
			}
		}
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "snapshot_responses_test") {
			t.Fatal("technical metrics route is not the supplied registry")
		}
	}
}
