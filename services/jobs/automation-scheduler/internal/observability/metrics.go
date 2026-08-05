// Package observability содержит business-метрики automation-scheduler с закрытыми labels.
package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	cycles      *prometheus.CounterVec
	occurrences *prometheus.CounterVec
	tracked     prometheus.Gauge
}

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	cycles := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "automation_scheduler", Name: "cycles_total",
		Help: "Total number of bounded automation scheduler reconciliation cycles.",
	}, []string{"outcome"})
	occurrences := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex", Subsystem: "automation_scheduler", Name: "occurrences_total",
		Help: "Total number of automation schedule occurrence operations.",
	}, []string{"operation", "outcome"})
	tracked := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "mattercodex", Subsystem: "automation_scheduler", Name: "tracked_claims",
		Help: "Current number of transiently tracked schedule claims.",
	})
	if err := register(cycles, occurrences, tracked); err != nil {
		return nil, err
	}
	return &Metrics{cycles: cycles, occurrences: occurrences, tracked: tracked}, nil
}

func (metrics *Metrics) ObserveCycle(outcome string) {
	switch outcome {
	case "success", "partial", "error":
		metrics.cycles.WithLabelValues(outcome).Inc()
	}
}

func (metrics *Metrics) ObserveOccurrence(operation, outcome string) {
	if !validOperation(operation) || !validOutcome(outcome) {
		return
	}
	metrics.occurrences.WithLabelValues(operation, outcome).Inc()
}

func (metrics *Metrics) SetTrackedClaims(count int) {
	if count >= 0 {
		metrics.tracked.Set(float64(count))
	}
}

func validOperation(value string) bool {
	switch value {
	case "materialize", "claim", "complete", "watchdog":
		return true
	default:
		return false
	}
}

func validOutcome(value string) bool {
	switch value {
	case "created", "empty", "claimed", "pending", "terminal", "expired", "retired", "error":
		return true
	default:
		return false
	}
}
