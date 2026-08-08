package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	workerCycles *prometheus.CounterVec
	invocations  *prometheus.CounterVec
}

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	metrics := &Metrics{
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "integration_gateway", Name: "http_requests_total",
			Help: "Total number of completed integration gateway HTTP requests.",
		}, []string{"route", "outcome"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mattercodex", Subsystem: "integration_gateway", Name: "http_request_duration_seconds",
			Help: "Duration of completed integration gateway HTTP requests in seconds.", Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		workerCycles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "integration_gateway", Name: "worker_cycles_total",
			Help: "Total number of bounded integration gateway worker cycles.",
		}, []string{"worker", "outcome"}),
		invocations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "integration_gateway", Name: "invocations_total",
			Help: "Total number of integration invocation state outcomes.",
		}, []string{"outcome"}),
	}
	if err := register(metrics.httpRequests, metrics.httpDuration, metrics.workerCycles, metrics.invocations); err != nil {
		return nil, err
	}
	return metrics, nil
}

func (metrics *Metrics) ObserveHTTP(route, outcome string, started time.Time) {
	metrics.httpRequests.WithLabelValues(normalizeRoute(route), normalizeOutcome(outcome)).Inc()
	metrics.httpDuration.WithLabelValues(normalizeRoute(route)).Observe(time.Since(started).Seconds())
}

func (metrics *Metrics) ObserveWorker(worker, outcome string) {
	metrics.workerCycles.WithLabelValues(normalizeWorker(worker), normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) ObserveInvocation(outcome string) {
	metrics.invocations.WithLabelValues(normalizeOutcome(outcome)).Inc()
}

func normalizeRoute(value string) string {
	switch value {
	case "mcp", "api", "technical":
		return value
	default:
		return "unknown"
	}
}

func normalizeWorker(value string) string {
	switch value {
	case "execution", "continuation", "lifecycle", "management", "readiness":
		return value
	default:
		return "unknown"
	}
}

func normalizeOutcome(value string) string {
	switch value {
	case "success", "failure", "idle", "accepted", "rejected":
		return value
	default:
		return "unknown"
	}
}
