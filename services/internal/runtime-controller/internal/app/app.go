// Package app содержит единственный composition root runtime-controller.
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
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/adapters/eventconsumer"
	kubeadapter "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/adapters/kubernetes"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/clients/controlplane"
	runtimeservice "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/service/runtime"
	internalobservability "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/observability"
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
	consumer     *eventconsumer.Consumer
	business     *internalobservability.Metrics
}

func Run(
	lifecycle context.Context,
	shutdownBase context.Context,
	buildVersion string,
) (resultErr error) {
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
	state.metrics = sharedobservability.NewMetrics(serviceName, buildVersion, map[string]string{})
	state.metrics.SetReady(false)
	businessMetrics, err := internalobservability.New(state.metrics.Register)
	if err != nil {
		return err
	}
	state.business = businessMetrics
	state.controlPlane, err = controlplane.Dial(startup, controlplane.Config{
		Mode: controlplane.ModeController, Target: config.ControlPlaneTarget,
		TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile:               config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile:                config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile:                config.ApplicationGrantFile,
		ExpectedRoleImageRepository:         config.PromotedRoleImageRepository,
		ExpectedRoleRuntimeContractRevision: config.RoleRuntimeContractRevision,
		ExpectedRoleRuntimeContractSHA256:   config.RoleRuntimeContractSHA256,
		ExpectedIssuerUID:                   29001, ExpectedIssuerGID: 29000, DialTimeout: 2 * time.Second,
	})
	if err != nil {
		return err
	}
	state.consumer, err = eventconsumer.Open(startup, eventconsumer.Config{
		NATSURL: config.NATSURL, NATSTLSServerName: config.NATSTLSServerName,
		NATSCAFile: config.NATSCAFile, NATSCertificateFile: config.NATSCertificateFile,
		NATSPrivateKeyFile: config.NATSPrivateKeyFile, NATSCredentialsFile: config.NATSCredentialsFile,
		Stream: config.NATSStream, Durable: config.NATSDurable, Replicas: config.NATSReplicas,
		MaxBytes:        config.NATSMaxBytes,
		PostgresDSNFile: config.PostgresDSNFile, PostgresTLSServerName: config.PostgresTLSServerName,
		PostgresCAFile: config.PostgresCAFile, PostgresPrincipal: config.PostgresPrincipal,
		InstanceID: config.PodUID, FetchTimeout: 2 * time.Second,
	}, state.controlPlane, businessMetrics, state.report("event_consume"))
	if err != nil {
		return err
	}
	cluster, err := kubeadapter.InCluster(kubeadapter.Config{
		Environment: config.Environment, Namespace: config.Namespace,
		RunnerControlPlaneTarget:        config.RunnerControlPlaneTarget,
		RunnerControlPlaneTLSServerName: config.RunnerControlPlaneTLSServerName,
		InteractionGatewayURL:           config.InteractionGatewayURL, SessionMCPURL: config.SessionMCPURL,
		ControllerImage: config.ControllerImage, AuthorityImage: config.AuthorityImage,
		PromotedRoleImageRepository: config.PromotedRoleImageRepository,
		StorageClass:                config.StorageClass, PVCSize: config.PVCSize,
		ReadClusterRole:       config.ReadClusterRole,
		AdminClusterRole:      config.AdminClusterRole,
		ArchiveRestoreEnabled: config.ArchiveRestoreCapability == "enabled",
		ArchiveServiceAccount: config.ArchiveServiceAccount,
		RestoreServiceAccount: config.RestoreServiceAccount, CleanupServiceAccount: config.CleanupServiceAccount,
		CredentialBrokerServiceAccount:   config.CredentialBrokerServiceAccount,
		ProjectReadBrokerServiceAccount:  config.ProjectReadBrokerServiceAccount,
		ClusterAdminBrokerServiceAccount: config.ClusterAdminBrokerServiceAccount,
		S3ArchiveBrokerServiceAccount:    config.S3ArchiveBrokerServiceAccount,
		S3RestoreBrokerServiceAccount:    config.S3RestoreBrokerServiceAccount,
		MaximumPods:                      config.MaximumPods, MaximumOrganizationExecutions: config.MaximumOrganizationExecutions,
		MaximumCPU:         config.MaximumCPUMilli,
		MaximumMemoryBytes: config.MaximumMemoryBytes, JobTTL: config.JobTTL,
		S3Endpoint: config.S3Endpoint, S3TLSServerName: config.S3TLSServerName,
		S3Bucket: config.S3Bucket, S3Region: config.S3Region,
	})
	if err != nil {
		return err
	}
	service, err := runtimeservice.New(
		state.controlPlane, cluster, businessMetrics, config.WarmTTL,
		config.Watchdog, config.ArchiveRestoreCapability == "enabled",
	)
	if err != nil {
		return err
	}
	if err := service.Check(startup); err != nil {
		return err
	}
	state.httpServer, err = httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 64 << 10, MaximumConnections: 256,
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
	go func() {
		serveResult <- state.httpServer.Serve()
		cancelServe()
	}()
	if err := state.consumer.Check(startup); err != nil {
		return err
	}
	// Readiness означает способность Pod принять лидерство. Standby-реплика
	// уже прошла те же startup dependency checks и не должна блокировать rollout.
	state.readiness.Set(true, "standby")
	state.metrics.SetReady(true)
	leaderErr := cluster.RunAsLeader(serveContext, config.PodUID, func(leaderContext context.Context) error {
		state.readiness.Set(true, "ready")
		state.metrics.SetReady(true)
		defer func() {
			state.readiness.Set(true, "standby")
			state.metrics.SetReady(true)
		}()
		state.workers = serviceruntime.StartWorkers(leaderContext,
			func(ctx context.Context) error { return state.consumer.Run(ctx) },
			periodic(config.ClaimInterval, func(ctx context.Context) error { return service.ReconcileNext(ctx) }, state.report("claim_loop")),
			periodic(config.ReconcileInterval, func(ctx context.Context) error { return service.ReconcileExisting(ctx) }, state.report("reconcile_loop")),
			periodic(config.ExpiryInterval, func(ctx context.Context) error { return service.ExpireOne(ctx) }, state.report("expiry_loop")),
			periodic(time.Hour, func(ctx context.Context) error {
				return service.CleanupTemporary(ctx, time.Now().UTC().Add(-config.JobTTL))
			}, state.report("temporary_cleanup_loop")),
			periodic(config.ReadinessInterval, func(ctx context.Context) error {
				checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				if err := service.Check(checkCtx); err != nil {
					return err
				}
				return state.consumer.Check(checkCtx)
			}, func(ctx context.Context, err error) {
				if err != nil {
					state.readiness.Set(false, "dependency_unavailable")
					state.metrics.SetReady(false)
					state.report("readiness_loop")(ctx, err)
					return
				}
				state.readiness.Set(true, "ready")
				state.metrics.SetReady(true)
			}),
		)
		workerResult := make(chan error, 1)
		go func() { workerResult <- state.workers.Wait(leaderContext) }()
		select {
		case <-leaderContext.Done():
			return nil
		case err := <-workerResult:
			if err != nil {
				return fmt.Errorf("runtime workers stopped: %w", err)
			}
			return errors.New("runtime workers stopped unexpectedly")
		case err := <-serveResult:
			if err != nil {
				return fmt.Errorf("serve technical HTTP: %w", err)
			}
			return errors.New("technical HTTP server stopped unexpectedly")
		}
	})
	select {
	case err := <-serveResult:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("serve technical HTTP: %w", err)
		}
	default:
	}
	if lifecycle.Err() != nil {
		return nil
	}
	return leaderErr
}

func periodic(interval time.Duration, run func(context.Context) error, report func(context.Context, error)) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				report(ctx, err)
			} else {
				report(ctx, nil)
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
		state.logger.Error("runtime-controller operation failed", "operation", operation, "error", err)
		if state.business != nil {
			state.business.ObserveFailure(operation)
		}
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
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: state.config.ShutdownTimeout / 4, Run: func(ctx context.Context) error {
			if state.httpServer == nil {
				return nil
			}
			return state.httpServer.Shutdown(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "runtime workers", Timeout: state.config.ShutdownTimeout / 2, Run: func(ctx context.Context) error {
			if state.workers == nil {
				return nil
			}
			return state.workers.Wait(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "runtime event consumer", Timeout: state.config.ShutdownTimeout / 2, Run: func(ctx context.Context) error {
			if state.consumer == nil {
				return nil
			}
			return state.consumer.Close(ctx)
		}},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: state.config.ShutdownTimeout / 4, Run: func(context.Context) error {
			if state.controlPlane == nil {
				return nil
			}
			return state.controlPlane.Close()
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
