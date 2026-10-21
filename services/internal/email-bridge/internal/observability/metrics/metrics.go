package metrics

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics owns bounded counters for mailbox operations and owner reconciliation without labels derived from messages or identities.
// Metrics содержит счётчики почтовых операций и сверки с владельцем с ограниченным набором меток, не зависящих от писем или идентичностей.
type Metrics struct{ Operations, Reconciliations *prometheus.CounterVec }

// New creates separate operation/outcome and reconciliation/outcome counters without registering them globally.
// New создаёт отдельные счётчики операций и сверки по результатам без их глобальной регистрации.
func New() *Metrics {
	return &Metrics{Operations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_email_bridge_operations_total", Help: "Total bounded mailbox operation outcomes."}, []string{"operation", "outcome"}), Reconciliations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_email_bridge_reconciliations_total", Help: "Total bounded owner reconciliation outcomes."}, []string{"outcome"})}
}

// Reconciliation increments only a closed outcome label, folding unknown values into the error bucket.
// Reconciliation увеличивает счётчик только для закрытого набора результатов, сводя неизвестные значения к ошибке.
func (m *Metrics) Reconciliation(outcome string) {
	switch outcome {
	case "committed", "replay", "reported", "none", "denied", "invalid", "barrier", "error":
	default:
		outcome = "error"
	}
	m.Reconciliations.WithLabelValues(outcome).Inc()
}

// Record counts a mailbox operation after constraining both the operation and outcome labels to finite sets.
// Record учитывает почтовую операцию после ограничения меток операции и результата конечными наборами.
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
