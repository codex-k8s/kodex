// Package app содержит единственный composition root control-api-gateway.
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
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	oidcauth "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/authorization/oidc"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/publictls"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/session"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	websockettransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/websocket"
)

const (
	issuerUID = 29001
	issuerGID = 29000
)

type runtimeState struct {
	config    Config
	telemetry *sharedobservability.Runtime
	logger    *slog.Logger
	metrics   *sharedobservability.Metrics
	readiness *serviceruntime.Readiness
	oidc      *oidcauth.Verifier
	control   *controlplaneclient.Client
	public    *http.Server
	technical *http.Server
	workers   *serviceruntime.WorkerGroup
	security  *boundary.Boundary
	realtime  *websockettransport.Server
	publicTLS *publictls.Manager
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
	state.metrics = sharedobservability.NewMetrics(serviceName, buildVersion, map[string]string{})
	state.metrics.SetReady(false)
	businessMetrics, err := internalobservability.New(state.metrics.Register)
	if err != nil {
		return err
	}
	state.oidc, err = oidcauth.New(startup, oidcauth.Config{Issuer: config.OIDCIssuer, Audience: config.OIDCAudience, TLSServerName: config.OIDCTLSServerName, CAFile: config.OIDCCAFile, Timeout: config.RPCTimeout})
	if err != nil {
		return err
	}
	sessions, err := session.New(session.Config{CurrentKeyFile: config.SessionCurrentKeyFile, PreviousKeyFile: config.SessionPreviousKeyFile, TTL: config.SessionTTL})
	if err != nil {
		return err
	}
	limiter := ratelimit.New(ratelimit.Config{
		Window: config.RateWindow, Limit: config.RateLimit, MaximumKeys: config.MaximumRateKeys,
		PreAuthConcurrency: config.PreAuthConcurrency, GlobalHTTPConcurrency: config.MaximumHTTPConcurrency,
		PerSubjectHTTPConcurrency: config.PerSubjectHTTPConcurrency, GlobalWebSocketConcurrency: config.MaximumWebSocketConcurrency,
		PerSubjectWebSocketConcurrency: config.PerSubjectWebSocketConcurrency,
	})
	security, err := boundary.New(boundary.Config{Origins: config.origins(), Verifier: state.oidc, Sessions: sessions, Limiter: limiter, Timeout: config.RequestTimeout})
	if err != nil {
		return err
	}
	state.control, err = controlplaneclient.Dial(startup, controlplaneclient.Config{
		Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneClientCertificateFile, ClientPrivateKeyFile: config.ControlPlaneClientPrivateKeyFile,
		ApplicationGrantFile: config.ControlPlaneApplicationGrantFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID,
		DialTimeout: config.RPCTimeout, Operations: controlplaneclient.ControlAPIGatewayOperations(),
		UnaryClientInterceptor: state.telemetry.UnaryClientInterceptor(controlPlaneMethodOperations()),
	})
	if err != nil {
		return err
	}
	state.publicTLS, err = publictls.New(publictls.Config{CertificateFile: config.TLSCertificateFile, PrivateKeyFile: config.TLSPrivateKeyFile, CAFile: config.PublicTLSCAFile, StateFile: config.PublicTLSStateFile, ServerName: config.PublicTLSServerName})
	if err != nil {
		return err
	}
	if err := state.publicTLS.Admit(startup, state.control.ControlPlane); err != nil {
		return err
	}
	if err := state.control.Check(startup); err != nil {
		return err
	}
	state.readiness.Set(false, "listener_starting")
	state.metrics.SetReady(false)
	state.security = security
	httpAPI, err := httptransport.New(state.control.ControlPlane, security, state.logger)
	if err != nil {
		return err
	}
	state.realtime, err = websockettransport.New(state.control.ControlPlane, security, businessMetrics, state.logger, config.origins(), config.RealtimePollInterval, config.RPCTimeout)
	if err != nil {
		return err
	}
	httpAPI.AttachRealtime(state.realtime)
	baseContext := func(net.Listener) context.Context { return lifecycle }
	state.public = &http.Server{Addr: config.HTTPListen, Handler: secureHeaders(state.telemetry.HTTPMiddleware(internalobservability.Route, businessMetrics.ObserveHTTP, httpAPI.Handler())), TLSConfig: state.publicTLS.TLSConfig(), BaseContext: baseContext, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: config.RequestTimeout, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	technicalMux := http.NewServeMux()
	technicalMux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	technicalMux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		ready, _ := state.readiness.Ready()
		if !ready {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	technicalMux.Handle("/metrics", state.metrics.PrometheusHandler())
	state.technical = &http.Server{Addr: config.TechnicalListen, Handler: state.telemetry.HTTPMiddleware(internalobservability.Route, businessMetrics.ObserveHTTP, technicalMux), BaseContext: baseContext, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	state.workers = serviceruntime.StartWorkers(
		lifecycle,
		httpWorker(state.public, true, config.ShutdownTimeout),
		httpWorker(state.technical, false, config.ShutdownTimeout),
		readinessWorker(state.control, state.publicTLS, config.HTTPListen, state.readiness, state.metrics, state.logger, config.ReadinessInterval, config.RPCTimeout),
		admissionWorker(security, state.realtime, config.ShutdownTimeout),
	)
	if err := state.workers.Wait(context.WithoutCancel(lifecycle)); err != nil {
		return err
	}
	return nil
}

func httpWorker(server *http.Server, tlsEnabled bool, shutdownTimeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		result := make(chan error, 1)
		go func() {
			if tlsEnabled {
				result <- server.ListenAndServeTLS("", "")
			} else {
				result <- server.ListenAndServe()
			}
		}()
		select {
		case err := <-result:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
			err := server.Shutdown(shutdown)
			cancel()
			serveErr := <-result
			if !errors.Is(serveErr, http.ErrServerClosed) {
				err = errors.Join(err, serveErr)
			}
			return err
		}
	}
}

func readinessWorker(control *controlplaneclient.Client, publicTLS *publictls.Manager, listen string, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, interval, timeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, timeout)
			err := errors.Join(
				publicTLS.Admit(check, control.ControlPlane),
				control.Check(check),
				publicTLS.VerifyServed(check, listen),
			)
			cancel()
			if err != nil {
				readiness.Set(false, "protected_path_unavailable")
				metrics.SetReady(false)
				logger.WarnContext(ctx, "control API protected readiness path is unavailable", "error_class", "dependency")
			} else {
				readiness.Set(true, "ready")
				metrics.SetReady(true)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func admissionWorker(security *boundary.Boundary, realtime *websockettransport.Server, timeout time.Duration) serviceruntime.Worker {
	return func(ctx context.Context) error {
		<-ctx.Done()
		security.StopAdmission()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		return realtime.Shutdown(shutdown)
	}
}

func controlPlaneMethodOperations() map[string]string {
	result := make(map[string]string)
	for operation, method := range controlplaneclient.ControlAPIGatewayOperations() {
		result[method] = operation
	}
	return result
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(writer, request)
	})
}

func (state *runtimeState) shutdown(base context.Context) error {
	if state.readiness != nil {
		state.readiness.Set(false, "stopping")
	}
	if state.metrics != nil {
		state.metrics.SetReady(false)
	}
	if state.security != nil {
		state.security.StopAdmission()
	}
	if state.workers != nil {
		state.workers.Stop()
	}
	var result error
	if state.workers != nil {
		result = errors.Join(result, serviceruntime.RunShutdown(base, serviceruntime.ShutdownOperation{Name: "workers", Timeout: state.config.ShutdownTimeout, Run: state.workers.Wait}))
	}
	if state.control != nil {
		result = errors.Join(result, state.control.Close())
	}
	if state.oidc != nil {
		state.oidc.Close()
	}
	if state.telemetry != nil {
		result = errors.Join(result, serviceruntime.RunShutdown(
			base,
			serviceruntime.ShutdownOperation{Name: "tracing", Timeout: state.config.ShutdownTimeout, Run: state.telemetry.ShutdownTracing},
			serviceruntime.ShutdownOperation{Name: "sentry", Timeout: state.config.ShutdownTimeout, Run: state.telemetry.FlushSentry},
		))
	}
	return result
}
