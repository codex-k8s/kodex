package observability

import "github.com/prometheus/client_golang/prometheus"

type SecretDrafts struct {
	cycles    *prometheus.CounterVec
	deletions *prometheus.CounterVec
	ready     prometheus.Gauge
}

func NewSecretDrafts() *SecretDrafts {
	return &SecretDrafts{
		cycles: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "secret_broker",
			Name: "draft_recovery_cycles_total", Help: "Total completed secret draft recovery cycles by bounded outcome."}, []string{"outcome"}),
		deletions: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "secret_broker",
			Name: "draft_cleanup_readbacks_total", Help: "Total exact secret draft cleanup readbacks by bounded object kind."}, []string{"kind"}),
		ready: prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "kodex", Subsystem: "secret_broker",
			Name: "draft_recovery_ready", Help: "Whether the last bounded secret draft recovery cycle completed successfully."}),
	}
}

func (metrics *SecretDrafts) Collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.deletions, metrics.ready}
}
func (metrics *SecretDrafts) EncryptedDeleted() { metrics.deletions.WithLabelValues("encrypted").Inc() }
func (metrics *SecretDrafts) RuntimeDeleted()   { metrics.deletions.WithLabelValues("runtime").Inc() }
func (metrics *SecretDrafts) RecoveryCompleted(success bool) {
	outcome, ready := "error", float64(0)
	if success {
		outcome, ready = "success", 1
	}
	metrics.cycles.WithLabelValues(outcome).Inc()
	metrics.ready.Set(ready)
}
