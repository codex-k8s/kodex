package observability

import "github.com/prometheus/client_golang/prometheus"

// SecretDrafts owns bounded recovery-outcome/deletion counters and latest recovery-readiness gauge; it
// observes completed effects without granting deletion or acknowledging owner receipts.
// SecretDrafts владеет bounded recovery-outcome/deletion counters и gauge последней recovery readiness;
// наблюдает выполненные эффекты без выдачи deletion authority и подтверждения owner receipts.
type SecretDrafts struct {
	cycles    *prometheus.CounterVec
	deletions *prometheus.CounterVec
	ready     prometheus.Gauge
}

// NewSecretDrafts creates unregistered collectors with fixed outcome/kind label dimensions and English
// HELP; registering them belongs to composition.
// NewSecretDrafts создаёт незарегистрированные collectors с фиксированными outcome/kind label dimensions и
// английским HELP; регистрирует composition.
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

// Collectors returns a new slice referencing the same three collectors for registration, not cloned metric
// state.
// Collectors возвращает новый slice со ссылками на те же три collectors для регистрации, не копию metric
// state.
func (metrics *SecretDrafts) Collectors() []prometheus.Collector {
	return []prometheus.Collector{metrics.cycles, metrics.deletions, metrics.ready}
}

// EncryptedDeleted increments only the fixed encrypted-kind counter after a completed local deletion,
// independently of later acknowledgement failure.
// EncryptedDeleted увеличивает лишь fixed encrypted-kind counter после выполненного local deletion
// независимо от последующего acknowledgement failure.
func (metrics *SecretDrafts) EncryptedDeleted() { metrics.deletions.WithLabelValues("encrypted").Inc() }

// RuntimeDeleted increments only the fixed runtime-kind counter and does not imply owner cleanup
// acknowledgement.
// RuntimeDeleted увеличивает лишь fixed runtime-kind counter и не означает owner cleanup acknowledgement.
func (metrics *SecretDrafts) RuntimeDeleted() { metrics.deletions.WithLabelValues("runtime").Inc() }

// RecoveryCompleted increments one success/error cycle counter and sets the latest readiness gauge to 1/0
// without resetting earlier deletion counts.
// RecoveryCompleted увеличивает один success/error cycle counter и выставляет latest readiness gauge в 1/0
// без сброса прежних deletion counts.
func (metrics *SecretDrafts) RecoveryCompleted(success bool) {
	outcome, ready := "error", float64(0)
	if success {
		outcome, ready = "success", 1
	}
	metrics.cycles.WithLabelValues(outcome).Inc()
	metrics.ready.Set(ready)
}
