package serviceprocess

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ReadinessCheck describes one dependency that must be healthy before traffic is accepted.
type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

// NewHealthMux returns the standard live/readiness/metrics HTTP boundary.
func NewHealthMux(checks []ReadinessCheck, timeout time.Duration) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		for _, check := range checks {
			if check.Check == nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if err := check.Check(readyCtx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
