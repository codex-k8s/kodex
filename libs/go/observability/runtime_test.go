package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestRuntimeConfigRejectsUnpinnedDestinations(t *testing.T) {
	t.Parallel()
	valid := RuntimeConfig{
		ServiceName: "authority", ServiceVersion: "test", Environment: "staging",
		OTLPEndpoint:      "otel-collector.observability.svc:4317",
		OTLPTLSServerName: "otel-collector.observability.svc.cluster.local",
		OTLPCAFile:        "/var/run/config/otel/ca.pem", TraceSampleRatio: 0.1,
		SentryDSN:          "https://public-key@sentry-relay.observability.svc:8443/42",
		SentryExpectedHost: "sentry-relay.observability.svc:8443",
	}
	if err := validateRuntimeConfig(valid); err != nil {
		t.Fatalf("valid telemetry boundary rejected: %v", err)
	}
	for name, mutate := range map[string]func(*RuntimeConfig){
		"OTLP IP": func(value *RuntimeConfig) {
			value.OTLPEndpoint = "10.0.0.1:4317"
		},
		"wrong OTLP port": func(value *RuntimeConfig) {
			value.OTLPEndpoint = "otel-collector.observability.svc:4318"
		},
		"plaintext Sentry": func(value *RuntimeConfig) {
			value.SentryDSN = "http://public-key@sentry-relay.observability.svc:8443/42"
		},
		"wrong Sentry host": func(value *RuntimeConfig) {
			value.SentryDSN = "https://public-key@external.example.test/42"
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := validateRuntimeConfig(candidate); err == nil {
				t.Fatal("unpinned telemetry destination was accepted")
			}
		})
	}
}

func TestTraceHandlerAddsBoundedTraceIdentity(t *testing.T) {
	t.Parallel()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	ctx, span := provider.Tracer("test").Start(context.Background(), "operation")
	defer span.End()
	var output bytes.Buffer
	logger := slog.New(&traceHandler{
		delegate: slog.NewJSONHandler(&output, &slog.HandlerOptions{}),
	})
	logger.InfoContext(ctx, "message")
	line := output.String()
	if !strings.Contains(line, `"trace_id":"`) ||
		!strings.Contains(line, `"span_id":"`) {
		t.Fatalf("trace-aware slog record is incomplete: %s", line)
	}
}
