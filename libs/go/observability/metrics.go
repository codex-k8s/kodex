package observability

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

const unknownMethod = "unknown"

// Metrics хранит ограниченные Prometheus-метрики gRPC и готовности.
type Metrics struct {
	registry       *prometheus.Registry
	allowedMethods map[string]string
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	readiness      *prometheus.GaugeVec
}

// NewMetrics создаёт изолированный registry с закрытыми метками операций.
func NewMetrics(
	serviceName string,
	buildVersion string,
	allowedMethods map[string]string,
) *Metrics {
	normalizedMethods := make(map[string]string, len(allowedMethods))
	for fullMethod, operation := range allowedMethods {
		normalizedMethods[fullMethod] = operation
	}
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kodex",
		Subsystem: serviceName,
		Name:      "grpc_requests_total",
		Help:      "Total number of completed gRPC requests.",
	}, []string{"operation", "code"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kodex",
		Subsystem: serviceName,
		Name:      "grpc_request_duration_seconds",
		Help:      "Duration of completed gRPC requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation"})
	readiness := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kodex",
		Subsystem: serviceName,
		Name:      "readiness",
		Help:      "Readiness state of the served runtime dependency.",
	}, []string{"ready"})
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kodex",
		Subsystem: serviceName,
		Name:      "build_info",
		Help:      "Build information for the running binary.",
	}, []string{"version"})
	buildInfo.WithLabelValues(buildVersion).Set(1)
	registry.MustRegister(
		requests,
		duration,
		readiness,
		buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return &Metrics{
		registry:       registry,
		allowedMethods: normalizedMethods,
		requests:       requests,
		duration:       duration,
		readiness:      readiness,
	}
}

// PrometheusHandler возвращает HTTP handler для registry.
func (metrics *Metrics) PrometheusHandler() http.Handler {
	return promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{})
}

// Register добавляет service-owned collectors в изолированный registry.
func (metrics *Metrics) Register(collectors ...prometheus.Collector) error {
	for _, collector := range collectors {
		if err := metrics.registry.Register(collector); err != nil {
			return err
		}
	}
	return nil
}

// UnaryServerInterceptor учитывает длительность и код завершённого RPC.
func (metrics *Metrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		started := time.Now()
		response, err := handler(ctx, request)
		operation := metrics.operation(info.FullMethod)
		metrics.duration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
		metrics.requests.WithLabelValues(operation, status.Code(err).String()).Inc()
		return response, err
	}
}

// SetReady обновляет ограниченную метрику готовности.
func (metrics *Metrics) SetReady(ready bool) {
	metrics.readiness.Reset()
	metrics.readiness.WithLabelValues(strconv.FormatBool(ready)).Set(1)
}

func (metrics *Metrics) operation(fullMethod string) string {
	if operation, ok := metrics.allowedMethods[fullMethod]; ok {
		return operation
	}
	return unknownMethod
}
