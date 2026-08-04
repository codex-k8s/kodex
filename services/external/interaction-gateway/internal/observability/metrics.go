// Package observability содержит только business/transport метрики gateway.
package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	workerCycles *prometheus.CounterVec
	inbound      *prometheus.CounterVec
	deliveries   *prometheus.CounterVec
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
	}
	if err := register(metrics.httpRequests, metrics.httpDuration, metrics.workerCycles, metrics.inbound, metrics.deliveries); err != nil {
		return nil, err
	}
	return metrics, nil
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
	case "slash", "action", "dialog", "readback", "technical":
		return value
	default:
		return "unknown"
	}
}

func normalizeWorker(value string) string {
	switch value {
	case "websocket", "inbound", "delivery", "owner_gate", "expiry", "readiness":
		return value
	default:
		return "unknown"
	}
}

func normalizeInbound(value string) string {
	switch value {
	case "POST", "SLASH", "ACTION", "DIALOG", "REACTION":
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
	case "success", "failure", "idle", "ignored", "replay", "retry", "dead_letter":
		return value
	default:
		return "unknown"
	}
}
