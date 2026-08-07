// Package observability хранит service-owned bounded-cardinality metrics.
package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics владеет изолированным Prometheus registry.
type Metrics struct {
	registry     *prometheus.Registry
	connections  *prometheus.CounterVec
	dns          *prometheus.CounterVec
	dials        *prometheus.CounterVec
	active       prometheus.Gauge
	readiness    prometheus.Gauge
	policyActive prometheus.Gauge
}

// NewMetrics создаёт collectors только с закрытыми labels.
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		registry: registry,
		connections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "egress_gateway", Name: "connection_attempts_total",
			Help: "Total number of bounded CONNECT connection outcomes.",
		}, []string{"outcome", "stage", "reason"}),
		dns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "egress_gateway", Name: "dns_resolutions_total",
			Help: "Total number of server-owned DNS resolution outcomes.",
		}, []string{"outcome", "reason"}),
		dials: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "egress_gateway", Name: "external_dials_total",
			Help: "Total number of literal external dial outcomes.",
		}, []string{"outcome", "reason"}),
		active: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "egress_gateway", Name: "active_connections",
			Help: "Current number of bounded CONNECT connections.",
		}),
		readiness: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "egress_gateway", Name: "readiness",
			Help: "Current readiness state of the effective gateway path.",
		}),
		policyActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "egress_gateway", Name: "policy_active",
			Help: "Whether the immutable policy passed revision and digest validation.",
		}),
	}
	registry.MustRegister(metrics.connections, metrics.dns, metrics.dials, metrics.active, metrics.readiness, metrics.policyActive, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return metrics
}

// Handler возвращает metrics endpoint.
func (metrics *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{})
}

// Connection учитывает только нормализованные закрытые значения.
func (metrics *Metrics) Connection(outcome, stage, reason string) {
	metrics.connections.WithLabelValues(normalizeOutcome(outcome), normalizeStage(stage), normalizeReason(reason)).Inc()
}

// DNSObserver адаптирует закрытые DNS outcome/reason без hostname/IP labels.
func (metrics *Metrics) DNSObserver(outcome, reason string) {
	metrics.dns.WithLabelValues(normalizeDNSOutcome(outcome), normalizeReason(reason)).Inc()
}

// Dial учитывает literal dial outcome.
func (metrics *Metrics) Dial(outcome, reason string) {
	metrics.dials.WithLabelValues(normalizeDialOutcome(outcome), normalizeReason(reason)).Inc()
}

// AddActive обновляет gauge активных соединений.
func (metrics *Metrics) AddActive(delta float64) { metrics.active.Add(delta) }

// SetReady обновляет effective readiness gauge.
func (metrics *Metrics) SetReady(ready bool) {
	if ready {
		metrics.readiness.Set(1)
		return
	}
	metrics.readiness.Set(0)
}

// SetPolicyActive фиксирует startup policy validation.
func (metrics *Metrics) SetPolicyActive(active bool) {
	if active {
		metrics.policyActive.Set(1)
		return
	}
	metrics.policyActive.Set(0)
}

func normalizeOutcome(value string) string {
	switch value {
	case "completed", "rejected", "failed", "cancelled":
		return value
	default:
		return "unknown"
	}
}

func normalizeDNSOutcome(value string) string {
	switch value {
	case "validated", "cache_hit", "rejected":
		return value
	default:
		return "unknown"
	}
}

func normalizeDialOutcome(value string) string {
	switch value {
	case "success", "failure":
		return value
	default:
		return "unknown"
	}
}

func normalizeStage(value string) string {
	switch value {
	case "accept", "connect", "clienthello", "dns", "dial", "tunnel", "shutdown":
		return value
	default:
		return "unknown"
	}
}

func normalizeReason(value string) string {
	switch value {
	case "none", "malformed", "method", "authority", "body", "credentials", "oversized", "policy",
		"missing_sni", "duplicate_sni", "sni_mismatch", "ech", "timeout", "nxdomain", "truncated",
		"bounds", "special_address", "empty", "connection_limit", "dial_failure", "io", "shutdown":
		return value
	default:
		return "unknown"
	}
}
