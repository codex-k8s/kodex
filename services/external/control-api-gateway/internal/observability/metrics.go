package observability

import (
	"bufio"
	"errors"
	"net"
	"net/http"
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

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(raw []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(raw)
}

func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("HTTP connection does not support hijacking")
	}
	writer.status = http.StatusSwitchingProtocols
	return hijacker.Hijack()
}

func (writer *statusWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (metrics *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		captured := &statusWriter{ResponseWriter: writer}
		next.ServeHTTP(captured, request)
		status := captured.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.ObserveHTTP(route(request.URL.Path), status, started)
	})
}

func route(path string) string {
	switch {
	case path == "/api/v1/session":
		return "session"
	case path == "/api/v1/projects":
		return "projects"
	case path == "/api/v1/access-resources":
		return "access"
	case path == "/api/v1/runs":
		return "runs"
	case path == "/api/v1/audit":
		return "audit"
	case path == "/api/v1/incidents":
		return "incidents"
	case path == "/api/v1/configuration-changes":
		return "configuration_changes"
	case path == "/api/v1/diagnostics":
		return "diagnostics"
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
	case "session", "projects", "resources", "access", "runs", "audit", "incidents", "configuration_changes", "diagnostics", "realtime", "technical":
		return value
	default:
		return "unknown"
	}
}

func normalizeStatus(value int) string {
	switch value {
	case 101, 200, 201, 204, 400, 401, 403, 404, 405, 409, 412, 413, 415, 429, 500, 503:
		return strconv.Itoa(value)
	default:
		return "unknown"
	}
}

func normalizeChannel(value string) string {
	switch value {
	case "RUNS", "INCIDENTS", "RESOURCES", "CONFIGURATION_CHANGES":
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
