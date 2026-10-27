package app

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// newTelemetry loads the canonical process telemetry configuration, preserving SDK disablement and exact exporter pins before runtime construction.
// newTelemetry читает каноническую конфигурацию телеметрии процесса, сохраняя отключение SDK и точные привязки экспорта до создания runtime.
func newTelemetry(ctx context.Context, version string) (*observability.Runtime, error) {
	return observability.NewRuntimeFromEnv(ctx, "email-bridge", version)
}

// telemetryShutdown narrows cleanup ownership to tracing and Sentry, allowing independent budget assertions without real exporters.
// telemetryShutdown ограничивает владение очисткой трассировкой и Sentry, позволяя проверять независимые бюджеты без настоящих exporters.
type telemetryShutdown interface {
	// ShutdownTracing flushes/stops tracing within the caller's bounded cleanup context.
	// ShutdownTracing завершает и сбрасывает трассировку в ограниченном контексте очистки вызывающего.
	ShutdownTracing(context.Context) error
	// FlushSentry flushes error reporting under its own context rather than inheriting a previous cleanup failure.
	// FlushSentry сбрасывает отчёты ошибок в собственном контексте, не наследуя отказ прежнего этапа очистки.
	FlushSentry(context.Context) error
}

// stopTelemetry detaches lifecycle cancellation and attempts tracing and Sentry cleanup in order with independent five-second budgets.
// stopTelemetry отделяет отмену жизненного цикла и последовательно пытается завершить tracing и Sentry с независимыми бюджетами по пять секунд.
func stopTelemetry(base context.Context, runtime telemetryShutdown) error {
	return serviceruntime.RunShutdown(context.WithoutCancel(base),
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: runtime.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "sentry", Timeout: 5 * time.Second, Run: runtime.FlushSentry},
	)
}
