// Package app owns composition and explicit startup/shutdown for the image builder and admission controller commands.
// Пакет app управляет композицией и явным запуском и остановкой команд сборщика образов и контроллера допуска.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/build"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/observability"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/runner"
)

// runtimeState owns initialized builder dependencies so partial startup can share the same ordered shutdown path.
// Readiness/workers are process-local; authoritative claims and build state remain in the control plane.
// runtimeState владеет созданными зависимостями сборщика, позволяя частичному запуску использовать тот же порядок остановки.
// Готовность и workers локальны процессу; авторитетные claims и состояние сборки остаются в управляющем сервисе.
type runtimeState struct {
	config       Config
	logger       *slog.Logger
	telemetry    *sharedobservability.Runtime
	metrics      *sharedobservability.Metrics
	readiness    *serviceruntime.Readiness
	workers      *serviceruntime.WorkerGroup
	httpServer   *httpserver.Server
	controlPlane *controlplane.Client
}

// Run loads builder configuration, initializes telemetry/RPC/executor and checks local infrastructure before listening and starting workers.
// It blocks on cancellation or serving/worker failure and joins cleanup errors using a noncancelled shutdown base; no root context is created here.
// Run загружает конфигурацию сборщика, создаёт telemetry, RPC и executor и проверяет локальную инфраструктуру до listener и workers.
// Он ждёт отмены либо ошибки сервера или worker и объединяет ошибки очистки с неотменённой базой остановки; корневой контекст здесь не создаётся.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	state := &runtimeState{config: config, readiness: serviceruntime.NewReadiness()}
	defer func() { resultErr = errors.Join(resultErr, state.shutdown(context.WithoutCancel(shutdownBase))) }()
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	state.telemetry, err = sharedobservability.NewRuntimeFromEnv(startup, serviceName, buildVersion)
	if err != nil {
		return err
	}
	state.logger = state.telemetry.Logger(os.Stdout)
	state.metrics = sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	state.metrics.SetReady(false)
	business, err := internalobservability.New(state.metrics.Register)
	if err != nil {
		return err
	}
	state.controlPlane, err = controlplane.Dial(startup, controlplane.Config{
		RPCProfile: config.RPCProfile,
		Target:     config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: 29001, ExpectedIssuerGID: 29000, DialTimeout: 3 * time.Second, RPCDeadline: config.RPCDeadline,
	})
	if err != nil {
		return err
	}
	executor, err := build.New(build.Config{
		Binary: config.BuildKitBinary, Address: config.BuildKitAddress, TLSServerName: config.BuildKitTLSServerName,
		CAFile: config.BuildKitCAFile, CertificateFile: config.BuildKitCertificateFile, PrivateKeyFile: config.BuildKitPrivateKeyFile,
		BuildKitPullDockerConfig:     config.BuildKitPullDockerConfig,
		InputDockerConfig:            config.InputDockerConfig,
		InputRegistryTLSServerName:   config.InputRegistryTLSServerName,
		InputRegistryCAFile:          config.InputRegistryCAFile,
		InputRegistryCertificateFile: config.InputRegistryCertificateFile,
		InputRegistryPrivateKeyFile:  config.InputRegistryPrivateKeyFile,
		AllowedRoleBaseImagesFile:    config.AllowedRoleBaseImagesFile,
		WorkspaceRoot:                config.WorkspaceRoot, InputRepository: config.InputRepository,
		TrustedRoleBaseRepository: config.TrustedRoleBaseRepository, TrustedRoleBaseDigest: config.TrustedRoleBaseDigest,
		FrontendRepository: config.FrontendRepository,
		StagingRepository:  config.StagingRepository, ExpectedBuilderSHA256: config.ExpectedBuilderSHA256,
		ExpectedFrontendSHA256: config.ExpectedFrontendSHA256, ExpectedToolchainSHA256: config.ExpectedToolchainSHA256,
		RoleRuntimeContractRevision: config.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   config.RoleRuntimeContractSHA256,
	})
	if err != nil {
		return err
	}
	if err := checkStartupInfrastructure(lifecycle, executor, config); err != nil {
		return err
	}
	job, err := runner.New(state.controlPlane, executor, business, runner.Config{RenewInterval: config.RenewInterval})
	if err != nil {
		return err
	}
	state.httpServer, err = httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 64 << 10, MaximumConnections: 128,
	}, state.readiness, state.metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := state.httpServer.Listen(); err != nil {
		return err
	}
	serveContext, cancelServe := context.WithCancel(lifecycle)
	defer cancelServe()
	serveResult := make(chan error, 1)
	go func() { serveResult <- state.httpServer.Serve(); cancelServe() }()
	state.workers = serviceruntime.StartWorkers(serveContext,
		runBuildLoop(job.Cycle, state, config),
		monitorLocalReadiness(executor, state, config),
	)
	state.readiness.Set(true, "ready")
	state.metrics.SetReady(true)
	workerResult := make(chan error, 1)
	go func() { workerResult <- state.workers.Wait(serveContext) }()
	select {
	case <-lifecycle.Done():
		return nil
	case serveErr := <-serveResult:
		if serveErr != nil {
			return fmt.Errorf("serve technical HTTP: %w", serveErr)
		}
		return errors.New("technical HTTP server stopped unexpectedly")
	case workerErr := <-workerResult:
		if workerErr != nil {
			return fmt.Errorf("role image builder workers stopped: %w", workerErr)
		}
		return errors.New("role image builder workers stopped unexpectedly")
	}
}

