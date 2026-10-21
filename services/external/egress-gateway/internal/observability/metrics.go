// Package observability owns gateway metrics with bounded label cardinality.
// Package observability содержит метрики шлюза с ограниченной кардинальностью меток.
package observability

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// RegisterCollectors registers service-owned collectors in the shared registry.
// RegisterCollectors регистрирует принадлежащие сервису коллекторы в общем реестре.
type RegisterCollectors func(...prometheus.Collector) error

// Metrics holds only gateway-specific collectors with closed label sets.
// Metrics содержит только коллекторы шлюза с закрытыми наборами меток.
type Metrics struct {
	connections  *prometheus.CounterVec
	dns          *prometheus.CounterVec
	dials        *prometheus.CounterVec
	active       prometheus.Gauge
	policyActive prometheus.Gauge
}

// New creates gateway collectors and registers them through the service registry.
// New создаёт коллекторы шлюза и регистрирует их через реестр сервиса.
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

// Connection counts a tunnel outcome after normalizing every label to a closed set.
// Connection учитывает исход туннеля после нормализации каждой метки в закрытый набор.
func (metrics *Metrics) Connection(outcome, stage, reason string) {
	metrics.connections.WithLabelValues(normalizeOutcome(outcome), normalizeStage(stage), normalizeReason(reason)).Inc()
}

// DNSObserver records closed DNS outcome and reason without host or IP labels.
// DNSObserver фиксирует закрытые исход и причину DNS без меток узла и IP.
func (metrics *Metrics) DNSObserver(outcome, reason string) {
	metrics.dns.WithLabelValues(normalizeDNSOutcome(outcome), normalizeReason(reason)).Inc()
}

// Dial counts literal-address dial outcomes without recording destinations.
// Dial учитывает исходы подключения по буквальному адресу без записи адресатов.
func (metrics *Metrics) Dial(outcome, reason string) {
	metrics.dials.WithLabelValues(normalizeDialOutcome(outcome), normalizeReason(reason)).Inc()
}

// AddActive adjusts the gauge for currently accepted tunnel connections.
// AddActive изменяет показатель текущих принятых туннельных соединений.
func (metrics *Metrics) AddActive(delta float64) { metrics.active.Add(delta) }

// SetPolicyActive reflects whether startup policy validation succeeded.
// SetPolicyActive отражает результат проверки политики при запуске.
func (metrics *Metrics) SetPolicyActive(active bool) {
	if active {
		metrics.policyActive.Set(1)
		return
	}
	metrics.policyActive.Set(0)
}

// RegisterMailReadiness samples live TTL-aware mail readiness without source-derived labels.
// RegisterMailReadiness получает актуальную готовность почты с учётом TTL без меток из источника.
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

// RegisterIntegrationReadiness reports readiness of the separate OpenAPI listener.
// RegisterIntegrationReadiness сообщает готовность отдельного входа OpenAPI.
func RegisterIntegrationReadiness(register RegisterCollectors, ready func() (bool, string)) error {
	return register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "kodex", Subsystem: "egress_gateway", Name: "integration_ready",
		Help: "Whether the immutable integration projection and current DNS pins are ready.",
	}, func() float64 {
		if value, _ := ready(); value {
			return 1
		}
		return 0
	}))
}

// normalizeOutcome maps arbitrary connection outcomes to approved metric labels.
// normalizeOutcome переводит произвольные исходы соединения в разрешённые метки метрик.
func normalizeOutcome(value string) string {
	switch value {
	case "completed", "rejected", "failed", "cancelled":
		return value
	default:
		return "unknown"
	}
}

// normalizeDNSOutcome maps DNS outcomes to a fixed metric vocabulary.
// normalizeDNSOutcome переводит исходы DNS в фиксированный словарь метрик.
func normalizeDNSOutcome(value string) string {
	switch value {
	case "validated", "cache_hit", "rejected":
		return value
	default:
		return "unknown"
	}
}

// normalizeDialOutcome maps external dial results to a fixed metric vocabulary.
// normalizeDialOutcome переводит исходы внешнего подключения в фиксированный словарь метрик.
func normalizeDialOutcome(value string) string {
	switch value {
	case "success", "failure":
		return value
	default:
		return "unknown"
	}
}

// normalizeStage keeps connection stage labels within a fixed operational set.
// normalizeStage ограничивает метки этапов соединения фиксированным рабочим набором.
func normalizeStage(value string) string {
	switch value {
	case "accept", "connect", "clienthello", "dns", "dial", "tunnel", "proxy", "shutdown":
		return value
	default:
		return "unknown"
	}
}

// normalizeReason collapses unrecognized failure text to a single bounded label.
// normalizeReason сводит неизвестный текст причины к одной ограниченной метке.
func normalizeReason(value string) string {
	switch value {
	case "none", "malformed", "method", "authority", "body", "credentials", "oversized", "policy",
		"missing_sni", "duplicate_sni", "sni_mismatch", "ech", "timeout", "nxdomain", "truncated",
		"bounds", "special_address", "empty", "connection_limit", "dial_failure", "io", "shutdown", "not_ready",
		"certificate", "tls", "sni", "request", "upstream":
		return value
	default:
		return "unknown"
	}
}
