package app

import (
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// newSnapshotTechnicalServer composes the identical text-probe HTTP profile used by publisher, readback and restore. Readiness reads only the shared cached snapshot and never calls the application; listening, serving and shutdown remain caller-owned. Routes retain their existing all-method matching and responses, unlike the distinct authority profile and the bounded GET-only httpserver.New profile.
// newSnapshotTechnicalServer собирает одинаковый HTTP-профиль текстовых проб publisher, readback и restore. Готовность читает только общий сохранённый снимок и не вызывает приложение; создание listener, обслуживание и завершение остаются у вызывающего. Маршруты сохраняют прежнее сопоставление всех методов и ответы, отличаясь от отдельного профиля authority и ограниченного GET-only профиля httpserver.New.
func newSnapshotTechnicalServer(readiness *serviceruntime.Readiness, metrics *observability.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.PrometheusHandler())
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		if ready, _ := readiness.Ready(); !ready {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte("ready\n"))
	})
	return &http.Server{
		Handler: mux, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second,
	}
}
