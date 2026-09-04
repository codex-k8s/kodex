package metrics

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct{ Operations *prometheus.CounterVec }

func New() *Metrics {
	return &Metrics{Operations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_email_bridge_operations_total", Help: "Total bounded mailbox operation outcomes."}, []string{"operation", "outcome"})}
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
