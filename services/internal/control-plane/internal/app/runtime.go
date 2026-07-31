package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/credentials"
)

const (
	maximumSecretFileBytes = 64 << 10
	maximumCAFileBytes     = 1 << 20
)

type appTelemetry struct {
	*observability.Runtime
	traceOnce  sync.Once
	sentryOnce sync.Once
	traceErr   error
	sentryErr  error
}

func startTelemetry(
	ctx context.Context,
	buildVersion string,
) (*appTelemetry, *slog.Logger, error) {
	config, err := observability.RuntimeConfigFromEnv(serviceName, buildVersion)
	if err != nil {
		return nil, nil, err
	}
	runtime, err := observability.NewRuntime(ctx, config)
	if err != nil {
		return nil, nil, err
	}
	return &appTelemetry{Runtime: runtime}, runtime.Logger(os.Stdout), nil
}

func (telemetry *appTelemetry) shutdownTracing(ctx context.Context) error {
	telemetry.traceOnce.Do(func() {
		telemetry.traceErr = telemetry.ShutdownTracing(ctx)
	})
	return telemetry.traceErr
}

func (telemetry *appTelemetry) flushSentry(ctx context.Context) error {
	telemetry.sentryOnce.Do(func() {
		telemetry.sentryErr = telemetry.FlushSentry(ctx)
	})
	return telemetry.sentryErr
}

func openPostgres(
	ctx context.Context,
	dsnFile string,
	caFile string,
	serverName string,
	maxConnections int32,
) (*pgxpool.Pool, error) {
	dsn, err := readSecret(dsnFile, maximumSecretFileBytes)
	if err != nil {
		return nil, errors.New("read PostgreSQL DSN")
	}
	config, err := pgxpool.ParseConfig(strings.TrimSpace(string(dsn)))
	if err != nil {
		return nil, errors.New("parse PostgreSQL DSN")
	}
	if len(config.ConnConfig.Fallbacks) != 0 ||
		config.ConnConfig.Host != serverName ||
		config.ConnConfig.TLSConfig == nil ||
		config.ConnConfig.TLSConfig.ServerName != serverName ||
		config.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("PostgreSQL DSN must use exact verify-full TLS")
	}
	roots, err := loadCertPool(caFile)
	if err != nil {
		return nil, errors.New("load PostgreSQL CA")
	}
	config.ConnConfig.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: serverName,
		RootCAs:    roots,
	}
	config.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithDisableConnectionDetailsInAttributes(),
		otelpgx.WithDisableSQLStatementInAttributes(),
		otelpgx.WithSpanNameCtxFunc(func(context.Context, string) string {
			return serviceName + ".postgresql"
		}),
	)
	config.MaxConns = maxConnections
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("open PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("check PostgreSQL pool")
	}
	return pool, nil
}

func serverCredentials(config Config) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(
		config.ServerCertificateFile,
		config.ServerPrivateKeyFile,
	)
	if err != nil {
		return nil, errors.New("load control-plane server identity")
	}
	clientRoots, err := loadCertPool(config.ClientCAFile)
	if err != nil {
		return nil, errors.New("load internal client CA")
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientRoots,
	}), nil
}

func loadCertPool(path string) (*x509.CertPool, error) {
	raw, err := readSecret(path, maximumCAFileBytes)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("certificate bundle is invalid")
	}
	return pool, nil
}

func readSecret(path string, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("runtime secret path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() ||
		info.Size() <= 0 || info.Size() > maximum ||
		info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("runtime secret file is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open runtime secret file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime secret file")
	}
	return raw, nil
}

func technicalServer(
	address string,
	metrics *observability.Metrics,
	ready func() bool,
	logger *slog.Logger,
) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.PrometheusHandler())
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready() {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte("not ready\n"))
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ready\n"))
	})
	return &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func closeWithLabel(label string, close func() error) error {
	if err := close(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}
