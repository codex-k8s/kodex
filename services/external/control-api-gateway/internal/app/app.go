// Package app содержит composition root web-first control-api-gateway.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	oidcauth "github.com/codex-k8s/matter-codex/libs/go/oidcverifier"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/security/session"
	httptransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http"
	websockettransport "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/websocket"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/usertext"
	"github.com/nats-io/nats.go"
)

const issuerUID, issuerGID = 29001, 29000

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
	metrics := sharedobservability.NewMetrics(serviceName, buildVersion, map[string]string{})
	metrics.SetReady(false)
	businessMetrics, err := internalobservability.New(metrics.Register)
	if err != nil {
		return err
	}
	oidc, err := oidcauth.New(startup, oidcauth.Config{Issuer: config.OIDCIssuer, Audience: config.OIDCAudience, JWKSURL: config.OIDCJWKSURL, ConnectAddress: config.OIDCConnectAddress, TLSServerName: config.OIDCTLSServerName, CAFile: config.OIDCCAFile, Timeout: config.RPCTimeout})
	if err != nil {
		return err
	}
	sessions, err := session.New(session.Config{CurrentKeyFile: config.SessionCurrentKeyFile, PreviousKeyFile: config.SessionPreviousKeyFile, TTL: config.SessionTTL})
	if err != nil {
		return err
	}
	limiter := ratelimit.New(ratelimit.Config{Window: config.RateWindow, Limit: config.RateLimit, MaximumKeys: config.MaximumRateKeys, PreAuthConcurrency: config.PreAuthConcurrency, GlobalHTTPConcurrency: config.MaximumHTTPConcurrency, PerSubjectHTTPConcurrency: config.PerSubjectHTTPConcurrency, GlobalWebSocketConcurrency: config.MaximumWebSocketConcurrency, PerSubjectWebSocketConcurrency: config.PerSubjectWebSocketConcurrency})
	security, err := boundary.New(boundary.Config{Origins: config.origins(), Verifier: oidc, Sessions: sessions, Limiter: limiter, Timeout: config.RequestTimeout})
	if err != nil {
		return err
	}
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneClientCertificateFile, ClientPrivateKeyFile: config.ControlPlaneClientPrivateKeyFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RPCTimeout, Operations: controlplaneclient.ControlAPIGatewayOperations(), ProjectRequiredOperations: controlplaneclient.ControlAPIGatewayProjectRequiredOperations(), UnaryClientInterceptor: telemetry.UnaryClientInterceptor(methodOperations())})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	bus, err := connectNATS(config, logger)
	if err != nil {
		return err
	}
	defer bus.Close()
	realtime, err := websockettransport.New(control, bus, config.origins())
	if err != nil {
		return err
	}
	texts, err := usertext.New()
	if err != nil {
		return err
	}
	api, err := httptransport.New(control, security, logger, texts)
	if err != nil {
		return err
	}
	api.AttachRealtime(http.HandlerFunc(realtime.ServeRunHTTP), http.HandlerFunc(realtime.ServePlatformHTTP))
	readiness := serviceruntime.NewReadiness()
	public := &http.Server{Addr: config.HTTPListen, Handler: secureHeaders(telemetry.HTTPMiddleware(internalobservability.Route, businessMetrics.ObserveHTTP, api.Handler())), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}, BaseContext: func(net.Listener) context.Context { return lifecycle }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: config.RequestTimeout, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	technicalMux := http.NewServeMux()
	technicalMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	technicalMux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	technicalMux.HandleFunc("/startupz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	technicalMux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		ready, reason := readiness.Ready()
		if !ready {
			http.Error(w, reason, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	technicalMux.Handle("/metrics", metrics.PrometheusHandler())
	technical := &http.Server{Addr: config.TechnicalListen, Handler: technicalMux, BaseContext: func(net.Listener) context.Context { return lifecycle }, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	readiness.Set(false, "dependencies_starting")
	workers := serviceruntime.StartWorkers(lifecycle, httpWorker(public, true, config), httpWorker(technical, false, config), readinessWorker(control, realtime, readiness, metrics, logger, config), oidcRefreshWorker(oidc, logger, config))
	err = workers.Wait(context.WithoutCancel(lifecycle))
	security.StopAdmission()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase, serviceruntime.ShutdownOperation{Name: "public HTTP server", Timeout: config.ShutdownTimeout, Run: public.Shutdown}, serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown}, serviceruntime.ShutdownOperation{Name: "gateway workers", Timeout: config.ShutdownTimeout, Run: workers.Wait}, serviceruntime.ShutdownOperation{Name: "tracing", Timeout: config.ShutdownTimeout, Run: telemetry.ShutdownTracing}, serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: config.ShutdownTimeout, Run: telemetry.FlushSentry})
	return errors.Join(err, shutdownErr)
}

func oidcRefreshWorker(verifier *oidcauth.Verifier, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		degraded := false
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				refresh, cancel := context.WithTimeout(ctx, config.RPCTimeout)
				err := verifier.Refresh(refresh)
				cancel()
				if err != nil && !degraded {
					degraded = true
					logger.WarnContext(ctx, "OIDC signing-key refresh degraded", "error_class", "identity_provider")
				} else if err == nil && degraded {
					degraded = false
					logger.InfoContext(ctx, "OIDC signing-key refresh restored")
				}
			}
		}
	}
}

func connectNATS(config Config, logger *slog.Logger) (*nats.Conn, error) {
	options := []nats.Option{nats.Name(serviceName), nats.Secure(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.NATSTLSServerName}), nats.RootCAs(config.NATSCAFile), nats.ClientCert(config.NATSCertificateFile, config.NATSPrivateKeyFile), nats.UserCredentials(config.NATSCredentialsFile), nats.NoEcho(), nats.Timeout(3 * time.Second), nats.ReconnectWait(time.Second), nats.MaxReconnects(-1), nats.PingInterval(20 * time.Second), nats.MaxPingsOutstanding(2), nats.DisconnectErrHandler(func(_ *nats.Conn, _ error) {
		logger.Warn("realtime NATS connection interrupted", "error_class", "dependency")
	}), nats.ReconnectHandler(func(_ *nats.Conn) { logger.Info("realtime NATS connection restored") })}
	connection, err := nats.Connect(config.NATSURL, options...)
	if err != nil {
		return nil, errors.New("connect realtime NATS consumer")
	}
	if err := connection.FlushTimeout(2 * time.Second); err != nil {
		connection.Close()
		return nil, errors.New("verify realtime NATS consumer")
	}
	return connection, nil
}

func httpWorker(server *http.Server, tlsEnabled bool, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() {
			if tlsEnabled {
				done <- server.ListenAndServeTLS(config.TLSCertificateFile, config.TLSPrivateKeyFile)
			} else {
				done <- server.ListenAndServe()
			}
		}()
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
func readinessWorker(control *controlplaneclient.Client, realtime *websockettransport.Server, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.RPCTimeout)
			err := errors.Join(control.CheckLocalAuthority(check), realtime.Check(check))
			cancel()
			if err == nil {
				changed := readiness.Set(true, "ready")
				metrics.SetReady(true)
				if changed {
					logger.InfoContext(ctx, "control API readiness restored")
				}
			} else {
				changed := readiness.Set(false, "direct_infrastructure_unavailable")
				metrics.SetReady(false)
				if changed {
					logger.WarnContext(ctx, "control API readiness lost", "error_class", "direct_infrastructure")
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
func methodOperations() map[string]string {
	result := map[string]string{}
	for operation, method := range controlplaneclient.ControlAPIGatewayOperations() {
		result[method] = operation
	}
	return result
}
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
