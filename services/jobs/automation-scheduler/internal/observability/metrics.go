package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	cycles        *prometheus.CounterVec
	occurrences   *prometheus.CounterVec
	trackedClaims prometheus.Gauge
}

const metricsSubsystem = "automation_scheduler"

func NewMetrics() *Metrics {
	return &Metrics{
		cycles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: metricsSubsystem, Name: "cycles_total",
			Help: "Total number of completed automation scheduler cycles.",
		}, []string{"outcome"}),
		occurrences: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: metricsSubsystem, Name: "occurrences_total",
			Help: "Total number of automation schedule occurrence processing outcomes.",
		}, []string{"outcome"}),
		trackedClaims: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kodex", Subsystem: metricsSubsystem, Name: "tracked_claims",
			Help: "Current number of schedule claims held by this automation scheduler replica.",
		}),
	}
}

func (metrics *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.occurrences, metrics.trackedClaims}
}

func (metrics *Metrics) Cycle(failed bool) {
	outcome := "success"
	if failed {
		outcome = "error"
	}
	metrics.cycles.WithLabelValues(outcome).Inc()
}

func (metrics *Metrics) Occurrence(outcome string) {
	switch outcome {
	case "invalid", "renew_error", "materialize_error", "materialized":
	default:
		outcome = "other"
	}
	metrics.occurrences.WithLabelValues(outcome).Inc()
}

func (metrics *Metrics) Track(delta float64) { metrics.trackedClaims.Add(delta) }
