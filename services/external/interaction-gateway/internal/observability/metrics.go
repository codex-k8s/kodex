// Package observability содержит только business/transport метрики gateway.
package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	httpRequests    *prometheus.CounterVec
	httpDuration    *prometheus.HistogramVec
	workerCycles    *prometheus.CounterVec
	inbound         *prometheus.CounterVec
	deliveries      *prometheus.CounterVec
	externalEffects *prometheus.CounterVec
	teamOperations  *prometheus.CounterVec
}

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	metrics := &Metrics{
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "http_requests_total",
			Help: "Total number of completed interaction gateway HTTP requests.",
		}, []string{"route", "outcome"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "http_request_duration_seconds",
			Help: "Duration of completed interaction gateway HTTP requests in seconds.", Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		workerCycles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "worker_cycles_total",
			Help: "Total number of bounded interaction gateway worker cycles.",
		}, []string{"worker", "outcome"}),
		inbound: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "inbound_events_total",
			Help: "Total number of durable inbound Mattermost event outcomes.",
		}, []string{"kind", "outcome"}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "deliveries_total",
			Help: "Total number of durable Mattermost delivery outcomes.",
		}, []string{"kind", "outcome"}),
		externalEffects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "external_effects_total",
			Help: "Total number of confirmed external Mattermost effects.",
		}, []string{"effect", "outcome"}),
		teamOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "interaction_gateway", Name: "mattermost_team_operations_total",
			Help: "Total number of bounded Mattermost Team provider operation outcomes.",
		}, []string{"operation", "outcome"}),
	}
	if err := register(metrics.httpRequests, metrics.httpDuration, metrics.workerCycles, metrics.inbound,
		metrics.deliveries, metrics.externalEffects, metrics.teamOperations); err != nil {
		return nil, err
	}
	return metrics, nil
}

func (metrics *Metrics) ObserveTeamOperation(operation, outcome string) {
	metrics.teamOperations.WithLabelValues(normalizeTeamOperation(operation), normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) ObserveExternalEffect(effect, outcome string) {
	metrics.externalEffects.WithLabelValues(normalizeEffect(effect), normalizeOutcome(outcome)).Inc()
}

func normalizeEffect(value string) string {
	switch value {
	case "upload_file", "create_post", "update_post", "create_team",
		"workspace_mapping_bind", "workspace_mapping_relink", "workspace_mapping_unlink":
		return value
	default:
		return "unknown"
	}
}

func (metrics *Metrics) ObserveHTTP(route, outcome string, started time.Time) {
	route, outcome = normalizeRoute(route), normalizeOutcome(outcome)
	metrics.httpRequests.WithLabelValues(route, outcome).Inc()
	metrics.httpDuration.WithLabelValues(route).Observe(time.Since(started).Seconds())
}

func (metrics *Metrics) ObserveWorker(worker, outcome string) {
	metrics.workerCycles.WithLabelValues(normalizeWorker(worker), normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) ObserveInbound(kind, outcome string) {
	metrics.inbound.WithLabelValues(normalizeInbound(kind), normalizeOutcome(outcome)).Inc()
}

func (metrics *Metrics) ObserveDelivery(kind, outcome string) {
	metrics.deliveries.WithLabelValues(normalizeDelivery(kind), normalizeOutcome(outcome)).Inc()
}

func normalizeRoute(value string) string {
	switch value {
	case "slash", "action", "dialog", "readback", "materialization", "runtime_output", "technical":
		return value
	default:
		return "unknown"
	}
}

func normalizeWorker(value string) string {
	switch value {
	case "websocket", "inbound", "delivery", "turn_delivery", "owner_gate", "owner_delivery", "expiry",
		"team_recovery", "mapping_recovery", "readiness":
		return value
	default:
		return "unknown"
	}
}

func normalizeTeamOperation(value string) string {
	switch value {
	case "catalog", "create", "readback", "recovery", "mapping_recovery":
		return value
	default:
		return "unknown"
	}
}

func normalizeInbound(value string) string {
	switch value {
	case "POST", "SLASH", "ACTION", "DIALOG", "REACTION",
		"CHANNEL_DELETE", "CHANNEL_RESTORE", "THREAD_DELETE", "THREAD_RESTORE":
		return value
	default:
		return "unknown"
	}
}

func normalizeDelivery(value string) string {
	switch value {
	case "RUN", "STATUS", "INCIDENT", "OWNER_DECISION", "ARTIFACT":
		return value
	default:
		return "unknown"
	}
}

func normalizeOutcome(value string) string {
	switch value {
	case "success", "failure", "idle", "ignored", "replay", "retry", "dead_letter", "ambiguous":
		return value
	default:
		return "unknown"
	}
}
