package app

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

func newTelemetry(ctx context.Context, version string) (*observability.Runtime, error) {
	config, err := observability.RuntimeConfigFromEnv("email-bridge", version)
	if err != nil {
		return nil, err
	}
	return observability.NewRuntime(ctx, config)
}

type telemetryShutdown interface {
	ShutdownTracing(context.Context) error
	FlushSentry(context.Context) error
}

func stopTelemetry(base context.Context, runtime telemetryShutdown) error {
	return serviceruntime.RunShutdown(context.WithoutCancel(base),
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: runtime.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "sentry", Timeout: 5 * time.Second, Run: runtime.FlushSentry},
	)
}
