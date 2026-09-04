// Package metrics описывает закрытые outcomes интеграционного worker.
package metrics

import (
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct{ cycles, operations *prometheus.CounterVec }

func New(registry *observability.Metrics) (*Metrics, error) {
	m := &Metrics{
		cycles:     prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "integration_gateway", Name: "cycles_total", Help: "Total completed integration worker cycles."}, []string{"outcome"}),
		operations: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "integration_gateway", Name: "operations_total", Help: "Total adapter outcomes before receipt persistence."}, []string{"operation", "outcome"}),
	}
	if err := registry.Register(m.cycles, m.operations); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Metrics) Cycle(err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	m.cycles.WithLabelValues(outcome).Inc()
}
func (m *Metrics) Operation(test, success, unknown bool) {
	operation := "execute"
	if test {
		operation = "test"
	}
	outcome := "success"
	if !success {
		outcome = "error"
	}
	if unknown {
		outcome = "unknown"
	}
	m.operations.WithLabelValues(operation, outcome).Inc()
}
