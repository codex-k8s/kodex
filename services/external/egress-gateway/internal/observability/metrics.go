// Package observability хранит service-owned bounded-cardinality metrics.
package observability

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// RegisterCollectors регистрирует service-owned collectors в общем registry.
type RegisterCollectors func(...prometheus.Collector) error

// Metrics содержит только gateway-specific collectors с закрытыми labels.
type Metrics struct {
	connections  *prometheus.CounterVec
	dns          *prometheus.CounterVec
	dials        *prometheus.CounterVec
	active       prometheus.Gauge
	policyActive prometheus.Gauge
}

// New создаёт collectors и регистрирует их в общем service registry.
func New(register RegisterCollectors) (*Metrics, error) {
	metrics := &Metrics{
		connections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "egress_gateway", Name: "connection_attempts_total",
			Help: "Total number of bounded CONNECT connection outcomes.",
		}, []string{"outcome", "stage", "reason"}),
		dns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "egress_gateway", Name: "dns_resolutions_total",
			Help: "Total number of server-owned DNS resolution outcomes.",
		}, []string{"outcome", "reason"}),
		dials: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kodex", Subsystem: "egress_gateway", Name: "external_dials_total",
			Help: "Total number of literal external dial outcomes.",
		}, []string{"outcome", "reason"}),
		active: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kodex", Subsystem: "egress_gateway", Name: "active_connections",
			Help: "Current number of bounded CONNECT connections.",
		}),
		policyActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kodex", Subsystem: "egress_gateway", Name: "policy_active",
			Help: "Whether the immutable policy passed revision and digest validation.",
		}),
	}
	if register == nil {
		return nil, errors.New("gateway metrics registry is required")
	}
	if err := register(metrics.connections, metrics.dns, metrics.dials, metrics.active, metrics.policyActive); err != nil {
		return nil, err
	}
	return metrics, nil
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

// SetPolicyActive фиксирует startup policy validation.
func (metrics *Metrics) SetPolicyActive(active bool) {
	if active {
		metrics.policyActive.Set(1)
		return
	}
	metrics.policyActive.Set(0)
}

// RegisterMailReadiness читает фактическую TTL-aware readiness без labels из source.
func RegisterMailReadiness(register RegisterCollectors, ready func() (bool, string)) error {
	return register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "kodex", Subsystem: "egress_gateway", Name: "mail_ready",
		Help: "Whether the immutable mail projection and current DNS pins are ready.",
	}, func() float64 {
		if value, _ := ready(); value {
			return 1
		}
		return 0
	}))
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
		"bounds", "special_address", "empty", "connection_limit", "dial_failure", "io", "shutdown", "not_ready":
		return value
	default:
		return "unknown"
	}
}
