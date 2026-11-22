// Package app owns the artifact-retention process composition and bounded worker lifecycle.
// RU: Package app владеет композицией процесса artifact-retention и ограниченным жизненным циклом workers.
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

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/capture"
	controlowner "github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/clients/controlplane"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/prepared"
	retentionpostgres "github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/repository/postgres"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/retention"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maximumSecretBytes caps protected credential and CA files before parsing.
// RU: maximumSecretBytes ограничивает защищённые файлы реквизитов и CA до разбора.
const maximumSecretBytes = 16 << 10

const (
	// captureIssuerUID pins the actual shared issuer sidecar's Unix peer identity.
	// captureIssuerUID закрепляет actual Unix peer identity общего issuer sidecar.
	captureIssuerUID = 29001
	// captureIssuerGID pins the sidecar's projected socket group from the canonical component.
	// captureIssuerGID закрепляет projected socket group sidecar из canonical component.
	captureIssuerGID = 29000
)

// Run starts telemetry, verifies PostgreSQL and object storage, then owns workers and ordered shutdown.
// RU: Run запускает telemetry, проверяет PostgreSQL и объектное хранилище, затем владеет workers и порядком остановки.
func Run(lifecycle, shutdownBase context.Context, buildVersion string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, serviceName, buildVersion)
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
	preparedRepository := retentionpostgres.New(pool)
	if deleter, ok := any(objects).(objectstorage.SingleAttemptDeleter); !ok || deleter.CheckDeleteOnce(startup) != nil {
		pool.Close()
		return errors.New("artifact-retention single-attempt deletion is unavailable")
	}
	if err := preparedRepository.CheckManual(startup); err != nil {
		pool.Close()
		return err
	}
	if err := preparedRepository.CheckPrepared(startup); err != nil {
		pool.Close()
		return err
	}
	control, err := controlplaneclient.Dial(startup, controlplaneclient.Config{
		RPCProfile:     config.RPCProfile,
		CallerWorkload: serviceName, Target: config.ControlPlaneTarget, TLSServerName: config.ControlPlaneTLSServerName,
		CAFile: config.ControlPlaneCAFile, ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile: config.ControlPlanePrivateKeyFile, ApplicationGrantFile: config.ApplicationGrantFile,
		ExpectedIssuerUID: captureIssuerUID, ExpectedIssuerGID: captureIssuerGID, DialTimeout: 5 * time.Second,
		Operations: controlplaneclient.ArtifactRetentionCaptureOperations(),
	})
	if err != nil {
		pool.Close()
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	captures := capture.NewProcessor(preparedRepository, controlowner.NewCapture(cp.NewManagedKubernetesOwnerServiceClient(control.ManagedKubernetesCaptureConnection())))
	if err := captures.Check(startup, config.InstanceID); err != nil {
		pool.Close()
		return err
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "ready")
	metrics.SetReady(true)
	technical, err := httpserver.New(httpserver.StandardProcessConfig(config.TechnicalListen), readiness, metrics.PrometheusHandler())
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
		technical.Worker(),
		monitorReadiness(pool, objects, readiness, metrics, logger, config, captures),
		runRetentionLoop(processor, retentionMetrics, logger, config),
		runPreparedLoop(prepared.NewProcessor(preparedRepository, objects), logger, config),
		runPreparedLoop(prepared.NewProcessor(retentionpostgres.NewKubernetesCleanup(preparedRepository), objects), logger, config),
		runCaptureLoop(captures, retentionMetrics, logger, config),
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

// openPostgres reads a protected DSN and CA, enforces TLS 1.3 with exact server name and pings the pool.
// RU: openPostgres читает защищённые DSN и CA, требует TLS 1.3 с точным именем сервера и проверяет pool через Ping.
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

// openObjectStorage reads bounded credentials, builds the S3 client and clears credential buffers.
// RU: openObjectStorage читает ограниченные реквизиты, создаёт S3-клиент и очищает буферы ключей.
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

// dependencyChecker exposes the startup/readiness probe of the exact object-store adapter.
// RU: dependencyChecker предоставляет проверку startup/readiness точного адаптера объектного хранилища.
type dependencyChecker interface {
	// Check verifies that the concrete dependency currently serves the required path.
	// RU: Check проверяет, что конкретная зависимость сейчас обслуживает нужный путь.
	Check(context.Context) error
}

// monitorReadiness periodically checks PostgreSQL, object storage, prepared statements and the protected capture producer
// before publishing a local readiness snapshot and bounded availability metrics.
// RU: monitorReadiness периодически проверяет PostgreSQL, объектное хранилище, prepared statements и защищённый capture producer
// перед публикацией локального снимка готовности и ограниченных метрик доступности.
func monitorReadiness(pool *pgxpool.Pool, objects dependencyChecker, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, logger *slog.Logger, config Config, captures *capture.Processor) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.OperationTimeout)
			err := pool.Ping(check)
			if err == nil {
				err = objects.Check(check)
			}
			if err == nil {
				err = retentionpostgres.New(pool).CheckPrepared(check)
			}
			if err == nil {
				err = retentionpostgres.New(pool).CheckManual(check)
			}
			if err == nil {
				err = captures.Check(check, config.InstanceID)
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

// runRetentionLoop claims bounded purge batches, records actual completed purges and backs off while idle.
// RU: runRetentionLoop получает ограниченные пачки очистки, учитывает реально удалённое и увеличивает паузу при простое.
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
			timer := time.NewTimer(idleBackoff.Next(shouldResetRetentionBackoff(processed, err)))
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

// shouldResetRetentionBackoff resets polling after work or a failure that needs timely retry.
// RU: shouldResetRetentionBackoff сбрасывает паузу опроса после работы или отказа, требующего скорого повтора.
func shouldResetRetentionBackoff(processed int, err error) bool {
	return err != nil || processed > 0
}
