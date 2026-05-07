package serviceprocess

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ReadinessCheck describes one dependency that must be healthy before traffic is accepted.
type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

// PingStore describes a dependency that can verify its backing connection.
type PingStore interface {
	Ping(context.Context) error
}

// StaticReadinessCheck reports a startup invariant such as a composed service dependency.
func StaticReadinessCheck(name string, ready bool) ReadinessCheck {
	return ReadinessCheck{
		Name: name,
		Check: func(context.Context) error {
			if !ready {
				return fmt.Errorf("%s is not ready", name)
			}
			return nil
		},
	}
}

// ServiceDatabaseReadinessChecks returns the standard service + database readiness set.
func ServiceDatabaseReadinessChecks(
	serviceCheckName string,
	serviceReady bool,
	databaseCheckName string,
	database PingStore,
	eventLogDatabase PingStore,
) []ReadinessCheck {
	checks := []ReadinessCheck{
		StaticReadinessCheck(serviceCheckName, serviceReady),
		pingReadinessCheck(databaseCheckName, database),
	}
	if eventLogDatabase != nil {
		checks = append(checks, pingReadinessCheck("event log database", eventLogDatabase))
	}
	return checks
}

func pingReadinessCheck(name string, store PingStore) ReadinessCheck {
	if store == nil {
		return StaticReadinessCheck(name, false)
	}
	return ReadinessCheck{Name: name, Check: store.Ping}
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
