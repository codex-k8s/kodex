// Package app owns control API process composition, startup barriers and bounded
// worker/HTTP shutdown without implementing domain authority decisions.
// Пакет app владеет композицией процесса control API, startup barriers и ограниченным
// завершением workers и HTTP без реализации доменных решений о полномочиях.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/libs/go/eventing/sessionrevocation"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/observability"
	secretbrokerclient "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/secretbroker"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/sttclient"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	websockettransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/usertext"
	"github.com/nats-io/nats.go"
)

// issuerUID and issuerGID pin the expected local authorization issuer's peer credentials passed to all internal clients; these numeric socket-identity checks do not grant a browser user application permission.
// issuerUID и issuerGID закрепляют ожидаемые peer credentials локального издателя полномочий, передаваемые всем внутренним клиентам; числовые проверки socket identity не выдают browser user прикладное разрешение.
const issuerUID, issuerGID = 29001, 29000

// Run loads configuration, constructs telemetry, identity/session/RPC/NATS dependencies and checks the local infrastructure barrier before starting public/technical HTTP and readiness/refresh workers. On the normal worker-stop path it drains requests, cancels serving contexts and closes resources in the declared order; startup errors return at their existing cleanup points.
// Run загружает конфигурацию, создаёт telemetry, identity, session, RPC и NATS dependencies и проверяет local infrastructure barrier до запуска public и technical HTTP и readiness и refresh workers. При штатном выходе workers завершаются запросы, отменяются serving contexts и ресурсы закрываются в объявленном порядке; startup errors возвращаются в прежних точках cleanup.
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
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{RPCProfile: config.RPCProfile, CallerWorkload: "control-api-gateway", ServiceIdentity: true, Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneClientCertificateFile, ClientPrivateKeyFile: config.ControlPlaneClientPrivateKeyFile, ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RPCTimeout, Operations: controlplaneclient.ControlAPIGatewayOperations(), ProofOperations: authorityProofOperations(), ProjectRequiredOperations: authorityProjectRequiredOperations(), UnaryClientInterceptor: telemetry.UnaryClientInterceptor(methodOperations())})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	speech, err := sttclient.Dial(startup, sttclient.Config{
		RPCProfile: config.RPCProfile,
		Target:     config.STTTarget, TLSServerName: config.STTTLSServerName, CAFile: config.STTCAFile,
		ClientCertificateFile: config.STTClientCertificateFile, ClientPrivateKeyFile: config.STTClientPrivateKeyFile,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, DialTimeout: config.RPCTimeout, Proofs: control,
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, speech.Close()) }()
	secrets, err := secretbrokerclient.Dial(startup, secretbrokerclient.Config{
		RPCProfile: config.RPCProfile,
		Target:     config.SecretBrokerTarget, TLSServerName: config.SecretBrokerTLSServerName, CAFile: config.SecretBrokerCAFile,
		ClientCertificateFile: config.SecretBrokerClientCertificateFile, ClientPrivateKeyFile: config.SecretBrokerClientPrivateKeyFile,
		DialTimeout: config.RPCTimeout, RequestTimeout: config.RPCTimeout,
		ExpectedIssuerUID: issuerUID, ExpectedIssuerGID: issuerGID, Proofs: control,
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, secrets.Close()) }()
	bus, err := connectNATS(config, logger)
	if err != nil {
		return err
	}
	defer bus.Close()
	revocations, err := sessionrevocation.New(startup, bus, config.NATSReplicas, config.RPCTimeout)
	if err != nil {
		return err
	}
	browserState, err := browserstate.New(startup, bus, config.NATSReplicas, config.RPCTimeout)
	if err != nil {
		return err
	}
	browserOIDC, err := oidc.BrowserClient("kodex-control-center", config.OIDCBrowserOrigin+"/auth/callback")
	if err != nil {
		return err
	}
	families, err := session.NewFamilies(lifecycle, browserState, browserOIDC, sessions)
	if err != nil {
		return err
	}
	logins, err := session.NewLogins(lifecycle, browserState, browserOIDC, sessions)
	if err != nil {
		return err
	}
	security, err := boundary.New(boundary.Config{BrowserOrigin: config.OIDCBrowserOrigin, Logins: logins, Families: families, Origins: config.origins(), Verifier: oidc, Sessions: sessions, Revocations: revocations, Limiter: limiter, Timeout: config.RequestTimeout})
	if err != nil {
		return err
	}
	legacyUntil, err := config.legacyWebSocketDeadline(time.Now().UTC())
	if err != nil {
		return err
	}
	realtime, err := websockettransport.New(control, bus, speech.Speech, config.origins(), security, legacyUntil)
	if err != nil {
		return err
	}
	if err := errors.Join(
		realtime.Check(startup),
		revocations.Check(startup),
		browserState.Check(startup),
	); err != nil {
		return errors.Join(errors.New("control API local infrastructure startup barrier failed"), err)
	}
	texts, err := usertext.New()
	if err != nil {
		return err
	}
	api, err := httptransport.New(control, security, logger, texts)
	if err != nil {
		return err
	}
	if err := api.AttachSecretBroker(secrets.SecretBroker); err != nil {
		return err
	}
	if err := api.AttachSecretDraftBroker(secrets.Drafts); err != nil {
		return err
	}
	if err := api.AttachSpeechToText(speech.Speech); err != nil {
		return err
	}
	api.AttachRealtime(http.HandlerFunc(realtime.ServeSessionHTTP))
	readiness := serviceruntime.NewReadiness()
	requests, cancelRequests := servingContext(lifecycle)
	defer cancelRequests()
	public := &http.Server{Addr: config.HTTPListen, Handler: secureHeaders(telemetry.HTTPMiddleware(internalobservability.Route, businessMetrics.ObserveHTTP, api.Handler())), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}, BaseContext: func(net.Listener) context.Context { return requests }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: config.RequestTimeout, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	technicalMux := httpserver.ProbeMux(readiness.Ready)
	technicalMux.Handle("/metrics", metrics.PrometheusHandler())
	technical := &http.Server{Addr: config.TechnicalListen, Handler: technicalMux, BaseContext: func(net.Listener) context.Context { return lifecycle }, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	readiness.Set(false, "dependencies_starting")
	workers := serviceruntime.StartWorkers(lifecycle, httpWorker(public, true, config), httpWorker(technical, false, config), readinessWorker(realtime, revocations, browserState, readiness, metrics, logger, config), oidcRefreshWorker(oidc, logger, config))
	err = workers.Wait(context.WithoutCancel(lifecycle))
	// HTTP-запросы уже завершены либо принудительно закрыты по бюджету.
	// Отмена закрывает также контексты hijacked WebSocket до закрытия клиентов.
	cancelRequests()
	security.StopAdmission()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase, serviceruntime.ShutdownOperation{Name: "public HTTP server", Timeout: config.ShutdownTimeout, Run: public.Shutdown}, serviceruntime.ShutdownOperation{Name: "technical HTTP server", Timeout: config.ShutdownTimeout, Run: technical.Shutdown}, serviceruntime.ShutdownOperation{Name: "gateway workers", Timeout: config.ShutdownTimeout, Run: workers.Wait}, serviceruntime.ShutdownOperation{Name: "tracing", Timeout: config.ShutdownTimeout, Run: telemetry.ShutdownTracing}, serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: config.ShutdownTimeout, Run: telemetry.FlushSentry})
	return errors.Join(err, shutdownErr)
}

