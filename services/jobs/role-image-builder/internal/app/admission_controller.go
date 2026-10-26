package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/admissioncontroller"
)

// admissionControllerServiceName fixes the controller command's telemetry identity independently of the builder process.
// admissionControllerServiceName фиксирует идентичность telemetry команды контроллера независимо от процесса сборщика.
const admissionControllerServiceName = "image-admission-controller"

// RunAdmissionController wires telemetry, profile-selected availability, the local renderer, in-cluster client and pinned leadership.
// After infrastructure preflight it starts technical HTTP and workers, then stops them and joins bounded cleanup errors on exit.
// Existing availability-profile selection is preserved; this composition path does not itself grant admission or start an agent session.
// RunAdmissionController связывает telemetry, доступность по выбранному профилю, локальный renderer, внутрикластерный клиент и закреплённое лидерство.
// После проверки инфраструктуры запускаются технический HTTP и workers, а при выходе они останавливаются с объединением ограниченных ошибок очистки.
// Существующий выбор профиля доступности сохраняется; корень композиции сам не выдаёт допуск и не запускает сессию агента.
func RunAdmissionController(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadAdmissionControllerConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.RequestTimeout)
	defer cancelStartup()
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, admissionControllerServiceName, buildVersion)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, serviceruntime.RunShutdown(shutdownBase,
			serviceruntime.ShutdownOperation{Name: "image admission tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "image admission Sentry", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
		))
	}()
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(admissionControllerServiceName, buildVersion, nil)
	readiness := serviceruntime.NewReadiness()
	readiness.Set(false, "infrastructure_starting")
	metrics.SetReady(false)
	renderer, err := admissioncontroller.NewScriptRenderer(config.RendererPath)
	if err != nil {
		return err
	}
	controllerConfig := config.controllerConfig()
	controllerConfig.WorkSource = admissioncontroller.LegacyPollingWorkSource{}
	if config.RPCProfile == transportprofile.TrustedCluster {
		workSource, dialErr := admissioncontroller.DialWorkSource(startup, config.workSourceConfig())
		if dialErr != nil {
			return dialErr
		}
		defer func() { resultErr = errors.Join(resultErr, workSource.Close()) }()
		controllerConfig.WorkSource = workSource
	}
	controller, err := admissioncontroller.InCluster(controllerConfig, renderer, logger)
	if err != nil {
		return err
	}
	if err := controller.ConfigureLeadership(startup, config.PodUID); err != nil {
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

// monitorAdmissionControllerReadiness periodically probes only local controller infrastructure and publishes readiness transitions.
// Checks use per-request timeouts; failures close readiness without stopping the loop, and cancellation ends the next wait.
// monitorAdmissionControllerReadiness периодически проверяет только локальную инфраструктуру контроллера и публикует переходы готовности.
// Проверки используют отдельные таймауты; ошибка закрывает готовность без остановки цикла, а отмена завершает очередное ожидание.
func monitorAdmissionControllerReadiness(controller *admissioncontroller.Controller, readiness *serviceruntime.Readiness,
	metrics *sharedobservability.Metrics, logger interface {
		// InfoContext records restored readiness without taking ownership of the caller's logger.
		// InfoContext записывает восстановление готовности, не забирая владение журналом вызывающего.
		InfoContext(context.Context, string, ...any)
		// WarnContext records a closed local-infrastructure failure class without logging private API error details.
		// WarnContext записывает закрытый класс сбоя локальной инфраструктуры без внутренних подробностей ошибки API.
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
