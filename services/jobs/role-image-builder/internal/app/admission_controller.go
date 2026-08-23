package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/admissioncontroller"
)

const admissionControllerServiceName = "image-admission-controller"

func RunAdmissionController(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadAdmissionControllerConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.RequestTimeout)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(admissionControllerServiceName, buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(admissionControllerServiceName, buildVersion, nil)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "infrastructure_starting")
	metrics.SetReady(false)
	renderer, err := admissioncontroller.NewScriptRenderer(config.RendererPath)
	if err != nil {
		return err
	}
	controller, err := admissioncontroller.InCluster(config.controllerConfig(), renderer, logger)
	if err != nil {
		return err
	}
	if err := controller.Check(startup); err != nil {
		return err
	}
	technical, err := httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 64 << 10, MaximumConnections: 128,
	}, readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	workers := serviceruntime.StartWorkers(lifecycle, controller.Run,
		monitorAdmissionControllerReadiness(controller, readiness, metrics, logger, config),
	)
	serveResult := make(chan error, 1)
	go func() { serveResult <- technical.Serve() }()
	workerResult := make(chan error, 1)
	go func() { workerResult <- workers.Wait(context.WithoutCancel(lifecycle)) }()
	defer func() {
		readiness.Set(false, "stopping")
		metrics.SetReady(false)
		workers.Stop()
		resultErr = errors.Join(resultErr, serviceruntime.RunShutdown(shutdownBase,
			serviceruntime.ShutdownOperation{Name: "image admission technical HTTP", Timeout: config.ShutdownTimeout / 3, Run: technical.Shutdown},
			serviceruntime.ShutdownOperation{Name: "image admission workers", Timeout: config.ShutdownTimeout / 3, Run: workers.Wait},
			serviceruntime.ShutdownOperation{Name: "image admission tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "image admission Sentry", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
		))
	}()
	select {
	case <-lifecycle.Done():
		return nil
	case err = <-serveResult:
		if err == nil {
			return errors.New("image admission technical HTTP stopped unexpectedly")
		}
		return fmt.Errorf("serve image admission technical HTTP: %w", err)
	case err = <-workerResult:
		if errors.Is(err, context.Canceled) || lifecycle.Err() != nil {
			return nil
		}
		return err
	}
}

func monitorAdmissionControllerReadiness(controller *admissioncontroller.Controller, readiness *serviceruntime.Readiness,
	metrics *sharedobservability.Metrics, logger interface {
		InfoContext(context.Context, string, ...any)
		WarnContext(context.Context, string, ...any)
	}, config admissionControllerConfig) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.InfrastructureCheck)
		defer ticker.Stop()
		for {
			request, cancel := context.WithTimeout(ctx, config.RequestTimeout)
			err := controller.Check(request)
			cancel()
			if err == nil {
				metrics.SetReady(true)
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "image admission controller readiness restored")
				}
			} else {
				metrics.SetReady(false)
				if readiness.Set(false, "local_infrastructure_unavailable") {
					logger.WarnContext(ctx, "image admission controller readiness lost", "error_class", "kubernetes_api")
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}
