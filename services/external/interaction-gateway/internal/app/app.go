// Package app содержит composition root необязательного interaction adapter.
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
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/mattermost"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/usertext"
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
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneCertificateFile, ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RequestTimeout, Operations: controlplaneclient.InteractionGatewayOperations(),
	})
	if err != nil {
		return err
	}
	if err := control.CheckLocalAuthority(startup); err != nil {
		return errors.Join(
			fmt.Errorf("interaction gateway startup barrier failed: %w", err),
			control.Close(),
		)
	}
	text, err := usertext.New()
	if err != nil {
		return err
	}
	adapter, err := mattermost.New(mattermost.Config{
		CredentialDirectory: config.CredentialDirectory, ProxyURL: config.EgressProxyURL,
		AllowedHosts: config.AllowedHosts, Timeout: config.OperationTimeout,
	}, text)
	if err != nil {
		return err
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
	sources := newSourceManager(control, adapter, logger, config)
	workers := serviceruntime.StartWorkers(lifecycle,
		serveTechnical(technical),
		monitorLocalReadiness(control, readiness, metrics, logger, config),
		runDeliveryLoop(control, adapter, logger, config),
		runSourceRefresh(sources, control, logger, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "interaction workers", Timeout: config.ShutdownTimeout / 3, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "interaction sources", Timeout: config.ShutdownTimeout / 3, Run: sources.Close},
		serviceruntime.ShutdownOperation{Name: "control-plane client", Timeout: config.ShutdownTimeout / 6, Run: func(context.Context) error { return control.Close() }},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 6, Run: technical.Shutdown},
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
			check, cancel := context.WithTimeout(ctx, config.RequestTimeout)
			err := control.CheckLocalAuthority(check)
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "interaction gateway readiness restored")
				}
				metrics.SetReady(true)
			} else {
				if readiness.Set(false, "local_authority_unavailable") {
					logger.WarnContext(ctx, "interaction gateway readiness lost", "error_class", "sidecar")
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

func runDeliveryLoop(control *controlplaneclient.Client, adapter *mattermost.Adapter, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := processDeliveries(cycle, control, adapter, config)
			cancel()
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "interaction delivery degraded", "error_class", "control_plane_or_adapter")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "interaction delivery restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func processDeliveries(ctx context.Context, control *controlplaneclient.Client, adapter *mattermost.Adapter, config Config) error {
	claimed, err := control.Interaction.ClaimInteractionDeliveries(ctx, &controlplanev1.ClaimInteractionDeliveriesRequest{
		WorkloadInstance: config.InstanceID, Limit: config.ClaimLimit,
	})
	if err != nil {
		return err
	}
	for _, claim := range claimed.GetClaims() {
		postRef, threadRef, deliveryErr := adapter.Deliver(ctx, claim)
		if err := completeDelivery(ctx, control, claim, postRef, threadRef, deliveryErr); err != nil {
			return err
		}
	}
	return nil
}

func completeDelivery(ctx context.Context, control *controlplaneclient.Client, claim *controlplanev1.InteractionDeliveryClaim, postRef, threadRef string, deliveryErr error) error {
	lease := claim.GetLease()
	if lease == nil {
		return errors.New("interaction delivery lease is missing")
	}
	success, code := mattermost.Outcome(deliveryErr)
	_, err := control.Interaction.CompleteInteractionDelivery(ctx, &controlplanev1.CompleteInteractionDeliveryRequest{
		Mutation:    &controlplanev1.MutationContext{IdempotencyKey: stableKey(claim.GetDeliveryRef(), "complete")},
		DeliveryRef: claim.GetDeliveryRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(),
		Success: success, ExternalPostRef: postRef, ExternalThreadRef: threadRef, SafeErrorCode: code,
	})
	return err
}

func stableKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}
