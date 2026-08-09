package observability

import (
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	websockets   *prometheus.GaugeVec
	snapshots    *prometheus.CounterVec
}

func Route(path string) string {
	switch {
	case path == "/api/v1/session":
		return "session"
	case path == "/api/v1/projects":
		return "projects"
	case strings.HasPrefix(path, "/api/v1/access-resources"):
		return "access"
	case strings.HasPrefix(path, "/api/v1/role-image-recipes") || strings.HasPrefix(path, "/api/v1/image-builds"):
		return "role_images"
	case strings.HasPrefix(path, "/api/v1/schedules") || path == "/api/v1/schedule-selectors":
		return "schedules"
	case strings.HasPrefix(path, "/api/v1/owner-gates"):
		return "owner_gates"
	case strings.HasPrefix(path, "/api/v1/backups") || strings.HasPrefix(path, "/api/v1/restore-operations") || strings.HasPrefix(path, "/api/v1/workspace-backups") || strings.HasPrefix(path, "/api/v1/workspace-restores"):
		return "backups"
	case strings.HasPrefix(path, "/api/v1/runs"):
		return "runs"
	case strings.HasPrefix(path, "/api/v1/audit"):
		return "audit"
	case strings.HasPrefix(path, "/api/v1/incidents"):
		return "incidents"
	case path == "/api/v1/configuration-changes" || path == "/api/v1/configuration-diff" || strings.HasPrefix(path, "/api/v1/configuration-source/"):
		return "configuration_changes"
	case path == "/api/v1/diagnostics" || path == "/api/v1/health-series":
		return "diagnostics"
	case strings.HasPrefix(path, "/api/v1/mattermost/"):
		return "workspaces"
	case strings.HasPrefix(path, "/api/v1/role-definitions"):
		return "role_definitions"
	case strings.HasPrefix(path, "/api/v1/agents") || strings.HasPrefix(path, "/api/v1/agent-assignments"):
		return "agents"
	case strings.HasPrefix(path, "/api/v1/instruction-sets"):
		return "instructions"
	case strings.HasPrefix(path, "/api/v1/providers") || strings.HasPrefix(path, "/api/v1/provider-"):
		return "providers"
	case strings.HasPrefix(path, "/api/v1/integration-"):
		return "integrations"
	case path == "/api/v1/realtime":
		return "realtime"
	case path == "/api/v1/resources" || strings.HasPrefix(path, "/api/v1/resources/"):
		return "resources"
	case path == "/livez" || path == "/readyz" || path == "/metrics":
		return "technical"
	default:
		return "unknown"
	}
}

func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	metrics := &Metrics{
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "control_api_gateway", Name: "http_requests_total",
			Help: "Total number of completed control API HTTP requests.",
		}, []string{"route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mattercodex", Subsystem: "control_api_gateway", Name: "http_request_duration_seconds",
			Help: "Duration of completed control API HTTP requests in seconds.", Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		websockets: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "mattercodex", Subsystem: "control_api_gateway", Name: "websocket_connections",
			Help: "Current bounded control API WebSocket connections.",
		}, []string{"state"}),
		snapshots: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mattercodex", Subsystem: "control_api_gateway", Name: "websocket_snapshots_total",
			Help: "Total number of control API WebSocket snapshot outcomes.",
		}, []string{"channel", "outcome"}),
	}
	if err := register(metrics.httpRequests, metrics.httpDuration, metrics.websockets, metrics.snapshots); err != nil {
		return nil, err
	}
	return metrics, nil
}

func (metrics *Metrics) ObserveHTTP(route string, status int, started time.Time) {
	metrics.httpRequests.WithLabelValues(normalizeRoute(route), normalizeStatus(status)).Inc()
	metrics.httpDuration.WithLabelValues(normalizeRoute(route)).Observe(time.Since(started).Seconds())
}

func (metrics *Metrics) SetWebSockets(current int) {
	metrics.websockets.Reset()
	metrics.websockets.WithLabelValues("open").Set(float64(current))
}

func (metrics *Metrics) ObserveSnapshot(channel, outcome string) {
	metrics.snapshots.WithLabelValues(normalizeChannel(channel), normalizeOutcome(outcome)).Inc()
}

func normalizeRoute(value string) string {
	switch value {
	case "session", "projects", "resources", "access", "role_images", "schedules", "owner_gates", "backups", "runs", "audit", "incidents", "configuration_changes", "diagnostics", "workspaces", "role_definitions", "agents", "instructions", "providers", "integrations", "realtime", "technical":
		return value
	default:
		return "unknown"
	}
}

func normalizeStatus(value int) string {
	switch value {
	case 101, 200, 201, 202, 204, 400, 401, 403, 404, 405, 409, 412, 413, 415, 429, 500, 503:
		return strconv.Itoa(value)
	default:
		return "unknown"
	}
}

func normalizeChannel(value string) string {
	switch value {
	case "RUNS", "INCIDENTS", "RESOURCES", "CONFIGURATION_CHANGES", "WORKSPACE_TEAMS", "PROVIDERS", "INTEGRATIONS", "APPROVALS", "BACKUPS", "HEALTH":
		return value
	default:
		return "UNKNOWN"
	}
}

func normalizeOutcome(value string) string {
	switch value {
	case "success", "failure":
		return value
	default:
		return "unknown"
	}
}
