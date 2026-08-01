// Package observability содержит ограниченные бизнесовые метрики control-plane.
package observability

import (
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/prometheus/client_golang/prometheus"
)

// BusinessMetrics реализует domain observer без aggregate/tenant labels.
type BusinessMetrics struct {
	mutations *prometheus.CounterVec
}

// NewBusinessMetrics регистрирует закрытые kind/action labels.
func NewBusinessMetrics(
	register func(...prometheus.Collector) error,
) (*BusinessMetrics, error) {
	mutations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mattercodex",
		Subsystem: "control_plane",
		Name:      "mutations_total",
		Help:      "Total number of durably committed control-plane mutations.",
	}, []string{"kind", "action"})
	if err := register(mutations); err != nil {
		return nil, err
	}
	return &BusinessMetrics{mutations: mutations}, nil
}

// ObserveMutation учитывает только уже проверенные enum/action.
func (metrics *BusinessMetrics) ObserveMutation(kind enum.Kind, action string) {
	if !kind.Valid() {
		return
	}
	switch action {
	case "create", "update", "transition", "delete",
		"enqueue_turn", "claim_turn", "complete_turn",
		"claim_schedules", "resolve_owner_gate":
		metrics.mutations.WithLabelValues(string(kind), action).Inc()
	}
}
