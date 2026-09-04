package observability

import (
	"context"
	"regexp"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *testServerStream) Context() context.Context { return stream.ctx }

func TestStreamCorrelationIsServerOwnedAndStable(t *testing.T) {
	interceptor := StreamCorrelationServerInterceptor()
	stream := &testServerStream{ctx: t.Context()}
	var first, second string
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Upload"}, func(_ any, wrapped grpc.ServerStream) error {
		first = CorrelationID(wrapped.Context())
		second = CorrelationID(wrapped.Context())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(first) {
		t.Fatalf("неканонический stream correlation: %q %q", first, second)
	}
}

func TestStreamMetricsObserveEarlyFailureAndReleaseInflight(t *testing.T) {
	const method = "/test.Service/Upload"
	metrics := NewMetrics("test_stream", "test", map[string]string{method: "upload"})
	interceptor := metrics.StreamServerInterceptor()
	stream := &testServerStream{ctx: t.Context()}
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: method}, func(any, grpc.ServerStream) error {
		if value := testutil.ToFloat64(metrics.streamInFlight.WithLabelValues("upload")); value != 1 {
			t.Fatalf("in-flight=%v, ожидался 1", value)
		}
		return status.Error(codes.Unauthenticated, "rejected")
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code=%s", status.Code(err))
	}
	if value := testutil.ToFloat64(metrics.streamInFlight.WithLabelValues("upload")); value != 0 {
		t.Fatalf("in-flight после завершения=%v", value)
	}
	if value := testutil.ToFloat64(metrics.requests.WithLabelValues("upload", codes.Unauthenticated.String())); value != 1 {
		t.Fatalf("completed=%v", value)
	}
	if err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: method}, func(any, grpc.ServerStream) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if value := testutil.ToFloat64(metrics.requests.WithLabelValues("upload", codes.OK.String())); value != 1 {
		t.Fatalf("success=%v", value)
	}
}

func TestStreamRuntimeTracesEarlyFailureWithCorrelation(t *testing.T) {
	const method = "/test.Service/Upload"
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	runtime := &Runtime{serviceName: "test", tracer: provider.Tracer("test")}
	stream := &testServerStream{ctx: t.Context()}
	correlation := StreamCorrelationServerInterceptor()
	traceStream := runtime.StreamServerInterceptor(map[string]string{method: "upload"})
	var correlationID string
	err := correlation(nil, stream, &grpc.StreamServerInfo{FullMethod: method}, func(service any, correlated grpc.ServerStream) error {
		return traceStream(service, correlated, &grpc.StreamServerInfo{FullMethod: method}, func(_ any, traced grpc.ServerStream) error {
			correlationID = CorrelationID(traced.Context())
			return status.Error(codes.Unauthenticated, "rejected")
		})
	})
	if status.Code(err) != codes.Unauthenticated || correlationID == "" {
		t.Fatalf("ранний отказ не сохранил correlation: id=%q err=%v", correlationID, err)
	}
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Name() != "upload" {
		t.Fatalf("stream spans=%d", len(ended))
	}
	attributes := map[attribute.Key]string{}
	for _, item := range ended[0].Attributes() {
		if item.Value.Type() == attribute.STRING {
			attributes[item.Key] = item.Value.AsString()
		}
	}
	if attributes["correlation_id"] != correlationID || attributes["rpc.grpc.status_code"] != codes.Unauthenticated.String() {
		t.Fatalf("stream attributes=%v", attributes)
	}
}
