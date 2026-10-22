package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// appTelemetry wraps the shared observability runtime with independent once-only tracing shutdown and Sentry flush, caching each result. A failed first cleanup remains cached rather than implicitly retried by later startup/normal-shutdown paths.
// appTelemetry оборачивает общий runtime наблюдаемости независимыми однократными завершением tracing и flush Sentry, сохраняя каждый результат. Первый неудачный cleanup остаётся сохранённым вместо неявного повтора поздними путями стартового или штатного завершения.
type appTelemetry struct {
	*observability.Runtime
	traceOnce  sync.Once
	sentryOnce sync.Once
	traceErr   error
	sentryErr  error
}

// instrumentPGX replaces the pool connection tracer with otelpgx configured to omit connection details and SQL-statement attributes and to use one service-name-based span name. It assumes an initialized nonnil pool/connection config and does not open a connection or claim that every external error is redacted.
// instrumentPGX заменяет tracer соединений пула на otelpgx без атрибутов подробностей подключения и SQL-текста и с одним именем span на основе имени сервиса. Ожидается настроенная ненулевая конфигурация пула и соединения; подключение не открывается и сокрытие каждой внешней ошибки не утверждается.
func instrumentPGX(config *pgxpool.Config, serviceName string) {
	config.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithDisableConnectionDetailsInAttributes(),
		otelpgx.WithDisableSQLStatementInAttributes(),
		otelpgx.WithSpanNameCtxFunc(func(context.Context, string) string {
			return serviceName + ".postgresql"
		}),
	)
}

// startTelemetry loads the shared runtime configuration from environment for the supplied service/version, constructs observability under the given context and returns its stdout logger with the local once-only wrapper. Configuration/construction errors pass through without creating a root context or silently falling back to another telemetry backend.
// startTelemetry загружает общую runtime-конфигурацию окружения для переданных сервиса и версии, создаёт наблюдаемость в заданном контексте и возвращает её stdout-журнал с локальной однократной обёрткой. Ошибки конфигурации и создания передаются без нового корневого контекста или тихого перехода к другому хранилищу телеметрии.
func startTelemetry(
	ctx context.Context,
	serviceName string,
	buildVersion string,
) (*appTelemetry, *slog.Logger, error) {
	config, err := observability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return nil, nil, err
	}
	runtime, err := observability.NewRuntime(ctx, config)
	if err != nil {
		return nil, nil, err
	}
	telemetry := &appTelemetry{Runtime: runtime}
	return telemetry, runtime.Logger(os.Stdout), nil
}

// shutdownTracing invokes shared tracing shutdown only once with the first caller's context and returns the cached result on later calls, including a cached timeout or cancellation error.
// shutdownTracing вызывает общее завершение tracing только один раз с контекстом первого вызывающего и затем возвращает сохранённый результат, включая ошибку срока или отмены.
func (telemetry *appTelemetry) shutdownTracing(ctx context.Context) error {
	telemetry.traceOnce.Do(func() {
		telemetry.traceErr = telemetry.ShutdownTracing(ctx)
	})
	return telemetry.traceErr
}

// flushSentry invokes the shared Sentry flush only once with the first caller's context and caches its result independently from tracing cleanup.
// flushSentry вызывает общий flush Sentry только один раз с контекстом первого вызывающего и сохраняет результат независимо от очистки tracing.
func (telemetry *appTelemetry) flushSentry(ctx context.Context) error {
	telemetry.sentryOnce.Do(func() {
		telemetry.sentryErr = telemetry.FlushSentry(ctx)
	})
	return telemetry.sentryErr
}

// cleanupAfterStartupFailure attempts tracing and Sentry cleanup with separate timeout contexts derived from WithoutCancel(shutdownBase), so cancellation or budget exhaustion of one does not cancel the other. Once-only wrappers prevent duplicate work; the joined errors are intentionally discarded by this startup-cleanup helper.
// cleanupAfterStartupFailure пытается завершить tracing и Sentry с отдельными ограниченными контекстами от WithoutCancel(shutdownBase), поэтому отмена или исчерпание бюджета одного не отменяет другой. Однократные обёртки исключают повтор работы; объединённые ошибки этот помощник стартовой очистки намеренно отбрасывает.
func (telemetry *appTelemetry) cleanupAfterStartupFailure(
	shutdownBase context.Context,
	timeout time.Duration,
) {
	traceCtx, traceCancel := context.WithTimeout(
		context.WithoutCancel(shutdownBase),
		timeout,
	)
	traceErr := telemetry.shutdownTracing(traceCtx)
	traceCancel()
	sentryCtx, sentryCancel := context.WithTimeout(
		context.WithoutCancel(shutdownBase),
		timeout,
	)
	sentryErr := telemetry.flushSentry(sentryCtx)
	sentryCancel()
	_ = errors.Join(traceErr, sentryErr)
}
