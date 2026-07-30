package observability

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	defaultTraceRatio = 0.1
	maxCAFileBytes    = 1 << 20
	maxDSNFileBytes   = 16 << 10
)

type RuntimeConfig struct {
	ServiceName        string
	ServiceVersion     string
	Environment        string
	OTLPEndpoint       string
	OTLPTLSServerName  string
	OTLPCAFile         string
	TraceSampleRatio   float64
	SentryDSN          string
	SentryExpectedHost string
}

type Runtime struct {
	serviceName string
	tracer      trace.Tracer
	provider    *sdktrace.TracerProvider
	sentry      *sentry.Client
}

func RuntimeConfigFromEnv(serviceName, serviceVersion string) (RuntimeConfig, error) {
	ratio := defaultTraceRatio
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return RuntimeConfig{}, errors.New("OTEL_TRACES_SAMPLER_ARG is invalid")
		}
		ratio = parsed
	}
	sentryDSN, err := readBoundedRuntimeFile(
		strings.TrimSpace(os.Getenv("SENTRY_DSN_FILE")),
		maxDSNFileBytes,
		"Sentry DSN",
	)
	if err != nil {
		return RuntimeConfig{}, err
	}
	config := RuntimeConfig{
		ServiceName:        serviceName,
		ServiceVersion:     serviceVersion,
		Environment:        strings.TrimSpace(os.Getenv("DEPLOYMENT_ENVIRONMENT")),
		OTLPEndpoint:       strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		OTLPTLSServerName:  strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TLS_SERVER_NAME")),
		OTLPCAFile:         strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_CA_FILE")),
		TraceSampleRatio:   ratio,
		SentryDSN:          strings.TrimSpace(sentryDSN),
		SentryExpectedHost: strings.TrimSpace(os.Getenv("SENTRY_EXPECTED_HOST")),
	}
	if err := validateRuntimeConfig(config); err != nil {
		return RuntimeConfig{}, err
	}
	return config, nil
}

func NewRuntime(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
	if err := validateRuntimeConfig(config); err != nil {
		return nil, err
	}
	certificatePool, err := loadRuntimeCertificatePool(config.OTLPCAFile)
	if err != nil {
		return nil, err
	}
	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
		otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    certificatePool,
			ServerName: config.OTLPTLSServerName,
		})),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("construct OTLP trace exporter: %w", err)
	}
	runtimeResource, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			attribute.String("deployment.environment.name", config.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("construct telemetry resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(runtimeResource),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(config.TraceSampleRatio),
		)),
	)
	sentryClient, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              config.SentryDSN,
		Release:          config.ServiceVersion,
		Environment:      config.Environment,
		AttachStacktrace: true,
		EnableTracing:    false,
	})
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(shutdownCtx)
		return nil, fmt.Errorf("construct Sentry client: %w", err)
	}
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return &Runtime{
		serviceName: config.ServiceName,
		tracer:      provider.Tracer(config.ServiceName),
		provider:    provider,
		sentry:      sentryClient,
	}, nil
}

func (runtime *Runtime) Logger(writer io.Writer) *slog.Logger {
	return slog.New(&traceHandler{
		delegate: slog.NewJSONHandler(writer, &slog.HandlerOptions{}),
	})
}

func (runtime *Runtime) UnaryServerInterceptor(
	allowedMethods map[string]string,
) grpc.UnaryServerInterceptor {
	normalized := make(map[string]string, len(allowedMethods))
	for method, operation := range allowedMethods {
		normalized[method] = operation
	}
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		operation := unknownMethod
		if allowed, ok := normalized[info.FullMethod]; ok {
			operation = allowed
		}
		traceCtx, span := runtime.tracer.Start(
			ctx,
			operation,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("rpc.system", "grpc")),
		)
		response, err := handler(traceCtx, request)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String("rpc.grpc.status_code", status.Code(err).String()))
			if grpcserver.IsUnexpectedCode(status.Code(err)) {
				runtime.CaptureException(traceCtx, err)
			}
		}
		span.End()
		return response, err
	}
}

