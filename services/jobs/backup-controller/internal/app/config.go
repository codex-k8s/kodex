package app

import (
	"errors"
	"net"
	"path/filepath"
	"regexp"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	serviceName      = "backup-controller"
	metricsSubsystem = "backup_controller"
)

type Config struct {
	Environment               string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen           string        `env:"BACKUP_CONTROLLER_TECHNICAL_LISTEN"`
	CredentialsFile           string        `env:"BACKUP_CONTROLLER_CREDENTIALS_FILE"`
	RepositoryCredentialsFile string        `env:"BACKUP_CONTROLLER_REPOSITORY_CREDENTIALS_FILE"`
	RestoreTargetsFile        string        `env:"BACKUP_CONTROLLER_RESTORE_TARGETS_FILE"`
	RestoreApprovalFile       string        `env:"BACKUP_CONTROLLER_RESTORE_APPROVAL_FILE"`
	WorkDirectory             string        `env:"BACKUP_CONTROLLER_WORK_DIRECTORY"`
	RepositoryPrefix          string        `env:"BACKUP_CONTROLLER_REPOSITORY_PREFIX"`
	ReleaseRevision           string        `env:"BACKUP_CONTROLLER_RELEASE_REVISION"`
	BackupID                  string        `env:"BACKUP_CONTROLLER_BACKUP_ID"`
	BackupInterval            time.Duration `env:"BACKUP_CONTROLLER_BACKUP_INTERVAL"`
	BackupTimeout             time.Duration `env:"BACKUP_CONTROLLER_BACKUP_TIMEOUT"`
	StartupTimeout            time.Duration `env:"BACKUP_CONTROLLER_STARTUP_TIMEOUT"`
	ShutdownTimeout           time.Duration `env:"BACKUP_CONTROLLER_SHUTDOWN_TIMEOUT"`
	ReadinessInterval         time.Duration `env:"BACKUP_CONTROLLER_READINESS_INTERVAL"`
	RetentionMinimumAge       time.Duration `env:"BACKUP_CONTROLLER_RETENTION_MINIMUM_AGE"`
	RetentionKeep             int           `env:"BACKUP_CONTROLLER_RETENTION_KEEP"`
	MaximumDatabaseBytes      int64         `env:"BACKUP_CONTROLLER_MAXIMUM_DATABASE_BYTES"`
}

func loadConfig(command string) (Config, error) {
	config := Config{
		TechnicalListen: ":9090", CredentialsFile: "/var/run/secrets/kodex/backup-controller/credentials.json",
		RepositoryCredentialsFile: "/var/run/secrets/kodex/backup-controller/repository.json",
		RestoreTargetsFile:        "/var/run/secrets/kodex/backup-controller/restore/targets.json",
		RestoreApprovalFile:       "/var/run/secrets/kodex/backup-controller/restore/approval.json",
		WorkDirectory:             "/work", RepositoryPrefix: "kodex", BackupInterval: 24 * time.Hour,
		BackupTimeout: 6 * time.Hour, StartupTimeout: 45 * time.Second, ShutdownTimeout: 30 * time.Second,
		ReadinessInterval: 30 * time.Second, RetentionMinimumAge: 30 * 24 * time.Hour,
		RetentionKeep: 14, MaximumDatabaseBytes: 128 << 30,
	}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, errors.New("parse backup-controller configuration")
	}
	if err := config.validate(command); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate(command string) error {
	switch command {
	case "serve", "backup", "verify", "retain", "restore-drill", "fingerprint-targets":
	default:
		return errors.New("backup-controller command is invalid")
	}
	if config.Environment != "staging" && config.Environment != "production" {
		return errors.New("backup-controller environment is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("backup-controller technical endpoint is invalid")
	}
	for _, value := range []string{config.CredentialsFile, config.RepositoryCredentialsFile,
		config.RestoreTargetsFile, config.RestoreApprovalFile, config.WorkDirectory} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return errors.New("backup-controller file path is invalid")
		}
	}
	if config.RepositoryPrefix == "" ||
		(command != "fingerprint-targets" && !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(config.ReleaseRevision)) ||
		config.BackupInterval < time.Hour || config.BackupInterval > 7*24*time.Hour ||
		config.BackupTimeout < 10*time.Minute || config.BackupTimeout > 24*time.Hour ||
		config.StartupTimeout < 5*time.Second || config.StartupTimeout > 5*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > 5*time.Minute ||
		config.ReadinessInterval < 5*time.Second || config.ReadinessInterval > 5*time.Minute ||
		config.RetentionMinimumAge < 24*time.Hour || config.RetentionMinimumAge > 3650*24*time.Hour ||
		config.RetentionKeep < 1 || config.RetentionKeep > 1000 || config.MaximumDatabaseBytes < 1<<20 {
		return errors.New("backup-controller policy configuration is invalid")
	}
	if command == "verify" && config.BackupID == "" {
		return errors.New("backup-controller backup ID is required")
	}
	return nil
}
