package app

import (
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/capture"
	"github.com/prometheus/client_golang/prometheus"
)

// retentionMetricOutcomeLabel names only the closed technical outcome dimension.
// retentionMetricOutcomeLabel обозначает только closed technical outcome dimension.
const retentionMetricOutcomeLabel = "outcome"

// retentionMetrics groups finite-outcome cycle, completed purge and failure counters.
// RU: retentionMetrics объединяет счётчики циклов с конечными исходами, завершённых удалений и отказов.
type retentionMetrics struct {
	cycles   *prometheus.CounterVec
	purged   prometheus.Counter
	failures prometheus.Counter
	captures *prometheus.CounterVec
}

// newRetentionMetrics constructs unregistered collectors with a bounded outcome label.
// RU: newRetentionMetrics создаёт незарегистрированные collectors с ограниченной меткой исхода.
func newRetentionMetrics() *retentionMetrics {
	return &retentionMetrics{
		captures: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "capture_tasks_total", Help: "Total number of retained output tasks by closed technical outcome."}, []string{retentionMetricOutcomeLabel}),
		cycles:   prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "cycles_total", Help: "Total number of completed artifact retention cycles."}, []string{retentionMetricOutcomeLabel}),
		purged:   prometheus.NewCounter(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "purged_artifacts_total", Help: "Total number of permanently purged artifacts."}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "failures_total", Help: "Total number of failed artifact retention cycles."}),
	}
}

// collectors returns the owned metrics for registration by the process composition root.
// RU: collectors возвращает принадлежащие job метрики для регистрации в composition root.
func (metrics *retentionMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.purged, metrics.failures, metrics.captures}
}

// recordCapture counts confirmed task facts even if a later operation reports an error.
// Outcome values are a fixed technical set; remote outcomes, task refs and source values never
// become metric labels. PARTIAL remains distinct from complete transcript coverage.
// recordCapture учитывает confirmed task facts даже при ошибке следующей операции.
// Outcome — fixed technical set; remote outcomes, task refs и source values не входят
// в metric labels. PARTIAL отделён от complete transcript coverage.
func (metrics *retentionMetrics) recordCapture(result capture.Result) {
	if result.Complete > 0 {
		outcome := "complete"
		if result.Partial > 0 {
			outcome = "partial"
		}
		metrics.captures.WithLabelValues(outcome).Add(float64(result.Complete))
	}
	if result.Closed > 0 {
		metrics.captures.WithLabelValues("closed").Add(float64(result.Closed))
	}
	if result.Uncertain > 0 {
		metrics.captures.WithLabelValues("uncertain").Add(float64(result.Uncertain))
	}
}
