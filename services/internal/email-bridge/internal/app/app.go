package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/authority"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/reconciliation"
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
	if metrics.Register(businessMetrics.Operations) != nil || metrics.Register(businessMetrics.Reconciliations) != nil {
		return errors.New("metrics unavailable")
	}
	client, e := controlplaneclient.Dial(ctx, controlplaneclient.Config{Target: c.AuthorityTarget, TLSServerName: "control-plane.kodex-system.svc.cluster.local", CAFile: c.CAFile, ClientCertificateFile: c.CertificateFile, ClientPrivateKeyFile: c.PrivateKeyFile, ApplicationGrantFile: c.ApplicationGrantFile, ExpectedIssuerUID: 29001, ExpectedIssuerGID: 29000, DialTimeout: 3 * time.Second, Operations: controlplaneclient.EmailBridgeOperations()})
	if e != nil {
		return errors.New("authority client invalid")
	}
	defer client.Close()
	owner := &authority.Client{API: client.Runtime}
	service := &mail.Service{Ledger: repository, CompletionBase: background, Config: configuration, Authority: owner, Effects: owner, Provider: &mailtransport.Provider{Secrets: mailtransport.Files{Root: c.SecretsRoot}, Dialer: mailtransport.Tunnel{Address: c.EgressAddress}}, Receipts: repository}
	reconciler := &reconciliation.Service{Repository: repository, Authority: owner, Interval: time.Duration(c.ReconciliationIntervalSeconds) * time.Second, Batch: c.ReconciliationBatch, Observer: businessMetrics, Barrier: func(probe context.Context) error {
		if err := repository.Ready(probe); err != nil {
			return err
		}
		if err := repository.Configuration(probe, configuration, api.Digest(configuration)); err != nil {
			return err
		}
		return client.CheckLocalAuthority(probe)
	}}
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
	group := serviceruntime.StartWorkers(ctx, reconciler.Run, func(context.Context) error {
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
			ok := repository.Ready(probe) == nil && client.CheckLocalAuthority(probe) == nil && repository.Configuration(probe, configuration, api.Digest(configuration)) == nil
			stop()
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
