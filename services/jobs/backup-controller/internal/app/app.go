// Package app composes backup-controller commands, readiness, workers and bounded shutdown.
// RU: Package app собирает команды backup-controller, готовность, workers и ограниченное завершение.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/httpserver"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	serviceobservability "github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/observability"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/postgresbackup"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/runner"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
)

// Run validates command configuration, handles fingerprint-only requests or starts the controller.
// RU: Run проверяет настройки команды, выполняет только вычисление fingerprint либо запускает контроллер.
func Run(lifecycle, shutdownBase context.Context, command, buildVersion string) error {
	config, err := loadConfig(command)
	if err != nil {
		return err
	}
	if command == "fingerprint-targets" {
		targets, err := configspec.LoadRestoreTargets(config.RestoreTargetsFile, config.Environment)
		if err != nil {
			return err
		}
		digest, err := configspec.FingerprintTargets(targets)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"targetSetSha256": digest})
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	sharedMetrics := sharedobservability.NewMetrics(metricsSubsystem, buildVersion, map[string]string{})
	metrics, err := serviceobservability.New(sharedMetrics)
	if err != nil {
		return errors.New("register backup-controller metrics")
	}
	controller, err := buildRunner(lifecycle, config, command, buildVersion, metrics)
	if err != nil {
		return err
	}
	if command != "serve" {
		return runOnce(lifecycle, controller, config, command)
	}
	return serve(lifecycle, shutdownBase, controller, config, sharedMetrics, metrics, logger)
}

// buildRunner selects the command's protected credential set and wires S3, PostgreSQL and business metrics.
// RU: buildRunner выбирает защищённые реквизиты команды и связывает S3, PostgreSQL и бизнес-метрики.
func buildRunner(ctx context.Context, config Config, command, buildVersion string,
	metrics *serviceobservability.Metrics) (*runner.Runner, error) {
	credentials := configspec.Credentials{SchemaVersion: 1}
	if command == "serve" || command == "backup" {
		loaded, err := configspec.LoadCredentials(config.CredentialsFile, config.Environment)
		if err != nil {
			return nil, err
		}
		credentials = loaded
	} else {
		loaded, err := configspec.LoadRepositoryCredentials(config.RepositoryCredentialsFile, config.Environment)
		if err != nil {
			return nil, err
		}
		credentials.Destination = loaded.Destination
	}
	destination, err := s3backup.NewClient(ctx, credentials.Destination)
	if err != nil {
		return nil, err
	}
	repository, err := s3backup.NewRepository(destination, config.RepositoryPrefix)
	if err != nil {
		return nil, err
	}
	postgres, err := postgresbackup.New(config.MaximumDatabaseBytes)
	if err != nil {
		return nil, err
	}
	return runner.New(postgres, repository, credentials, config.WorkDirectory,
		buildVersion, config.ReleaseRevision, metrics)
}

// runOnce executes one bounded backup, verify, retention or owner-approved restore-drill command.
// RU: runOnce выполняет одну ограниченную команду копирования, проверки, хранения либо подтверждённого восстановления.
func runOnce(ctx context.Context, controller *runner.Runner, config Config, command string) error {
	operation, cancel := context.WithTimeout(ctx, config.BackupTimeout)
	defer cancel()
	switch command {
	case "backup":
		if err := controller.Check(operation); err != nil {
			return err
		}
		if _, err := controller.Backup(operation); err != nil {
			return err
		}
		return controller.Retain(operation, runner.Policy{MinimumAge: config.RetentionMinimumAge, Keep: config.RetentionKeep})
	case "verify":
		return controller.Verify(operation, config.BackupID)
	case "retain":
		return controller.Retain(operation, runner.Policy{MinimumAge: config.RetentionMinimumAge, Keep: config.RetentionKeep})
	case "restore-drill":
		approval, err := configspec.LoadRestoreApproval(config.RestoreApprovalFile, time.Now().UTC())
		if err != nil {
			return err
		}
		targets, err := configspec.LoadRestoreTargets(config.RestoreTargetsFile, config.Environment)
		if err != nil {
			return err
		}
		return controller.RestoreDrill(operation, approval, targets)
	default:
		return errors.New("backup-controller one-shot command is invalid")
	}
}

