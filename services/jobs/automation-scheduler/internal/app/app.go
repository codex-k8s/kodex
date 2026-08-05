// Package app содержит единственный composition root automation-scheduler.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/observability"
	"github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/scheduler"
)

const operationFailureMessage = "automation-scheduler operation failed"

type runtimeState struct {
	config       Config
	logger       *slog.Logger
	telemetry    *sharedobservability.Runtime
	metrics      *sharedobservability.Metrics
	readiness    *serviceruntime.Readiness
	workers      *serviceruntime.WorkerGroup
	httpServer   *httpserver.Server
	controlPlane *controlplane.Client
	business     *internalobservability.Metrics
}

func Run(lifecycle context.Context, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	state := &runtimeState{config: config, readiness: serviceruntime.NewReadiness()}
	defer func() {
		resultErr = errors.Join(resultErr, state.shutdown(context.WithoutCancel(shutdownBase)))
	}()
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return err
	}
	state.telemetry, err = sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	state.logger = state.telemetry.Logger(os.Stdout)
	state.metrics = sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	state.metrics.SetReady(false)
	state.business, err = internalobservability.New(state.metrics.Register)
	if err != nil {
		return err
	}
	state.controlPlane, err = controlplane.Dial(startup, controlplane.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID:    29001, ExpectedIssuerGID: 29000,
		DialTimeout: 2 * time.Second, RPCDeadline: config.RPCDeadline,
	})
	if err != nil {
		return err
	}
	if err := state.controlPlane.Check(startup); err != nil {
		return err
	}
	job, err := scheduler.New(state.controlPlane, state.business, scheduler.Config{
		DueLimit: config.DueLimit, ClaimLimit: config.ClaimLimit,
		MaximumTrackedClaims: config.MaximumTrackedClaims,
	})
	if err != nil {
		return err
	}
	state.httpServer, err = httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 64 << 10, MaximumConnections: 128,
	}, state.readiness, state.metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := state.httpServer.Listen(); err != nil {
		return err
	}
	serveCtx, cancelServe := context.WithCancel(lifecycle)
	defer cancelServe()
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- state.httpServer.Serve()
		cancelServe()
	}()
	state.workers = serviceruntime.StartWorkers(
		serveCtx,
		periodic(config.PollInterval, job.Cycle, state.report("poll")),
		periodic(config.ReadinessInterval, state.controlPlane.Check, func(ctx context.Context, checkErr error) {
			if checkErr != nil {
				state.readiness.Set(false, "dependency_unavailable")
				state.metrics.SetReady(false)
				state.report("readiness")(ctx, checkErr)
				return
			}
			state.readiness.Set(true, "ready")
			state.metrics.SetReady(true)
		}),
	)
	state.readiness.Set(true, "ready")
	state.metrics.SetReady(true)
	workerResult := make(chan error, 1)
	go func() { workerResult <- state.workers.Wait(serveCtx) }()
	select {
	case <-lifecycle.Done():
		return nil
	case err := <-serveResult:
		if err != nil {
			return fmt.Errorf("serve technical HTTP: %w", err)
		}
		return errors.New("technical HTTP server stopped unexpectedly")
	case err := <-workerResult:
		if err != nil {
			return fmt.Errorf("automation scheduler workers stopped: %w", err)
		}
		return errors.New("automation scheduler workers stopped unexpectedly")
	}
}

func periodic(interval time.Duration, run func(context.Context) error, report func(context.Context, error)) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			err := run(ctx)
			if !errors.Is(err, context.Canceled) {
				report(ctx, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func (state *runtimeState) report(operation string) func(context.Context, error) {
	return func(ctx context.Context, err error) {
		if err == nil {
			return
		}
		state.logger.Error(operationFailureMessage, "operation", operation, "error", err)
		state.telemetry.CaptureException(ctx, err)
	}
}

func (state *runtimeState) shutdown(base context.Context) error {
	if state.readiness != nil {
		state.readiness.Set(false, "stopping")
	}
	if state.metrics != nil {
		state.metrics.SetReady(false)
	}
	if state.workers != nil {
		state.workers.Stop()
	}
	return serviceruntime.RunShutdown(base,
		serviceruntime.ShutdownOperation{Name: "automation scheduler workers", Timeout: state.config.ShutdownTimeout / 2, Run: func(ctx context.Context) error {
			if state.workers == nil {
				return nil
			}
			return state.workers.Wait(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: state.config.ShutdownTimeout / 4, Run: func(context.Context) error {
			return state.controlPlane.Close()
		}},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: state.config.ShutdownTimeout / 4, Run: func(ctx context.Context) error {
			if state.httpServer == nil {
				return nil
			}
			return state.httpServer.Shutdown(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: func(ctx context.Context) error {
			if state.telemetry == nil {
				return nil
			}
			return state.telemetry.ShutdownTracing(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "Sentry", Timeout: 5 * time.Second, Run: func(ctx context.Context) error {
			if state.telemetry == nil {
				return nil
			}
			return state.telemetry.FlushSentry(ctx)
		}},
	)
}
