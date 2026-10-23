package app

import "github.com/prometheus/client_golang/prometheus"

// retentionMetrics groups finite-outcome cycle, completed purge and failure counters.
// RU: retentionMetrics объединяет счётчики циклов с конечными исходами, завершённых удалений и отказов.
type retentionMetrics struct {
	cycles   *prometheus.CounterVec
	purged   prometheus.Counter
	failures prometheus.Counter
}

// newRetentionMetrics constructs unregistered collectors with a bounded outcome label.
// RU: newRetentionMetrics создаёт незарегистрированные collectors с ограниченной меткой исхода.
func newRetentionMetrics() *retentionMetrics {
	return &retentionMetrics{
		cycles:   prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "cycles_total", Help: "Total number of completed artifact retention cycles."}, []string{"outcome"}),
		purged:   prometheus.NewCounter(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "purged_artifacts_total", Help: "Total number of permanently purged artifacts."}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "failures_total", Help: "Total number of failed artifact retention cycles."}),
	}
}

// collectors returns the owned metrics for registration by the process composition root.
// RU: collectors возвращает принадлежащие job метрики для регистрации в composition root.
func (metrics *retentionMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.purged, metrics.failures}
}
