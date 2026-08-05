// Package app содержит единственный composition root role-image-builder.
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
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/build"
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/observability"
	"github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/runner"
)

const operationFailureMessage = "role-image-builder operation failed"

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
		CredentialVaultAddress:       config.CredentialVaultAddress, CredentialVaultTLSServerName: config.CredentialVaultTLSServerName,
		CredentialVaultCAFile: config.CredentialVaultCAFile, CredentialVaultTokenFile: config.CredentialVaultTokenFile,
		CredentialVaultRole: config.CredentialVaultRole,
		WorkspaceRoot:       config.WorkspaceRoot, InputRepository: config.InputRepository,
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
	if err := state.controlPlane.Check(startup); err != nil {
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
		periodic(config.PollInterval, job.Cycle, state.report("build_cycle")),
		periodic(config.ReadinessInterval, func(ctx context.Context) error {
			check, cancel := context.WithTimeout(ctx, config.RPCDeadline)
			defer cancel()
			return errors.Join(state.controlPlane.Check(check), executor.Check(check))
		}, func(ctx context.Context, checkErr error) {
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

func periodic(interval time.Duration, run func(context.Context) error, report func(context.Context, error)) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			err := run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
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
		state.logger.Error(operationFailureMessage, "operation", operation, "error_class", "bounded_failure")
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
