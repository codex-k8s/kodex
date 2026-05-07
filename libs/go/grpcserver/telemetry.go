package grpcserver

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
)

// DefaultTextMapPropagator defines the platform gRPC propagation baseline.
func DefaultTextMapPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// OpenTelemetryServerHandler builds the shared gRPC stats handler for traces and RPC metrics.
func OpenTelemetryServerHandler(
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	propagator propagation.TextMapPropagator,
) stats.Handler {
	if tracerProvider == nil {
		tracerProvider = otel.GetTracerProvider()
	}
	if meterProvider == nil {
		meterProvider = otel.GetMeterProvider()
	}
	if propagator == nil {
		propagator = DefaultTextMapPropagator()
	}
	return otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(tracerProvider),
		otelgrpc.WithMeterProvider(meterProvider),
		otelgrpc.WithPropagators(propagator),
	)
}
