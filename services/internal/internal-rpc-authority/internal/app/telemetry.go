package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

type appTelemetry struct {
	*observability.Runtime
	traceOnce  sync.Once
	sentryOnce sync.Once
	traceErr   error
	sentryErr  error
}

func instrumentPGX(config *pgxpool.Config, serviceName string) {
	config.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithDisableConnectionDetailsInAttributes(),
		otelpgx.WithDisableSQLStatementInAttributes(),
		otelpgx.WithSpanNameCtxFunc(func(context.Context, string) string {
			return serviceName + ".postgresql"
		}),
	)
}

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

func (telemetry *appTelemetry) shutdownTracing(ctx context.Context) error {
	telemetry.traceOnce.Do(func() {
		telemetry.traceErr = telemetry.ShutdownTracing(ctx)
	})
	return telemetry.traceErr
}

func (telemetry *appTelemetry) flushSentry(ctx context.Context) error {
	telemetry.sentryOnce.Do(func() {
		telemetry.sentryErr = telemetry.FlushSentry(ctx)
	})
	return telemetry.sentryErr
}

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
