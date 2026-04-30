package grpcserver

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

const defaultMetricNamespace = "kodex"

// MetricsConfig describes Prometheus metric names for one gRPC service boundary.
type MetricsConfig struct {
	Namespace   string
	Subsystem   string
	ServiceName string
	Buckets     []float64
}

// Metrics contains gRPC boundary metrics registered in Prometheus.
type Metrics struct {
	activeRPCs         *prometheus.GaugeVec
	requestsTotal      *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	inFlightRejections *prometheus.CounterVec
}

// NewMetrics registers gRPC boundary metrics in the provided registry.
func NewMetrics(registerer prometheus.Registerer, cfg MetricsConfig) (*Metrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	cfg = cfg.withDefaults()
	if cfg.Subsystem == "" {
		return nil, fmt.Errorf("grpc metrics subsystem is required")
	}
	metrics := &Metrics{}
	var err error
	metrics.activeRPCs, err = registerCollector(registerer, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "active_requests",
		Help:      fmt.Sprintf("Current number of active %s gRPC requests.", cfg.ServiceName),
	}, []string{"method"}))
	if err != nil {
		return nil, fmt.Errorf("register active gRPC requests metric: %w", err)
	}
	metrics.requestsTotal, err = registerCollector(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "requests_total",
		Help:      fmt.Sprintf("Total number of %s gRPC requests by method and code.", cfg.ServiceName),
	}, []string{"method", "code"}))
	if err != nil {
		return nil, fmt.Errorf("register total gRPC requests metric: %w", err)
	}
	metrics.requestDuration, err = registerCollector(registerer, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "request_duration_seconds",
		Help:      fmt.Sprintf("%s gRPC request duration by method and code.", cfg.ServiceName),
		Buckets:   cfg.Buckets,
	}, []string{"method", "code"}))
	if err != nil {
		return nil, fmt.Errorf("register gRPC request duration metric: %w", err)
	}
	metrics.inFlightRejections, err = registerCollector(registerer, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "in_flight_rejections_total",
		Help:      fmt.Sprintf("Total number of %s gRPC requests rejected by in-flight limit.", cfg.ServiceName),
	}, []string{"method"}))
	if err != nil {
		return nil, fmt.Errorf("register gRPC in-flight rejection metric: %w", err)
	}
	return metrics, nil
}

func (cfg MetricsConfig) withDefaults() MetricsConfig {
	if cfg.Namespace == "" {
		cfg.Namespace = defaultMetricNamespace
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "service"
	}
	if len(cfg.Buckets) == 0 {
		cfg.Buckets = prometheus.DefBuckets
	}
	return cfg
}

func (metrics *Metrics) recordInFlightRejection(method string) {
	if metrics == nil {
		return
	}
	metrics.inFlightRejections.WithLabelValues(method).Inc()
}

func registerCollector[T prometheus.Collector](registerer prometheus.Registerer, collector T) (T, error) {
	if err := registerer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(T)
			if ok {
				return existing, nil
			}
		}
		var zero T
		return zero, err
	}
	return collector, nil
}
