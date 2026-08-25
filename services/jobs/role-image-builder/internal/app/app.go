// Package app содержит единственный composition root role-image-builder.
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

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	state := &runtimeState{config: config, readiness: serviceruntime.NewReadiness()}
	defer func() { resultErr = errors.Join(resultErr, state.shutdown(context.WithoutCancel(shutdownBase))) }()
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
	business, err := internalobservability.New(state.metrics.Register)
	if err != nil {
		return err
	}
	state.controlPlane, err = controlplane.Dial(startup, controlplane.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
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
	if err := state.controlPlane.CheckLocalAuthority(startup); err != nil {
		return err
	}
	if err := executor.Check(startup); err != nil {
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
		monitorLocalReadiness(state.controlPlane, executor, state, config),
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

func monitorLocalReadiness(control *controlplane.Client, executor *build.Executor, state *runtimeState, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			authorityCheck, cancelAuthority := context.WithTimeout(ctx, config.RPCDeadline)
			authorityErr := control.CheckLocalAuthority(authorityCheck)
			cancelAuthority()
			infrastructureCheck, cancelInfrastructure := context.WithTimeout(ctx, config.RPCDeadline)
			infrastructureErr := executor.Check(infrastructureCheck)
			cancelInfrastructure()
			if authorityErr == nil && infrastructureErr == nil {
				state.metrics.SetReady(true)
				if state.readiness.Set(true, "ready") {
					state.logger.InfoContext(ctx, "role image builder readiness restored")
				}
			} else {
				state.metrics.SetReady(false)
				if state.readiness.Set(false, "local_infrastructure_unavailable") {
					failureClass := "buildkit_or_registry"
					if authorityErr != nil {
						failureClass = "sidecar"
					}
					state.logger.WarnContext(ctx, "role image builder readiness lost", "error_class", failureClass)
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
