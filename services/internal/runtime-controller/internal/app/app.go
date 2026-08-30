// Package app содержит единственный composition root runtime-controller.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
)

const (
	serviceName                   = "runtime-controller"
	issuerUID                     = 29001
	issuerGID                     = 29000
	kubernetesLastKnownGoodWindow = 2 * time.Minute
)

type kubernetesReadinessObserver struct {
	now         func() time.Time
	lastSuccess time.Time
	degraded    bool
}

func newKubernetesReadinessObserver() *kubernetesReadinessObserver {
	return &kubernetesReadinessObserver{now: time.Now}
}

func (observer *kubernetesReadinessObserver) Observe(err error, allowLastKnownGood bool) (available, changed, degraded bool) {
	now := observer.now().UTC()
	if err == nil {
		changed = observer.degraded
		observer.lastSuccess = now
		observer.degraded = false
		return true, changed, false
	}
	changed = !observer.degraded
	observer.degraded = true
	if !allowLastKnownGood {
		return false, changed, true
	}
	available = !observer.lastSuccess.IsZero() && now.Before(observer.lastSuccess.Add(kubernetesLastKnownGoodWindow))
	return available, changed, true
}

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, 10*time.Second)
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
	metrics := sharedobservability.NewMetrics(serviceName, buildVersion, nil)
	defer func() {
		trace, cancelTrace := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(trace))
		cancelTrace()
		sentry, cancelSentry := context.WithTimeout(shutdownBase, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentry))
		cancelSentry()
	}()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RequestTimeout,
		Operations: controlplaneclient.RuntimeOperations(),
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	manager, err := workload.InCluster(workload.Config{
		Environment: config.Environment, ControlNamespace: config.ControlNamespace, RuntimeNamespace: config.RuntimeNamespace,
		ControllerPodUID: config.PodUID, ControllerPodIP: config.PodIP,
		CallbackTLSServerName: config.CallbackTLSServerName, CallbackClientCASecret: config.CallbackClientCASecret,
		CallbackClientTLSSecret: config.CallbackClientTLSSecret, ProviderHTTPSProxy: config.ProviderHTTPSProxy,
		KubernetesAPIServiceIP: config.KubernetesAPIServiceIP,
		StorageClass:           config.StorageClass, SessionPVCSize: config.SessionPVCSize, RunnerServiceAccount: config.RunnerServiceAccount,
		PromotedRoleImageRepository: config.PromotedRoleImageRepository, RoleRuntimeContractRevision: config.RoleRuntimeContractRevision,
		DefaultRoleImageReference: config.DefaultRoleImageReference,
		RoleRuntimeContractSHA256: config.RoleRuntimeContractSHA256,
	})
	if err != nil {
		return err
	}
	if err := errors.Join(
		control.CheckLocalAuthority(startup),
		manager.Check(startup),
	); err != nil {
		return errors.Join(errors.New("runtime controller startup barrier failed"), err)
	}
	coordinator := callback.NewCoordinator()
	callbackServer, err := callback.New(callback.Config{Listen: config.CallbackListen,
		CertificateFile: config.CallbackServerCertificateFile, PrivateKeyFile: config.CallbackServerPrivateKeyFile,
		ClientCAFile: config.CallbackClientCAFile, ExpectedClientSPIFFEID: config.CallbackExpectedClientSPIFFEID,
		RequestTimeout: config.RequestTimeout, WarmLongPoll: config.WarmLongPoll}, manager, control, coordinator, logger)
	if err != nil {
		return err
	}
	unitReadiness, assistantReadiness := serviceruntime.NewReadiness(), serviceruntime.NewReadiness()
	unitReadiness.Set(false, "infrastructure_starting")
	metrics.SetReady(false)
	assistantReadiness.Set(false, "assistant_runtime_starting")
	runtime := newRuntime(control, manager, coordinator, config, assistantReadiness, logger)
	technical := technicalServer(lifecycle, config, unitReadiness, assistantReadiness, metrics)
	workers := serviceruntime.StartWorkers(lifecycle,
		serveHTTP(technical, config.ShutdownTimeout), callbackServer.Run,
		monitorUnitReadiness(control, manager, unitReadiness, metrics, logger, config),
		func(ctx context.Context) error { return manager.RunAsLeader(ctx, runtime.Run) },
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "runtime callback server", Timeout: config.ShutdownTimeout, Run: callbackServer.Shutdown},
		serviceruntime.ShutdownOperation{Name: "runtime workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

func technicalServer(lifecycle context.Context, config Config, unit, assistant *serviceruntime.Readiness, metrics *sharedobservability.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", readinessHandler(unit))
	mux.HandleFunc("/assistant/readyz", readinessHandler(assistant))
	mux.Handle("/metrics", metrics.PrometheusHandler())
	return &http.Server{Addr: config.TechnicalListen, Handler: mux, BaseContext: func(net.Listener) context.Context { return lifecycle },
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
}

func readinessHandler(readiness *serviceruntime.Readiness) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func monitorUnitReadiness(control *controlplaneclient.Client, manager *workload.Manager, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.InfrastructureCheckInterval)
		defer ticker.Stop()
		kubernetes := newKubernetesReadinessObserver()
		for {
			authorityCheck, cancelAuthority := context.WithTimeout(ctx, config.RequestTimeout)
			authorityErr := control.CheckLocalAuthority(authorityCheck)
			cancelAuthority()
			kubernetesCheck, cancelKubernetes := context.WithTimeout(ctx, config.RequestTimeout)
			kubernetesErr := manager.Check(kubernetesCheck)
			cancelKubernetes()
			kubernetesAvailable, kubernetesChanged, kubernetesDegraded := kubernetes.Observe(kubernetesErr, workload.AllowsLastKnownGoodObservation(kubernetesErr))
			if kubernetesChanged {
				if kubernetesDegraded {
					logger.WarnContext(ctx, "Kubernetes runtime observation degraded", "error_class", "kubernetes")
				} else {
					logger.InfoContext(ctx, "Kubernetes runtime observation restored")
				}
			}
			if authorityErr == nil && kubernetesAvailable {
				metrics.SetReady(true)
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "runtime readiness restored")
				}
			} else if readiness.Set(false, "local_infrastructure_unavailable") {
				metrics.SetReady(false)
				class := "kubernetes"
				if authorityErr != nil {
					class = "sidecar"
				}
				logger.WarnContext(ctx, "runtime readiness lost", "error_class", class)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func serveHTTP(server *http.Server, timeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer cancel()
			err := server.Shutdown(shutdown)
			serveErr := <-done
			if !errors.Is(serveErr, http.ErrServerClosed) {
				err = errors.Join(err, serveErr)
			}
			return err
		}
	}
}
