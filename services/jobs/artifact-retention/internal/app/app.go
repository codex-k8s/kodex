// Package app содержит composition root artifact-retention.
package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	retentionpostgres "github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/repository/postgres"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/retention"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maximumSecretBytes = 16 << 10

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
	retentionMetrics := newRetentionMetrics()
	if err := metrics.Register(retentionMetrics.collectors()...); err != nil {
		return errors.New("register artifact-retention metrics")
	}
	pool, err := openPostgres(startup, config)
	if err != nil {
		return err
	}
	objects, err := openObjectStorage(startup, config)
	if err != nil {
		pool.Close()
		return err
	}
	if err := objects.Check(startup); err != nil {
		pool.Close()
		return errors.New("artifact-retention object storage startup barrier failed")
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	technical, err := httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 32 << 10, MaximumConnections: 128,
	}, readiness, metrics.PrometheusHandler())
	if err != nil {
		pool.Close()
		return err
	}
	if err := technical.Listen(); err != nil {
		pool.Close()
		return err
	}
	processor := retention.NewProcessor(retentionpostgres.New(pool), objects)
	workers := serviceruntime.StartWorkers(
		lifecycle,
		serveTechnical(technical),
		monitorReadiness(pool, objects, readiness, metrics, logger, config),
		runRetentionLoop(processor, retentionMetrics, logger, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	metrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "retention workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "PostgreSQL", Timeout: config.ShutdownTimeout / 4, Run: func(context.Context) error { pool.Close(); return nil }},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 4, Run: technical.Shutdown},
		serviceruntime.ShutdownOperation{Name: "tracing", Timeout: 5 * time.Second, Run: telemetry.ShutdownTracing},
		serviceruntime.ShutdownOperation{Name: "error reporting", Timeout: 5 * time.Second, Run: telemetry.FlushSentry},
	)
	return errors.Join(err, shutdownErr)
}

func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	rawDSN, err := securefile.Read(config.PostgresDSNFile, maximumSecretBytes)
	if err != nil {
		return nil, errors.New("load artifact-retention PostgreSQL configuration")
	}
	poolConfig, parseErr := pgxpool.ParseConfig(strings.TrimSpace(string(rawDSN)))
	clear(rawDSN)
	if parseErr != nil {
		return nil, errors.New("parse artifact-retention PostgreSQL configuration")
	}
	ca, err := securefile.Read(config.PostgresCAFile, maximumSecretBytes)
	if err != nil {
		return nil, errors.New("load artifact-retention PostgreSQL CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse artifact-retention PostgreSQL CA")
	}
	poolConfig.ConnConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: config.PostgresTLSServerName}
	poolConfig.MaxConns = config.PostgresMaxConnections
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open artifact-retention PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify artifact-retention PostgreSQL connection")
	}
	return pool, nil
}

func openObjectStorage(ctx context.Context, config Config) (*s3store.Store, error) {
	accessKey, err := securefile.Read(config.ObjectStorageAccessKeyFile, maximumSecretBytes)
	if err != nil {
		return nil, errors.New("load artifact-retention object storage access key")
	}
	secretKey, err := securefile.Read(config.ObjectStorageSecretKeyFile, maximumSecretBytes)
	if err != nil {
		clear(accessKey)
		return nil, errors.New("load artifact-retention object storage secret key")
	}
	store, err := s3store.New(ctx, s3store.Config{
		Endpoint: config.ObjectStorageEndpoint, Region: config.ObjectStorageRegion, Bucket: config.ObjectStorageBucket,
		AccessKeyID: strings.TrimSpace(string(accessKey)), SecretKey: strings.TrimSpace(string(secretKey)), UsePathStyle: config.ObjectStorageUsePathStyle,
	})
	clear(accessKey)
	clear(secretKey)
	if err != nil {
		return nil, errors.New("open artifact-retention object storage")
	}
	return store, nil
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

type dependencyChecker interface{ Check(context.Context) error }

func monitorReadiness(pool *pgxpool.Pool, objects dependencyChecker, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := pool.Ping(check)
			if err == nil {
				err = objects.Check(check)
			}
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "artifact retention readiness restored")
				}
				metrics.SetReady(true)
			} else {
				if readiness.Set(false, "dependency_unavailable") {
					logger.WarnContext(ctx, "artifact retention readiness lost", "error_class", "dependency")
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

func runRetentionLoop(processor *retention.Processor, metrics *retentionMetrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		idleBackoff := serviceruntime.NewIdleBackoff(config.PollInterval, 5*time.Minute)
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			processed, err := processor.Process(cycle, config.InstanceID, config.BatchSize, int64(config.ClaimLease/time.Second))
			cancel()
			if err != nil {
				metrics.cycles.WithLabelValues("failed").Inc()
				metrics.failures.Inc()
				if !degraded {
					logger.WarnContext(ctx, "artifact retention cycle degraded", "error_class", "retention")
					degraded = true
				}
			} else {
				metrics.cycles.WithLabelValues("succeeded").Inc()
				metrics.purged.Add(float64(processed))
				if degraded {
					logger.InfoContext(ctx, "artifact retention cycle restored")
					degraded = false
				}
			}
			timer := time.NewTimer(idleBackoff.Next(err == nil && processed > 0))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
