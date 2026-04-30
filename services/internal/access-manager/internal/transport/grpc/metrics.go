package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

const (
	metricNamespace = "kodex"
	metricSubsystem = "access_manager_grpc"
)

// ServerMetrics contains gRPC boundary metrics registered in Prometheus.
type ServerMetrics struct {
	activeRPCs         *prometheus.GaugeVec
	requestsTotal      *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	inFlightRejections *prometheus.CounterVec
}

// NewServerMetrics registers access-manager gRPC metrics in the provided registry.
func NewServerMetrics(registerer prometheus.Registerer) (*ServerMetrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &ServerMetrics{
		activeRPCs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: metricSubsystem,
			Name:      "active_requests",
			Help:      "Current number of active access-manager gRPC requests.",
		}, []string{"method"}),
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Subsystem: metricSubsystem,
			Name:      "requests_total",
			Help:      "Total number of access-manager gRPC requests by method and code.",
		}, []string{"method", "code"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricNamespace,
			Subsystem: metricSubsystem,
			Name:      "request_duration_seconds",
			Help:      "Access-manager gRPC request duration by method and code.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "code"}),
		inFlightRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Subsystem: metricSubsystem,
			Name:      "in_flight_rejections_total",
			Help:      "Total number of access-manager gRPC requests rejected by in-flight limit.",
		}, []string{"method"}),
	}
	if err := registerCollector(registerer, metrics.activeRPCs); err != nil {
		return nil, fmt.Errorf("register active gRPC requests metric: %w", err)
	}
	if err := registerCollector(registerer, metrics.requestsTotal); err != nil {
		return nil, fmt.Errorf("register total gRPC requests metric: %w", err)
	}
	if err := registerCollector(registerer, metrics.requestDuration); err != nil {
		return nil, fmt.Errorf("register gRPC request duration metric: %w", err)
	}
	if err := registerCollector(registerer, metrics.inFlightRejections); err != nil {
		return nil, fmt.Errorf("register gRPC in-flight rejection metric: %w", err)
	}
	return metrics, nil
}

// UnaryMetricsInterceptor records active RPCs, terminal codes and durations.
func UnaryMetricsInterceptor(metrics *ServerMetrics) grpcruntime.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
		if metrics == nil {
			return handler(ctx, req)
		}
		startedAt := time.Now()
		metrics.activeRPCs.WithLabelValues(info.FullMethod).Inc()
		defer metrics.activeRPCs.WithLabelValues(info.FullMethod).Dec()
		response, err := handler(ctx, req)
		code := status.Code(err).String()
		metrics.requestsTotal.WithLabelValues(info.FullMethod, code).Inc()
		metrics.requestDuration.WithLabelValues(info.FullMethod, code).Observe(time.Since(startedAt).Seconds())
		return response, err
	}
}

func (metrics *ServerMetrics) recordInFlightRejection(method string) {
	if metrics == nil {
		return
	}
	metrics.inFlightRejections.WithLabelValues(method).Inc()
}

func registerCollector(registerer prometheus.Registerer, collector prometheus.Collector) error {
	if err := registerer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			return nil
		}
		return err
	}
	return nil
}