// oidcRefreshWorker periodically refreshes OIDC signing keys under the RPC timeout, logs only transitions into/out of degraded state and returns context cancellation. It does not log token contents, refresh browser sessions or change readiness directly.
// oidcRefreshWorker периодически обновляет OIDC signing keys с RPC timeout, пишет лишь переходы в degraded state и обратно и возвращает отмену контекста. Содержимое токенов не логируется, browser sessions не обновляются и readiness напрямую не меняется.
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

// connectNATS opens the configured TLS 1.3 NATS connection with pinned server name/CA, client certificate and user credentials, unlimited reconnects and bounded connect/flush checks. Failed initial flush closes the connection; the helper has no lifecycle context and does not create subscriptions.
// connectNATS открывает настроенное TLS 1.3 NATS-соединение с закреплёнными server name и CA, client certificate и user credentials, неограниченными reconnects и ограниченными connect и flush checks. Отказ первого flush закрывает соединение; lifecycle context не передаётся и subscriptions не создаются.
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

// httpWorker runs the supplied HTTP or TLS server, treats ErrServerClosed as normal and on cancellation performs bounded drain with forced Close on failure before joining Serve. It leaves hijacked-connection cancellation to the process-owned serving context.
// httpWorker запускает переданный HTTP либо TLS server, считает ErrServerClosed штатным и при отмене выполняет ограниченный drain с принудительным Close при отказе до join Serve. Отмена hijacked connections остаётся у принадлежащего процессу serving context.
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
			err := shutdownHTTPServer(ctx, server, config.ShutdownTimeout)
			serveErr := <-done
			if !errors.Is(serveErr, http.ErrServerClosed) {
				err = errors.Join(err, serveErr)
			}
			return err
		}
	}
}

