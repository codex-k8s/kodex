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

// unknownMethod is the single fallback operation label for any unregistered RPC method.
// unknownMethod — единственная запасная метка операции для незарегистрированного RPC-метода.
const unknownMethod = "unknown"

// Metrics owns an isolated registry and a copied operation allowlist for request, duration, streaming in-flight and readiness collectors.
// Metrics владеет изолированным реестром и копией разрешённых операций для счётчиков запросов, длительности, активных потоков и готовности.
type Metrics struct {
	registry       *prometheus.Registry
	allowedMethods map[string]string
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	streamInFlight *prometheus.GaugeVec
	readiness      *prometheus.GaugeVec
}

// NewMetrics copies the caller's closed method map and registers technical, build, process and Go collectors without using the global Prometheus registry.
// NewMetrics копирует закрытый словарь методов вызывающего и регистрирует технические, сборочные, процессные и Go-метрики без глобального Prometheus-реестра.
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
	streamInFlight := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kodex",
		Subsystem: serviceName,
		Name:      "grpc_streams_in_flight",
		Help:      "Current number of in-flight gRPC streams.",
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
		streamInFlight,
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
		streamInFlight: streamInFlight,
		readiness:      readiness,
	}
}

// StreamServerInterceptor increments in-flight before invoking the handler, releases it on unwinding and records duration and code for returned outcomes, including failures before the first message.
// StreamServerInterceptor увеличивает активность до вызова обработчика, освобождает её при выходе и учитывает длительность и код возвращённого результата, включая отказы до первого сообщения.
func (metrics *Metrics) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(
		service any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		started := time.Now()
		operation := metrics.operation(info.FullMethod)
		metrics.streamInFlight.WithLabelValues(operation).Inc()
		defer metrics.streamInFlight.WithLabelValues(operation).Dec()
		err := handler(service, stream)
		metrics.duration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
		metrics.requests.WithLabelValues(operation, status.Code(err).String()).Inc()
		return err
	}
}

// PrometheusHandler serves only this instance's registry using the standard Prometheus HTTP exposition handler.
// PrometheusHandler публикует только реестр этого экземпляра стандартным HTTP-обработчиком Prometheus.
func (metrics *Metrics) PrometheusHandler() http.Handler {
	return promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{})
}

// Register adds service-owned collectors in order and returns the first registration error; collectors already registered earlier in the call remain installed.
// Register добавляет принадлежащие сервису метрики по порядку и возвращает первую ошибку; ранее зарегистрированные в этом вызове метрики остаются установленными.
func (metrics *Metrics) Register(collectors ...prometheus.Collector) error {
	for _, collector := range collectors {
		if err := metrics.registry.Register(collector); err != nil {
			return err
		}
	}
	return nil
}

// UnaryServerInterceptor records elapsed time and canonical returned status under a registered operation or the single unknown fallback without changing the response.
// UnaryServerInterceptor учитывает длительность и канонический код результата под зарегистрированной операцией либо единственным unknown, не меняя ответ.
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

// SetReady replaces the readiness series with one current true or false label rather than accumulating historical states.
// SetReady заменяет серию готовности одним текущим true или false, не накапливая прошлые состояния.
func (metrics *Metrics) SetReady(ready bool) {
	metrics.readiness.Reset()
	metrics.readiness.WithLabelValues(strconv.FormatBool(ready)).Set(1)
}

// operation maps a full method through the copied allowlist and never exposes arbitrary incoming names as metric labels.
// operation сопоставляет полный метод с копией разрешённого словаря и не выдаёт произвольные входные имена как метки метрик.
func (metrics *Metrics) operation(fullMethod string) string {
	if operation, ok := metrics.allowedMethods[fullMethod]; ok {
		return operation
	}
	return unknownMethod
}
