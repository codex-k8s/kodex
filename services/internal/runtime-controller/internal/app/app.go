// Package app собирает always-hot provider-neutral runtime.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/provider"
)

const (
	serviceName = "runtime-controller"
	issuerUID   = 29001
	issuerGID   = 29000
)

func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})).With("service", serviceName, "version", buildVersion)
	startup, cancelStartup := context.WithTimeout(lifecycle, config.RequestTimeout)
	defer cancelStartup()
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneClientCertificateFile, ClientPrivateKeyFile: config.ControlPlaneClientPrivateKeyFile,
		ApplicationGrantFile: config.ControlPlaneApplicationGrantFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RequestTimeout, Operations: controlplaneclient.RuntimeOperations(),
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	model, err := provider.NewResponses(config.ProviderProxy, config.ProviderCredentialFile)
	if err != nil {
		return err
	}
	defer model.CloseIdleConnections()
	unitReadiness := serviceruntime.NewReadiness()
	assistantReadiness := serviceruntime.NewReadiness()
	runtime := newRuntime(control, model, config, assistantReadiness, logger)
	technical := technicalServer(lifecycle, config, unitReadiness)
	workers := serviceruntime.StartWorkers(
		lifecycle,
		runtime.Run,
		monitorUnitReadiness(control, unitReadiness, logger, config),
		serveHTTP(technical, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "runtime workers", Timeout: config.ShutdownTimeout, Run: workers.Wait},
	)
	return errors.Join(err, shutdownErr)
}

func technicalServer(lifecycle context.Context, config Config, readiness *serviceruntime.Readiness) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/startupz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(writer, reason, http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	return &http.Server{Addr: config.TechnicalListen, Handler: mux, BaseContext: func(net.Listener) context.Context { return lifecycle }, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
}

func monitorUnitReadiness(control *controlplaneclient.Client, readiness *serviceruntime.Readiness, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.RequestTimeout)
			err := control.CheckLocalAuthority(check)
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "runtime readiness restored")
				}
			} else if readiness.Set(false, "local_authority_unavailable") {
				logger.WarnContext(ctx, "runtime readiness lost", "error_class", "sidecar")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func serveHTTP(server *http.Server, config Config) serviceruntime.Worker {
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
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownTimeout)
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
