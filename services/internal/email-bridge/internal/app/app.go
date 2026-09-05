package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/authority"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	business "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/observability/metrics"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/netutil"
)

func Run(ctx, background context.Context, version string) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	c, e := loadConfig()
	if e != nil {
		return e
	}
	raw, e := securefile.Read(c.ConfigurationFile, 1<<20)
	if e != nil {
		return errors.New("email configuration unavailable")
	}
	var configuration api.Configuration
	if api.Decode(raw, &configuration) != nil || api.ValidateConfiguration(configuration) != nil {
		return errors.New("email configuration invalid")
	}
	transportTLS, e := tlsConfig(c)
	if e != nil {
		return e
	}
	dsn, e := databaseDSN(c.DSNFile)
	if e != nil {
		return e
	}
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return errors.New("database unavailable")
	}
	defer pool.Close()
	repository := &receipt.Repository{Pool: pool}
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	e = repository.Ready(startup)
	if e == nil {
		e = repository.Configuration(startup, configuration, api.Digest(configuration))
	}
	cancel()
	if e != nil {
		return errors.New("database schema unavailable")
	}
	telemetry, e := observability.NewRuntime(ctx, observability.RuntimeConfig{ServiceName: "email-bridge", ServiceVersion: version, Environment: c.Environment, OTLPEndpoint: c.OTLPEndpoint, OTLPTLSServerName: c.OTLPServerName, OTLPCAFile: c.OTLPCAFile, TraceSampleRatio: 0.1})
	if e != nil {
		return errors.New("telemetry unavailable")
	}
	defer serviceruntime.RunShutdown(background, serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing})
	metrics := observability.NewMetrics("email_bridge", version, nil)
	businessMetrics := business.New()
	if metrics.Register(businessMetrics.Operations) != nil {
		return errors.New("metrics unavailable")
	}
	clientTLS := transportTLS.Clone()
	clientTLS.ClientAuth = tls.NoClientCert
	clientTLS.ServerName = "control-plane.kodex-system.svc.cluster.local"
	clientHTTP := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: clientTLS, MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 16}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect forbidden") }}
	defer clientHTTP.CloseIdleConnections()
	client, e := api.NewClient(c.AuthorityURL, api.WithHTTPClient(clientHTTP))
	if e != nil {
		return errors.New("authority client invalid")
	}
	service := &mail.Service{CompletionBase: background, Config: configuration, Authority: &authority.Client{API: client, BearerFile: c.AuthorityBearerFile}, Provider: &mailtransport.Provider{Secrets: mailtransport.Files{Root: c.SecretsRoot}, Dialer: mailtransport.Tunnel{Address: c.EgressAddress}}, Receipts: repository}
	readiness := serviceruntime.NewReadiness()
	tech, e := httpserver.New(httpserver.Config{Address: c.Technical, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 16384, MaximumConnections: 64}, readiness, metrics.PrometheusHandler())
	if e != nil {
		return e
	}
	if e = tech.Listen(); e != nil {
		return e
	}
	defer serviceruntime.RunShutdown(background, serviceruntime.ShutdownOperation{Name: "technical", Timeout: 5 * time.Second, Run: tech.Shutdown})
	handler := telemetry.HTTPMiddleware(func(path string) string {
		switch path {
		case "/v1/health", "/v1/messages", "/v1/mailbox-operations":
			return path
		default:
			return "/other"
		}
	}, func(route string, status int, _ time.Time) {
		if status >= 500 {
			slog.Error("Email bridge request failed", "route", route, "status", status)
		}
	}, httptransport.Handler{Service: service, Metrics: businessMetrics})
	server := &http.Server{Handler: http.MaxBytesHandler(handler, 24<<20), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 70 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, TLSConfig: transportTLS, BaseContext: func(net.Listener) context.Context { return ctx }}
	listener, e := net.Listen("tcp", c.Listen)
	if e != nil {
		return errors.New("HTTPS listener unavailable")
	}
	group := serviceruntime.StartWorkers(ctx, func(context.Context) error {
		e := tech.Serve()
		if e != nil {
			stop()
		}
		return e
	}, func(context.Context) error {
		e := server.Serve(tls.NewListener(netutil.LimitListener(listener, 64), transportTLS))
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		if e != nil {
			stop()
		}
		return e
	}, func(worker context.Context) error {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			probe, stop := context.WithTimeout(worker, 10*time.Second)
			ok := repository.Ready(probe) == nil
			token, e := securefile.Read(c.HealthTokenFile, 16384)
			if e != nil {
				ok = false
			}
			active := 0
			for _, m := range configuration.Mailboxes {
				if m.Enabled {
					active++
					if result, err := service.Execute(probe, "spiffe://kodex.local/ns/kodex-system/sa/email-bridge", string(token), api.Command{Operation: api.OperationHealth, MailboxId: m.Id}); err != nil || result.Status != "ready" {
						ok = false
					}
				}
			}
			stop()
			ok = ok && active > 0
			readiness.Set(ok, "dependencies")
			metrics.SetReady(ok)
			select {
			case <-worker.Done():
				return nil
			case <-ticker.C:
			}
		}
	})
	<-ctx.Done()
	readiness.Set(false, "stopping")
	return serviceruntime.RunShutdown(background, serviceruntime.ShutdownOperation{Name: "https", Timeout: 10 * time.Second, Run: server.Shutdown}, serviceruntime.ShutdownOperation{Name: "technical", Timeout: 5 * time.Second, Run: tech.Shutdown}, serviceruntime.ShutdownOperation{Name: "workers", Timeout: 10 * time.Second, Run: func(shutdown context.Context) error { group.Stop(); return group.Wait(shutdown) }})
}
