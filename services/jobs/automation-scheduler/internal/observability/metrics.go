package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics owns bounded scheduler cycle, occurrence and in-flight-claim measurements.
// RU: Metrics владеет метриками циклов планировщика, исходов occurrence и текущих claims.
type Metrics struct {
	cycles        *prometheus.CounterVec
	occurrences   *prometheus.CounterVec
	trackedClaims prometheus.Gauge
}

// metricsSubsystem keeps the owned counters and gauge under the scheduler's metric namespace.
// RU: metricsSubsystem помещает собственные счётчики и gauge в подсистему планировщика.
const metricsSubsystem = "automation_scheduler"

// NewMetrics constructs unregistered collectors; the composition root registers them once.
// RU: NewMetrics создаёт незарегистрированные collectors; корень композиции регистрирует их один раз.
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

// Collectors exposes exactly the owned cycle, occurrence and tracked-claim collectors.
// RU: Collectors возвращает только собственные collectors циклов, occurrences и текущих claims.
func (metrics *Metrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.occurrences, metrics.trackedClaims}
}

// Cycle records a completed poll as success or error independently of per-occurrence outcomes.
// RU: Cycle учитывает завершённый опрос как успех или ошибку отдельно от исходов occurrences.
func (metrics *Metrics) Cycle(failed bool) {
	outcome := "success"
	if failed {
		outcome = "error"
	}
	metrics.cycles.WithLabelValues(outcome).Inc()
}

// Occurrence normalizes an unrecognized outcome to one fixed fallback label before counting it.
// RU: Occurrence заменяет неизвестный исход одной фиксированной меткой до увеличения счётчика.
func (metrics *Metrics) Occurrence(outcome string) {
	switch outcome {
	case "invalid", "renew_error", "materialize_error", "materialized":
	default:
		outcome = "other"
	}
	metrics.occurrences.WithLabelValues(outcome).Inc()
}

// Track adjusts the local in-flight-claim gauge on claim acquisition or release.
// RU: Track изменяет локальный gauge текущих claims при получении или освобождении аренды.
func (metrics *Metrics) Track(delta float64) { metrics.trackedClaims.Add(delta) }