// runBuildLoop calls one build cycle immediately, then waits for poll ticks, logging/capturing only the first degraded transition.
// Recovery is logged once; cycle errors do not stop polling, while cancellation is returned by the post-cycle wait.
// runBuildLoop сразу вызывает один цикл сборки, затем ждёт poll ticks, записывая и отправляя только первый переход к ошибкам.
// Восстановление записывается один раз; ошибка цикла не останавливает опрос, а отмена возвращается при ожидании после цикла.
func runBuildLoop(run func(context.Context) error, state *runtimeState, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		degraded := false
		for {
			err := run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) && !degraded {
				degraded = true
				state.logger.WarnContext(ctx, "role image build delivery degraded", "error_class", "control_plane_or_buildkit")
				state.telemetry.CaptureException(ctx, err)
			} else if err == nil && degraded {
				degraded = false
				state.logger.InfoContext(ctx, "role image build delivery restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// localInfrastructureChecker abstracts the executor's direct registry/BuildKit readiness operation without querying owner work.
// localInfrastructureChecker выделяет прямую проверку готовности реестра и BuildKit без запроса работы владельца.
type localInfrastructureChecker interface {
	// Check performs a synchronous caller-bounded infrastructure probe and returns only after its own cleanup completes.
	// Check выполняет синхронную проверку инфраструктуры с бюджетом вызывающего и возвращается после собственной очистки.
	Check(context.Context) error
}

// checkStartupInfrastructure gives the cold infrastructure check its own readiness budget derived from lifecycle, not telemetry/RPC startup.
// It waits synchronously for Check and releases the child context before returning, without opening listeners or starting workers.
// checkStartupInfrastructure выделяет холодной проверке свой бюджет готовности от lifecycle, а не бюджет запуска telemetry и RPC.
// Он синхронно ждёт Check и освобождает дочерний контекст до возврата, не открывая listener и не запуская workers.
func checkStartupInfrastructure(lifecycle context.Context, executor localInfrastructureChecker, config Config) error {
	infrastructure, cancelInfrastructure := context.WithTimeout(lifecycle, config.ReadinessTimeout)
	defer cancelInfrastructure()
	return executor.Check(infrastructure)
}

// readinessIntervalWaiter abstracts the cancellable interval between completed readiness checks for deterministic unit tests.
// readinessIntervalWaiter выделяет отменяемую паузу между завершёнными проверками готовности для детерминированных unit-тестов.
type readinessIntervalWaiter func(context.Context, time.Duration) error

// monitorLocalReadiness creates the worker using the normal cancellable timer; it does not start the worker itself.
// monitorLocalReadiness создаёт worker с обычным отменяемым таймером, но сам worker не запускает.
func monitorLocalReadiness(
	executor localInfrastructureChecker,
	state *runtimeState,
	config Config,
) serviceruntime.Worker {
	return monitorLocalReadinessWithWait(executor, state, config, waitReadinessInterval)
}

// monitorLocalReadinessWithWait runs one bounded infrastructure check at a time, updates readiness/metrics and logs state transitions.
// The injected wait controls the interval after each check; any wait error ends the worker without changing dependencies.
// monitorLocalReadinessWithWait выполняет по одной ограниченной проверке инфраструктуры, обновляет готовность и метрики и пишет переходы состояния.
// Подставленное ожидание управляет паузой после проверки; любая его ошибка завершает worker без изменения зависимостей.
func monitorLocalReadinessWithWait(
	executor localInfrastructureChecker,
	state *runtimeState,
	config Config,
	wait readinessIntervalWaiter,
) serviceruntime.Worker {
	return func(ctx context.Context) error {
		for {
			infrastructureCheck, cancelInfrastructure := context.WithTimeout(ctx, config.ReadinessTimeout)
			infrastructureErr := executor.Check(infrastructureCheck)
			cancelInfrastructure()
			if infrastructureErr == nil {
				state.metrics.SetReady(true)
				if state.readiness.Set(true, "ready") {
					state.logger.InfoContext(ctx, "role image builder readiness restored")
				}
			} else {
				state.metrics.SetReady(false)
				if state.readiness.Set(false, "local_infrastructure_unavailable") {
					state.logger.WarnContext(ctx, "role image builder readiness lost", "error_class", "buildkit_or_registry")
				}
			}
			if err := wait(ctx, config.ReadinessInterval); err != nil {
				return err
			}
		}
	}
}

// waitReadinessInterval waits for its one-shot timer or caller cancellation and stops the timer on every return.
// waitReadinessInterval ждёт одноразовый таймер либо отмену вызывающего и останавливает таймер при любом возврате.
func waitReadinessInterval(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// shutdown closes readiness, cancels workers and attempts bounded worker join before closing RPC/HTTP and flushing telemetry.
// Each registered cleanup has its own budget; failed or timed-out cleanup is reported, not proof that every worker actually joined.
// shutdown закрывает готовность, отменяет workers и пытается ограниченно дождаться их до закрытия RPC и HTTP и сброса telemetry.
// У каждой операции очистки свой бюджет; сбой или таймаут сообщается, а не считается доказательством завершения всех workers.
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
		serviceruntime.ShutdownOperation{Name: "role image builder workers", Timeout: state.config.ShutdownTimeout / 2, Run: func(ctx context.Context) error {
			if state.workers == nil {
				return nil
			}
			return state.workers.Wait(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: state.config.ShutdownTimeout / 4, Run: func(context.Context) error {
			if state.controlPlane == nil {
				return nil
			}
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
