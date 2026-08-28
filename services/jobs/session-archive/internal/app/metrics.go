package app

import "github.com/prometheus/client_golang/prometheus"

type archiveMetrics struct {
	cycles   *prometheus.CounterVec
	tasks    *prometheus.CounterVec
	duration *prometheus.HistogramVec
	active   prometheus.Gauge
	bytes    *prometheus.CounterVec
}

func newArchiveMetrics() *archiveMetrics {
	return &archiveMetrics{
		cycles:   prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "session_archive", Name: "cycles_total", Help: "Total number of session archive reconciliation cycles."}, []string{"outcome"}),
		tasks:    prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "session_archive", Name: "tasks_total", Help: "Total number of completed session archive tasks."}, []string{"kind", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "kodex", Subsystem: "session_archive", Name: "task_duration_seconds", Help: "Duration of session archive tasks in seconds.", Buckets: prometheus.DefBuckets}, []string{"kind"}),
		active:   prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "kodex", Subsystem: "session_archive", Name: "active_workers", Help: "Current number of active session archive workers."}),
		bytes:    prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "session_archive", Name: "bytes_total", Help: "Total bytes processed by successful session archive tasks."}, []string{"kind"}),
	}
}

func (metrics *archiveMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.tasks, metrics.duration, metrics.active, metrics.bytes}
}