// readinessWorker immediately and periodically checks realtime, revocation and browser-state dependencies with one bounded context, updates readiness/metrics and logs state transitions. Exit marks both unready; it does not itself probe every provider or issue a synthetic user request.
// readinessWorker немедленно и периодически проверяет realtime, revocation и browser-state dependencies одним ограниченным контекстом, обновляет readiness и metrics и логирует переходы. При выходе обе готовности снимаются; каждый provider не опрашивается и синтетический user request не отправляется.
func readinessWorker(realtime *websockettransport.Server, revocations *sessionrevocation.Store, browserState *browserstate.Store, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		defer func() {
			readiness.Set(false, "shutting_down")
			metrics.SetReady(false)
		}()
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.RPCTimeout)
			err := errors.Join(realtime.Check(check), revocations.Check(check), browserState.Check(check))
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

// methodOperations builds a new method-to-operation map by reversing the control-plane gateway registry for telemetry. A repeated method would overwrite the earlier entry; this mapping is not an authorization decision.
// methodOperations строит новую карту method к operation обращением control-plane gateway registry для telemetry. Повторный method перезаписал бы прежнюю entry; карта не является решением авторизации.
func methodOperations() map[string]string {
	result := map[string]string{}
	for operation, method := range controlplaneclient.ControlAPIGatewayOperations() {
		result[method] = operation
	}
	return result
}

// authorityProofOperations starts with the control-plane operation map and copies direct STT then secret-draft broker operations into it. The result selects exact proof targets without routing those direct operations through control-plane.
// authorityProofOperations начинает с карты control-plane operations и копирует direct STT, затем secret-draft broker operations. Результат выбирает точные proof targets без маршрутизации этих прямых операций через control-plane.
func authorityProofOperations() map[string]string {
	result := controlplaneclient.ControlAPIGatewayOperations()
	maps.Copy(result, controlplaneclient.STTGatewayOperations())
	maps.Copy(result, controlplaneclient.SecretDraftGatewayOperations())
	return result
}

// authorityProjectRequiredOperations returns the canonical gateway operation set requiring a project locator for authority proof construction; resource-owned/organization scope is not inferred by this forwarding helper.
// authorityProjectRequiredOperations возвращает канонический набор gateway operations, требующих project locator для построения authority proof; resource-owned и organization scope этим перенаправляющим helper не выводятся.
func authorityProjectRequiredOperations() map[string]struct{} {
	return controlplaneclient.ControlAPIGatewayProjectRequiredOperations()
}

// secureHeaders sets nosniff, no-referrer, same-origin microphone-only permissions and restrictive API CSP before calling the next handler. The downstream handler can still replace headers; this wrapper does not authenticate requests or implement CORS.
// secureHeaders задаёт nosniff, no-referrer, разрешение микрофона лишь same-origin и ограничительный API CSP до вызова следующего handler. Последующий handler может заменить заголовки; wrapper не аутентифицирует запросы и не реализует CORS.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(self), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
