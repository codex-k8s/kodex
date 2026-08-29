package app

import "github.com/prometheus/client_golang/prometheus"

type retentionMetrics struct {
	cycles   *prometheus.CounterVec
	purged   prometheus.Counter
	failures prometheus.Counter
}

func newRetentionMetrics() *retentionMetrics {
	return &retentionMetrics{
		cycles:   prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "cycles_total", Help: "Total number of completed artifact retention cycles."}, []string{"outcome"}),
		purged:   prometheus.NewCounter(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "purged_artifacts_total", Help: "Total number of permanently purged artifacts."}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{Namespace: "kodex", Subsystem: metricsSubsystem, Name: "failures_total", Help: "Total number of failed artifact retention cycles."}),
	}
}

func (metrics *retentionMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.purged, metrics.failures}
}
