// Package app содержит composition root необязательной платформы интеграций.
package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration"
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
		DialTimeout: config.RequestTimeout, Operations: controlplaneclient.IntegrationGatewayOperations(),
	})
	if err != nil {
		return err
	}
	adapter, err := integration.New(integration.Config{CredentialDirectory: config.CredentialDirectory, ProxyURL: config.EgressProxyURL, AllowedHosts: config.AllowedIntegrationHosts, Timeout: config.OperationTimeout})
	if err != nil {
		return err
	}
	technical, err := httpserver.New(httpserver.Config{Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 32 << 10, MaximumConnections: 128}, readiness, metrics.PrometheusHandler())
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	workers := serviceruntime.StartWorkers(lifecycle, serveTechnical(technical), monitorLocalReadiness(control, readiness, metrics, logger, config), runIntegrationLoop(control, adapter, logger, config))
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "integration workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
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
			check, cancel := context.WithTimeout(ctx, config.RequestTimeout)
			err := control.CheckLocalAuthority(check)
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "integration gateway readiness restored")
				}
				metrics.SetReady(true)
			} else {
				if readiness.Set(false, "local_authority_unavailable") {
					logger.WarnContext(ctx, "integration gateway readiness lost", "error_class", "sidecar")
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

func runIntegrationLoop(control *controlplaneclient.Client, adapter *integration.Adapter, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := processIntegrationWork(cycle, control, adapter, config)
			cancel()
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "integration work delivery degraded", "error_class", "control_plane_or_adapter")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "integration work delivery restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func processIntegrationWork(ctx context.Context, control *controlplaneclient.Client, adapter *integration.Adapter, config Config) error {
	tests, err := control.Runtime.ClaimIntegrationConnectionTests(ctx, &controlplanev1.ClaimIntegrationConnectionTestsRequest{WorkloadInstance: config.InstanceID, Limit: config.ClaimLimit})
	if err != nil {
		return err
	}
	for _, claim := range tests.GetClaims() {
		result, operationErr := adapter.Test(ctx, integration.RequestFromTest(claim))
		if err := completeTest(ctx, control, claim, result, operationErr); err != nil {
			return err
		}
	}
	invocations, err := control.Runtime.ClaimIntegrationInvocations(ctx, &controlplanev1.ClaimIntegrationInvocationsRequest{WorkloadInstance: config.InstanceID, Limit: config.ClaimLimit})
	if err != nil {
		return err
	}
	for _, claim := range invocations.GetClaims() {
		result, operationErr := adapter.Execute(ctx, integration.RequestFromInvocation(claim))
		if err := completeInvocation(ctx, control, claim, result, operationErr); err != nil {
			return err
		}
	}
	return nil
}

func completeTest(ctx context.Context, control *controlplaneclient.Client, claim *controlplanev1.IntegrationConnectionTestClaim, result string, operationErr error) error {
	lease := claim.GetLease()
	if lease == nil {
		return errors.New("integration test lease is missing")
	}
	success, code := integration.Outcome(operationErr)
	_, err := control.Runtime.CompleteIntegrationConnectionTest(ctx, &controlplanev1.CompleteIntegrationConnectionTestRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(claim.GetTestRef(), "complete")}, TestRef: claim.GetTestRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), Success: success, ResultSummary: result, SafeErrorCode: code})
	return err
}

func completeInvocation(ctx context.Context, control *controlplaneclient.Client, claim *controlplanev1.IntegrationInvocationClaim, result string, operationErr error) error {
	lease := claim.GetLease()
	if lease == nil {
		return errors.New("integration invocation lease is missing")
	}
	success, code := integration.Outcome(operationErr)
	_, err := control.Runtime.CompleteIntegrationInvocation(ctx, &controlplanev1.CompleteIntegrationInvocationRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(claim.GetInvocationRef(), "complete")}, InvocationRef: claim.GetInvocationRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), Success: success, ResultSummary: result, SafeErrorCode: code})
	return err
}

func stableKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}