// serve passes the startup barrier before exposing readiness and joining technical and backup workers at shutdown.
// RU: serve проходит стартовый барьер до публикации готовности и дожидается технического и backup workers при остановке.
func serve(lifecycle, shutdownBase context.Context, controller *runner.Runner, config Config,
	sharedMetrics *sharedobservability.Metrics, metrics *serviceobservability.Metrics, logger *slog.Logger) error {
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	if err := controller.Check(startup); err != nil {
		return fmt.Errorf("backup-controller startup barrier failed: %w", err)
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "ready")
	sharedMetrics.SetReady(true)
	status := newStatusStore()
	backupID, lastBackup, lastRestore, err := controller.Readback(startup)
	if err != nil {
		return fmt.Errorf("backup-controller startup readback failed: %w", err)
	}
	status.initialize(backupID, lastBackup)
	if !lastBackup.IsZero() {
		metrics.SetLastSuccessfulBackup(lastBackup)
	}
	if !lastRestore.IsZero() {
		metrics.SetLastVerifiedRestore(lastRestore)
	}
	technical, err := httpserver.New(httpserver.Config{
		Address: config.TechnicalListen, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaximumHeaderBytes: 32 << 10,
		MaximumConnections: 128,
	}, readiness, sharedMetrics.PrometheusHandler(), httpserver.ExactGETRoute{
		Path: "/status", ContentType: "application/json", Handler: status,
	})
	if err != nil {
		return err
	}
	if err := technical.Listen(); err != nil {
		return err
	}
	workers := serviceruntime.StartWorkers(lifecycle,
		technical.Worker(),
		monitorReadiness(controller, readiness, sharedMetrics, metrics, logger, config),
		runBackupLoop(controller, metrics, status, logger, config),
	)
	err = workers.Wait(context.WithoutCancel(lifecycle))
	readiness.Set(false, "stopping")
	sharedMetrics.SetReady(false)
	workers.Stop()
	shutdownErr := serviceruntime.RunShutdown(shutdownBase,
		serviceruntime.ShutdownOperation{Name: "backup workers", Timeout: config.ShutdownTimeout / 2, Run: workers.Wait},
		serviceruntime.ShutdownOperation{Name: "technical HTTP", Timeout: config.ShutdownTimeout / 2, Run: technical.Shutdown},
	)
	return errors.Join(err, shutdownErr)
}

// monitorReadiness periodically checks dependencies and readback before updating readiness and last-success gauges.
// RU: monitorReadiness периодически проверяет зависимости и readback до обновления готовности и метрик последних успехов.
func monitorReadiness(controller *runner.Runner, readiness *serviceruntime.Readiness,
	sharedMetrics *sharedobservability.Metrics, serviceMetrics *serviceobservability.Metrics,
	logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			check, cancel := context.WithTimeout(ctx, config.StartupTimeout)
			err := controller.Check(check)
			if err == nil {
				_, lastBackup, lastRestore, readbackErr := controller.Readback(check)
				err = readbackErr
				if !lastBackup.IsZero() {
					serviceMetrics.SetLastSuccessfulBackup(lastBackup)
				}
				if !lastRestore.IsZero() {
					serviceMetrics.SetLastVerifiedRestore(lastRestore)
				}
			}
			cancel()
			if err == nil {
				if readiness.Set(true, "ready") {
					logger.InfoContext(ctx, "backup controller readiness restored")
				}
				sharedMetrics.SetReady(true)
			} else {
				if readiness.Set(false, "dependency_unavailable") {
					logger.WarnContext(ctx, "backup controller readiness lost", "error_class", "dependency")
				}
				sharedMetrics.SetReady(false)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// runBackupLoop schedules due backups, accounts for retention after success and reports degraded transitions.
// RU: runBackupLoop запускает положенные по сроку копии, учитывает retention после успеха и сообщает о деградации.
func runBackupLoop(controller *runner.Runner, metrics *serviceobservability.Metrics,
	status *statusStore, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		interval := config.BackupInterval / 12
		if interval > 15*time.Minute {
			interval = 15 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		degraded := false
		for {
			operation, cancel := context.WithTimeout(ctx, config.BackupTimeout)
			due, err := controller.Due(operation, config.BackupInterval)
			if err == nil && due {
				status.set("running", "", true, false)
				backupID, backupErr := controller.Backup(operation)
				backupSucceeded := backupErr == nil
				if backupSucceeded {
					backupErr = controller.Retain(operation, runner.Policy{
						MinimumAge: config.RetentionMinimumAge, Keep: config.RetentionKeep,
					})
				}
				err = backupErr
				status.set(map[bool]string{true: "degraded", false: "idle"}[err != nil], backupID, false, backupSucceeded)
			} else if err == nil {
				metrics.BackupFinished("skipped")
			}
			cancel()
			if err != nil && !degraded {
				degraded = true
				logger.WarnContext(ctx, "backup controller cycle degraded", "error_class", "backup")
			} else if err == nil && degraded {
				degraded = false
				logger.InfoContext(ctx, "backup controller cycle restored")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}
