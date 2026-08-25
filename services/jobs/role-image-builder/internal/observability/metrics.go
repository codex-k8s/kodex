// Package observability содержит business-метрики с закрытыми label sets.
package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct{ operations *prometheus.CounterVec }

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	operations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kodex", Subsystem: "role_image_builder", Name: "operations_total",
		Help: "Total number of bounded role image build lifecycle operations.",
	}, []string{"operation", "outcome"})
	if err := register(operations); err != nil {
		return nil, err
	}
	return &Metrics{operations: operations}, nil
}

func (metrics *Metrics) Observe(operation, outcome string) {
	if !validOperation(operation) || !validOutcome(outcome) {
		return
	}
	metrics.operations.WithLabelValues(operation, outcome).Inc()
}

func validOperation(value string) bool {
	switch value {
	case "claim", "materialize", "context", "renew", "build", "complete":
		return true
	default:
		return false
	}
}

func validOutcome(value string) bool {
	switch value {
	case "success", "empty", "rejected", "error":
		return true
	default:
		return false
	}
}
