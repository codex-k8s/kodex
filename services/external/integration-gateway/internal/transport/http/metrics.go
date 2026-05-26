package httptransport

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type edgeSummary struct {
	RouteID           string
	Source            string
	Status            int
	Latency           time.Duration
	PayloadSizeBucket string
	RejectReason      string
}

var (
	edgeMetricsOnce     sync.Once
	edgeMetricsInitErr  error
	edgeRequestsTotal   *prometheus.CounterVec
	edgeRequestDuration *prometheus.HistogramVec
)

func recordEdgeSummary(summary edgeSummary) {
	edgeMetricsOnce.Do(initEdgeMetrics)
	if edgeMetricsInitErr != nil {
		return
	}
	routeID := metricLabelOrUnknown(summary.RouteID)
	source := metricLabelOrUnknown(summary.Source)
	status := strconv.Itoa(summary.Status)
	payloadSizeBucket := metricLabelOrUnknown(summary.PayloadSizeBucket)
	rejectReason := metricLabelOrNone(summary.RejectReason)

	edgeRequestsTotal.WithLabelValues(routeID, source, status, payloadSizeBucket, rejectReason).Inc()
	edgeRequestDuration.WithLabelValues(routeID, source, status, rejectReason).Observe(summary.Latency.Seconds())
}

func initEdgeMetrics() {
	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kodex_integration_gateway_http_requests_total",
		Help: "Integration gateway HTTP edge requests by safe route/source/status summary.",
	}, []string{"route", "source", "status", "payload_size_bucket", "reject_reason"})
	requestsCollector, err := registerEdgeCollector(requestsTotal)
	if err != nil {
		edgeMetricsInitErr = err
		return
	}
	var ok bool
	edgeRequestsTotal, ok = requestsCollector.(*prometheus.CounterVec)
	if !ok {
		edgeMetricsInitErr = fmt.Errorf("edge counter metric type mismatch")
		return
	}
	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kodex_integration_gateway_http_request_duration_seconds",
		Help:    "Integration gateway HTTP edge request duration by safe route/source/status summary.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "source", "status", "reject_reason"})
	durationCollector, err := registerEdgeCollector(requestDuration)
	if err != nil {
		edgeMetricsInitErr = err
		return
	}
	edgeRequestDuration, ok = durationCollector.(*prometheus.HistogramVec)
	if !ok {
		edgeMetricsInitErr = fmt.Errorf("edge histogram metric type mismatch")
		return
	}
}

func registerEdgeCollector(collector prometheus.Collector) (prometheus.Collector, error) {
	if err := prometheus.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			return alreadyRegistered.ExistingCollector, nil
		}
		return nil, err
	}
	return collector, nil
}

func metricLabelOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func metricLabelOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