func (runtime *Runtime) UnaryClientInterceptor(
	allowedMethods map[string]string,
) grpc.UnaryClientInterceptor {
	normalized := make(map[string]string, len(allowedMethods))
	for method, operation := range allowedMethods {
		normalized[method] = operation
	}
	return func(
		ctx context.Context,
		method string,
		request any,
		reply any,
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		operation := unknownMethod
		if allowed, ok := normalized[method]; ok {
			operation = allowed
		}
		traceCtx, span := runtime.tracer.Start(
			ctx,
			operation,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attribute.String("rpc.system", "grpc")),
		)
		err := invoker(traceCtx, method, request, reply, connection, options...)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String("rpc.grpc.status_code", status.Code(err).String()))
			if grpcserver.IsUnexpectedCode(status.Code(err)) {
				runtime.CaptureException(traceCtx, err)
			}
		}
		span.End()
		return err
	}
}

func (runtime *Runtime) CaptureException(ctx context.Context, err error) {
	if runtime == nil || runtime.sentry == nil || err == nil {
		return
	}
	scope := sentry.NewScope()
	scope.SetTag("service", runtime.serviceName)
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		scope.SetTag("trace_id", spanContext.TraceID().String())
		scope.SetTag("span_id", spanContext.SpanID().String())
	}
	runtime.sentry.CaptureException(err, &sentry.EventHint{Context: ctx}, scope)
}

func (runtime *Runtime) ShutdownTracing(ctx context.Context) error {
	if runtime == nil || runtime.provider == nil {
		return nil
	}
	return runtime.provider.Shutdown(ctx)
}

func (runtime *Runtime) FlushSentry(ctx context.Context) error {
	if runtime == nil || runtime.sentry == nil {
		return nil
	}
	if !runtime.sentry.FlushWithContext(ctx) {
		return errors.New("sentry flush deadline exceeded")
	}
	return nil
}

func validateRuntimeConfig(config RuntimeConfig) error {
	if config.ServiceName == "" ||
		config.ServiceVersion == "" ||
		config.Environment == "" ||
		config.TraceSampleRatio < 0 ||
		config.TraceSampleRatio > 1 {
		return errors.New("telemetry service identity is invalid")
	}
	host, port, err := net.SplitHostPort(config.OTLPEndpoint)
	if err != nil ||
		port != "4317" ||
		host == "" ||
		net.ParseIP(host) != nil ||
		!strings.HasSuffix(host, ".svc") ||
		config.OTLPTLSServerName == "" ||
		net.ParseIP(config.OTLPTLSServerName) != nil {
		return errors.New("exact OTLP gRPC TLS endpoint is invalid")
	}
	dsn, err := url.Parse(config.SentryDSN)
	if err != nil ||
		dsn.Scheme != "https" ||
		dsn.Host == "" ||
		dsn.Host != config.SentryExpectedHost ||
		dsn.User == nil ||
		dsn.User.Username() == "" ||
		dsn.RawQuery != "" ||
		dsn.Fragment != "" {
		return errors.New("sentry DSN is invalid")
	}
	if !filepath.IsAbs(config.OTLPCAFile) {
		return errors.New("OTLP CA path must be absolute")
	}
	return nil
}

func loadRuntimeCertificatePool(path string) (*x509.CertPool, error) {
	info, err := os.Stat(path)
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Size() <= 0 ||
		info.Size() > maxCAFileBytes ||
		info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("OTLP CA file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read OTLP CA file")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse OTLP CA file")
	}
	return pool, nil
}

func readBoundedRuntimeFile(path string, maximum int64, label string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s path must be absolute", label)
	}
	info, err := os.Stat(path)
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Size() <= 0 ||
		info.Size() > maximum ||
		info.Mode().Perm()&0o007 != 0 {
		return "", fmt.Errorf("%s file is unsafe", label)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file", label)
	}
	return string(raw), nil
}

type traceHandler struct {
	delegate slog.Handler
}

func (handler *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.delegate.Enabled(ctx, level)
}

func (handler *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return handler.delegate.Handle(ctx, record)
}

func (handler *traceHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return &traceHandler{delegate: handler.delegate.WithAttrs(attributes)}
}

func (handler *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{delegate: handler.delegate.WithGroup(name)}
}
