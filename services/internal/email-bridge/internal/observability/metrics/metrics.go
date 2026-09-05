package metrics

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct{ Operations, Reconciliations *prometheus.CounterVec }

func New() *Metrics {
	return &Metrics{Operations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_email_bridge_operations_total", Help: "Total bounded mailbox operation outcomes."}, []string{"operation", "outcome"}), Reconciliations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_email_bridge_reconciliations_total", Help: "Total bounded owner reconciliation outcomes."}, []string{"outcome"})}
}

func (m *Metrics) Reconciliation(outcome string) {
	switch outcome {
	case "committed", "replay", "reported", "none", "denied", "invalid", "barrier", "error":
	default:
		outcome = "error"
	}
	m.Reconciliations.WithLabelValues(outcome).Inc()
}
func (m *Metrics) Record(op api.Operation, outcome string) {
	if !op.Valid() {
		op = "other"
	}
	switch outcome {
	case "success", "error", "unknown":
	default:
		outcome = "error"
	}
	m.Operations.WithLabelValues(string(op), outcome).Inc()
}
