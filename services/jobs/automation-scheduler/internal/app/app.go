// Package app содержит composition root server-owned automation scheduler.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/google/uuid"
)

const (
	issuerUID = 29001
	issuerGID = 29000
)

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	logger := telemetry.Logger(os.Stdout)
	metrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	readiness := serviceruntime.NewReadiness()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RPCDeadline,
		Operations: controlplaneclient.AutomationSchedulerOperations(),
	})
	if err != nil {
		return err
	}
	if err := control.CheckLocalAuthority(startup); err != nil {
		return errors.Join(
			fmt.Errorf("automation scheduler startup barrier failed: %w", err),
			control.Close(),
		)
	}
	technical, err := httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 32 << 10, MaximumConnections: 128,
	}, readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	workers := serviceruntime.StartWorkers(
		lifecycle,
		serveTechnical(technical),
		monitorLocalReadiness(control, readiness, metrics, logger, config),
		runScheduleLoop(control, logger, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "scheduler workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: config.ShutdownTimeout / 4, Run: func(context.Context) error { return control.Close() }},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 4, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
	)
	return errors.Join(err, shutdownErr)
}

func serveTechnical(server *httpserver.Server) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve() }()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func monitorLocalReadiness(control *controlplaneclient.Client, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.RPCDeadline)
			err := control.CheckLocalAuthority(check)
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "automation scheduler readiness restored")
				}
				metrics.SetReady(true)
			} else {
				if readiness.Set(false, "local_authority_unavailable") {
					logger.WarnContext(ctx, "automation scheduler readiness lost", "error_class", "sidecar")
				}
				metrics.SetReady(false)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func runScheduleLoop(control *controlplaneclient.Client, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.RPCDeadline)
			err := materializeDue(cycle, control, config)
			cancel()
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "schedule materialization degraded", "error_class", "control_plane")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "schedule materialization restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func materializeDue(ctx context.Context, control *controlplaneclient.Client, config Config) error {
	claimed, err := control.Runtime.ClaimDueSchedules(ctx, &controlplanev1.ClaimDueSchedulesRequest{WorkloadInstance: config.InstanceID, Limit: int32(config.DueLimit)})
	if err != nil {
		return err
	}
	for _, claim := range claimed.GetClaims() {
		lease := claim.GetLease()
		if claim.GetOccurrenceRef() == "" || lease == nil {
			return errors.New("schedule claim is incomplete")
		}
		_, err := control.Runtime.MaterializeScheduleOccurrence(ctx, &controlplanev1.MaterializeScheduleOccurrenceRequest{
			Mutation:      &controlplanev1.MutationContext{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceOID, []byte(claim.GetOccurrenceRef()+"\x00materialize")).String()},
			OccurrenceRef: claim.GetOccurrenceRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		})
		if err != nil {
			return err
		}
	}
	return nil
}
