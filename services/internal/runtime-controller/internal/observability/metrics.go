// Package observability содержит ограниченные business-метрики runtime-controller.
package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	operations *prometheus.CounterVec
	capacity   *prometheus.GaugeVec
	failures   *prometheus.CounterVec
}

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	operations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "operations_total",
		Help: "Total number of bounded runtime-controller lifecycle operations.",
	}, []string{"operation", "outcome"})
	capacity := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "capacity_state",
		Help: "Current bounded runtime-controller capacity state.",
	}, []string{"state"})
	failures := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "runtime_controller", Name: "failures_total",
		Help: "Total number of bounded runtime-controller worker failures.",
	}, []string{"worker"})
	if err := register(operations, capacity, failures); err != nil {
		return nil, err
	}
	return &Metrics{operations: operations, capacity: capacity, failures: failures}, nil
}

func (metrics *Metrics) ObserveFailure(worker string) {
	switch worker {
	case "claim_loop", "reconcile_loop", "expiry_loop", "temporary_cleanup_loop",
		"readiness_loop", "event_consume":
		metrics.failures.WithLabelValues(worker).Inc()
	}
}

func (metrics *Metrics) Observe(operation, outcome string) {
	if !validOperation(operation) || !validOutcome(outcome) {
		return
	}
	metrics.operations.WithLabelValues(operation, outcome).Inc()
	if operation == "capacity" {
		metrics.capacity.Reset()
		metrics.capacity.WithLabelValues(outcome).Set(1)
	}
}

func validOperation(value string) bool {
	switch value {
	case "claim", "capacity", "evict", "materialize", "heartbeat", "complete",
		"incident", "archive", "restore", "cleanup_authorization", "cleanup",
		"idle_eviction", "temporary_cleanup", "event_consume":
		return true
	default:
		return false
	}
}

func validOutcome(value string) bool {
	switch value {
	case "empty", "error", "invalid", "deferred", "admitted", "deleted", "rejected",
		"materialized", "renewed", "terminal", "recorded", "scheduled", "consumed":
		return true
	default:
		return false
	}
}
