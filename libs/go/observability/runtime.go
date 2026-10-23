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

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	// defaultTraceRatio supplies the root-trace sampling ratio when the environment provides no override.
	// defaultTraceRatio задаёт долю выборки корневых трасс при отсутствии переопределения в окружении.
	defaultTraceRatio = 0.1
	// maxCAFileBytes limits the mounted OTLP trust bundle read through the secure-file boundary.
	// maxCAFileBytes ограничивает чтение смонтированного OTLP-набора доверия через границу безопасных файлов.
	maxCAFileBytes = 1 << 20
	// maxDSNFileBytes bounds the mounted private Sentry DSN before parsing.
	// maxDSNFileBytes ограничивает смонтированный частный Sentry DSN до разбора.
	maxDSNFileBytes = 16 << 10
)

// RuntimeConfig carries service identity, optional SDK disablement and the pinned OTLP and Sentry configuration; DSN material must not be logged.
// RuntimeConfig содержит идентичность сервиса, необязательное отключение SDK и закреплённые настройки OTLP и Sentry; DSN нельзя логировать.
type RuntimeConfig struct {
	Disabled           bool
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

// Runtime owns the service tracer, optional SDK provider and isolated Sentry client, with explicit caller-controlled shutdown methods.
// Runtime владеет tracer сервиса, необязательным SDK-провайдером и отдельным Sentry-клиентом с явными методами завершения под управлением вызывающего.
type Runtime struct {
	serviceName string
	tracer      trace.Tracer
	provider    *sdktrace.TracerProvider
	sentry      *sentry.Client
}

// RuntimeConfigFromEnv validates service metadata and, unless disabled, reads bounded DSN material, sampler settings and exact exporter pins from the designated environment keys.
// RuntimeConfigFromEnv проверяет метаданные сервиса и при включённом SDK читает ограниченный DSN, настройки выборки и точные привязки экспорта из назначенных ключей окружения.
func RuntimeConfigFromEnv(serviceName, serviceVersion string) (RuntimeConfig, error) {
	config := RuntimeConfig{
		Disabled:       strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true"),
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    strings.TrimSpace(os.Getenv("DEPLOYMENT_ENVIRONMENT")),
	}
	if config.Disabled {
		if err := validateRuntimeConfig(config); err != nil {
			return RuntimeConfig{}, err
		}
		return config, nil
	}

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
	config.OTLPEndpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	config.OTLPTLSServerName = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TLS_SERVER_NAME"))
	config.OTLPCAFile = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_CA_FILE"))
	config.TraceSampleRatio = ratio
	config.SentryDSN = strings.TrimSpace(sentryDSN)
	config.SentryExpectedHost = strings.TrimSpace(os.Getenv("SENTRY_EXPECTED_HOST"))
	if err := validateRuntimeConfig(config); err != nil {
		return RuntimeConfig{}, err
	}
	return config, nil
}

// NewRuntime validates configuration and installs global propagation and tracing, choosing no-op mode or a TLS-pinned OTLP exporter plus isolated Sentry client with bounded cleanup on Sentry failure.
// NewRuntime проверяет конфигурацию и устанавливает глобальные распространение и трассировку, выбирая no-op либо закреплённый TLS OTLP-exporter с отдельным Sentry-клиентом и ограниченной очисткой при отказе Sentry.
func NewRuntime(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
	if err := validateRuntimeConfig(config); err != nil {
		return nil, err
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	if config.Disabled {
		provider := nooptrace.NewTracerProvider()
		otel.SetTracerProvider(provider)
		return &Runtime{
			serviceName: config.ServiceName,
			tracer:      provider.Tracer(config.ServiceName),
		}, nil
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
	return &Runtime{
		serviceName: config.ServiceName,
		tracer:      provider.Tracer(config.ServiceName),
		provider:    provider,
		sentry:      sentryClient,
	}, nil
}

// Logger returns a JSON slog logger that adds valid trace and span IDs to records without replacing the process-global default logger.
// Logger возвращает JSON slog-логгер с допустимыми trace и span ID в записях без замены глобального логгера процесса.
func (runtime *Runtime) Logger(writer io.Writer) *slog.Logger {
	return slog.New(&traceHandler{
		delegate: slog.NewJSONHandler(writer, &slog.HandlerOptions{}),
	})
}

// UnaryServerInterceptor copies the operation registry, traces the returned unary result and reports unexpected gRPC codes through the common reporter while preserving response and error.
// UnaryServerInterceptor копирует реестр операций, трассирует возвращённый unary-результат и сообщает неожиданные gRPC-коды общему reporter, сохраняя ответ и ошибку.
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

// StreamServerInterceptor traces the entire handler call with the server correlation ID when available and reports a returned unexpected code once at this interceptor boundary.
// StreamServerInterceptor трассирует весь вызов обработчика с серверным ID корреляции при наличии и сообщает возвращённый неожиданный код один раз на границе этого interceptor.
func (runtime *Runtime) StreamServerInterceptor(
	allowedMethods map[string]string,
) grpc.StreamServerInterceptor {
	normalized := make(map[string]string, len(allowedMethods))
	for method, operation := range allowedMethods {
		normalized[method] = operation
	}
	return func(
		service any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		operation := unknownMethod
		if allowed, ok := normalized[info.FullMethod]; ok {
			operation = allowed
		}
		attributes := []attribute.KeyValue{attribute.String("rpc.system", "grpc")}
		if correlationID := CorrelationID(stream.Context()); correlationID != "" {
			attributes = append(attributes, attribute.String("correlation_id", correlationID))
		}
		traceCtx, span := runtime.tracer.Start(
			stream.Context(),
			operation,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attributes...),
		)
		err := handler(service, &contextServerStream{ServerStream: stream, ctx: traceCtx})
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

// UnaryClientInterceptor traces an invocation under a registered operation or unknown fallback, records returned errors and reports unexpected codes without changing call options or results.
// UnaryClientInterceptor трассирует вызов под зарегистрированной операцией либо unknown, записывает возвращённые ошибки и сообщает неожиданные коды без изменения параметров и результата.
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

// CaptureException submits the supplied error to this runtime's Sentry client with service, trace, span and correlation tags; callers remain responsible for supplying safe errors.
// CaptureException отправляет переданную ошибку Sentry-клиенту этого runtime с метками сервиса, trace, span и корреляции; безопасность текста ошибки обеспечивает вызывающий.
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
	if correlationID := CorrelationID(ctx); correlationID != "" {
		scope.SetTag("correlation_id", correlationID)
	}
	runtime.sentry.CaptureException(err, &sentry.EventHint{Context: ctx}, scope)
}

// ShutdownTracing delegates SDK shutdown to the caller's bounded context and is a no-op for nil or disabled runtimes; it creates no replacement context.
// ShutdownTracing передаёт завершение SDK ограниченному контексту вызывающего и ничего не делает для nil или отключённого runtime; новый контекст не создаётся.
func (runtime *Runtime) ShutdownTracing(ctx context.Context) error {
	if runtime == nil || runtime.provider == nil {
		return nil
	}
	return runtime.provider.Shutdown(ctx)
}

// FlushSentry flushes the isolated client using its independently supplied context, treats an absent client as a no-op and reports an exhausted flush budget.
// FlushSentry сбрасывает очередь отдельного клиента по независимо переданному контексту, ничего не делает при отсутствии клиента и сообщает об исчерпании бюджета.
func (runtime *Runtime) FlushSentry(ctx context.Context) error {
	if runtime == nil || runtime.sentry == nil {
		return nil
	}
	if !runtime.sentry.FlushWithContext(ctx) {
		return errors.New("sentry flush deadline exceeded")
	}
	return nil
}

// validateRuntimeConfig checks service identity and sampler bounds, then enforces enabled OTLP service-host, port, TLS-name, CA-path and exact HTTPS Sentry-host constraints.
// validateRuntimeConfig проверяет идентичность сервиса и границы выборки, затем ограничения включённых OTLP-узла, порта, TLS-имени, CA-пути и точного HTTPS-узла Sentry.
func validateRuntimeConfig(config RuntimeConfig) error {
	if config.ServiceName == "" ||
		config.ServiceVersion == "" ||
		config.Environment == "" ||
		config.TraceSampleRatio < 0 ||
		config.TraceSampleRatio > 1 {
		return errors.New("telemetry service identity is invalid")
	}
	if config.Disabled {
		return nil
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

// loadRuntimeCertificatePool reads a bounded trust file through securefile and requires at least one parseable PEM certificate without falling back to system roots.
// loadRuntimeCertificatePool читает ограниченный файл доверия через securefile и требует хотя бы один разбираемый PEM-сертификат без запасного перехода к системным корням.
func loadRuntimeCertificatePool(path string) (*x509.CertPool, error) {
	raw, err := securefile.Read(path, maxCAFileBytes)
	if err != nil {
		return nil, errors.New("OTLP CA file is unsafe")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse OTLP CA file")
	}
	return pool, nil
}

// readBoundedRuntimeFile requires an absolute secure-file path and caller byte limit, returning only a fixed label-based diagnostic rather than file contents on failure.
// readBoundedRuntimeFile требует абсолютный безопасный путь и байтовый лимит вызывающего, возвращая при отказе лишь фиксированную диагностику по метке, а не содержимое файла.
func readBoundedRuntimeFile(path string, maximum int64, label string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s path must be absolute", label)
	}
	raw, err := securefile.Read(path, maximum)
	if err != nil {
		return "", fmt.Errorf("%s file is unsafe", label)
	}
	return string(raw), nil
}

// traceHandler decorates an existing slog handler with valid trace identity while preserving its filtering, attributes and grouping behavior.
// traceHandler дополняет существующий slog-handler допустимой идентичностью трассы, сохраняя фильтрацию, атрибуты и группировку.
type traceHandler struct {
	delegate slog.Handler
}

// Enabled delegates level and context filtering without enabling records the underlying handler would discard.
// Enabled передаёт фильтрацию уровня и контекста, не включая записи, которые исходный handler отбросил бы.
func (handler *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.delegate.Enabled(ctx, level)
}

// Handle appends trace and span IDs only for a valid span context and passes the record to the delegate without inspecting application payloads.
// Handle добавляет trace и span ID лишь при допустимом контексте span и передаёт запись исходному handler без анализа прикладных данных.
func (handler *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return handler.delegate.Handle(ctx, record)
}

// WithAttrs wraps the delegate's derived attribute handler so later records retain trace decoration.
// WithAttrs оборачивает производный handler атрибутов, чтобы последующие записи сохраняли дополнение трассировкой.
func (handler *traceHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return &traceHandler{delegate: handler.delegate.WithAttrs(attributes)}
}

// WithGroup wraps the delegate's named group handler without dropping trace enrichment for future records.
// WithGroup оборачивает именованную группу исходного handler, не теряя дополнение трассировкой будущих записей.
func (handler *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{delegate: handler.delegate.WithGroup(name)}
}
