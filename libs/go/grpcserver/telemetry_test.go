package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const traceCaptureMethod = "/kodex.test.v1.TraceCapture/Ping"

func TestNewServerExtractsOpenTelemetryTraceContext(t *testing.T) {
	cfg := validConfig()
	cfg.AuthRequired = false

	tracerProvider := sdktrace.NewTracerProvider()
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
	})

	server, err := NewServer(cfg, Dependencies{
		TracerProvider: tracerProvider,
	})
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}

	service := &traceCaptureService{seen: make(chan traceCapture, 1)}
	registerTraceCaptureService(server, service)

	listener := bufconn.Listen(1024 * 1024)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := server.Serve(listener); err != nil && err != grpcruntime.ErrServerStopped {
			t.Errorf("Serve(): %v", err)
		}
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serveDone
	})

	conn, err := grpcruntime.NewClient(
		"passthrough:///bufnet",
		grpcruntime.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpcruntime.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	wantTraceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    wantTraceID,
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	carrier := propagation.MapCarrier{}
	DefaultTextMapPropagator().Inject(trace.ContextWithRemoteSpanContext(context.Background(), spanContext), carrier)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.New(map[string]string(carrier)))

	if err := conn.Invoke(ctx, traceCaptureMethod, &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatalf("Invoke(): %v", err)
	}

	select {
	case got := <-service.seen:
		if got.traceID != wantTraceID {
			t.Fatalf("trace id = %s, want %s", got.traceID, wantTraceID)
		}
		if gotTraceAttr, ok := logAttrValue(got.logAttrs, "trace_id"); !ok || gotTraceAttr != wantTraceID.String() {
			t.Fatalf("log trace_id attr = %v, %t, want %s", gotTraceAttr, ok, wantTraceID)
		}
	case <-time.After(time.Second):
		t.Fatal("trace id was not captured")
	}
}

type traceCaptureServiceServer interface {
	mustEmbedTraceCaptureServiceServer()
}

type traceCaptureService struct {
	seen chan traceCapture
}

type traceCapture struct {
	traceID  trace.TraceID
	logAttrs []any
}

func (*traceCaptureService) mustEmbedTraceCaptureServiceServer() {}

func registerTraceCaptureService(server *grpcruntime.Server, service *traceCaptureService) {
	server.RegisterService(&grpcruntime.ServiceDesc{
		ServiceName: "kodex.test.v1.TraceCapture",
		HandlerType: (*traceCaptureServiceServer)(nil),
		Methods: []grpcruntime.MethodDesc{
			{
				MethodName: "Ping",
				Handler:    traceCaptureUnaryHandler,
			},
		},
	}, service)
}

func traceCaptureUnaryHandler(
	srv any,
	ctx context.Context,
	dec func(any) error,
	interceptor grpcruntime.UnaryServerInterceptor,
) (any, error) {
	request := new(emptypb.Empty)
	if err := dec(request); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, _ any) (any, error) {
		service := srv.(*traceCaptureService)
		service.seen <- traceCapture{
			traceID:  trace.SpanContextFromContext(ctx).TraceID(),
			logAttrs: LogAttrsFromContext(ctx),
		}
		return &emptypb.Empty{}, nil
	}
	if interceptor == nil {
		return handler(ctx, request)
	}
	return interceptor(ctx, request, &grpcruntime.UnaryServerInfo{
		Server:     srv,
		FullMethod: traceCaptureMethod,
	}, handler)
}

func logAttrValue(attrs []any, key string) (any, bool) {
	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i] == key {
			return attrs[i+1], true
		}
	}
	return nil, false
}
